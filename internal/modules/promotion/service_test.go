package promotion

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPromotionRepo(t *testing.T) (*repository.CommercialRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "promotion.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PromotionAttributionAttempt{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return repository.NewCommercialRepository(db), db
}

func writePromotionEnvelope(t *testing.T, w http.ResponseWriter, status, code int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "", "request_id": "req-promotion", "timestamp": time.Now().Unix(), "data": data})
}

func TestEnsureCodeReturnsExistingActiveCodeWithoutCreatingDuplicate(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "program-1", "product_code": "ecommerce", "program_code": "ecommerce_signup_default", "name": "Signup", "status": "active", "trigger_type": "signup", "created_at": now, "updated_at": now}}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes":
			if r.URL.Query().Get("promoter_subject_id") != "org-promo" {
				t.Fatalf("unexpected promoter scope: %s", r.URL.RawQuery)
			}
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "code-1", "program_id": "program-1", "product_code": "ecommerce", "code": "SAVE20", "promoter_subject_type": "organization", "promoter_subject_id": "org-promo", "status": "active", "metadata": `{"owner":"unit"}`, "created_at": now, "updated_at": now}}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-codes":
			createCalls++
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "code-created", "program_id": "program-1", "product_code": "ecommerce", "code": "NEW", "status": "active", "created_at": now, "updated_at": now})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "promotion-test", InternalServiceSecret: "secret"})
	got, err := NewService(client, nil, config.AppConfig{ProductCode: "ecommerce", FrontendBaseURL: "https://console.example.test"}).EnsureCode("org-promo", CreateCodeInput{})
	if err != nil {
		t.Fatalf("EnsureCode: %v", err)
	}
	if got.Code != "SAVE20" || !strings.Contains(got.SignupURL, "promotion_code=SAVE20") || createCalls != 0 {
		t.Fatalf("unexpected ensure code result: got=%+v createCalls=%d", got, createCalls)
	}
}

func TestOverviewResolveAndSignupAttributionFailureIsRecordedButNonFatal(t *testing.T) {
	repo, db := newPromotionRepo(t)
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes/SAVE20/resolve":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"code": "SAVE20", "product_code": "ecommerce", "program_id": "program-1", "program_code": "ecommerce_signup_default", "program_name": "Signup", "status": "active", "promoter_subject_type": "organization", "promoter_subject_id": "org-promoter"})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "program-1", "product_code": "ecommerce", "program_code": "ecommerce_signup_default", "name": "Signup", "status": "active", "created_at": now, "updated_at": now}}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-conversions":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "conv-tracked", "product_code": "ecommerce", "status": "tracked", "created_at": now}, {"id": "conv-earned", "product_code": "ecommerce", "status": "reward_issued", "created_at": now}, {"id": "conv-reversed", "product_code": "ecommerce", "status": "reversed", "created_at": now}}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-conversions":
			writePromotionEnvelope(t, w, http.StatusBadGateway, 2001, nil)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "promotion-test", InternalServiceSecret: "secret"})
	svc := NewService(client, repo, config.AppConfig{ProductCode: "ecommerce", FrontendBaseURL: "https://console.example.test", CreditsAssetCode: "ECOMMERCE_CREDIT"})
	resolved, err := svc.ResolveCode("SAVE20")
	if err != nil || resolved.Code != "SAVE20" || resolved.PromoterSubjectID != "org-promoter" {
		t.Fatalf("unexpected resolve result: got=%+v err=%v", resolved, err)
	}
	overview, err := svc.Overview("org-promo", "")
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.TotalConversions != 3 || overview.TrackedConversions != 1 || overview.EarnedConversions != 1 || overview.ReversedConversions != 1 {
		t.Fatalf("unexpected overview counts: %+v", overview)
	}
	svc.TrackSignupAttribution("org-promo", "user-promo", "SAVE20")
	var attempt models.PromotionAttributionAttempt
	if err := db.First(&attempt, "organization_id = ? AND promotion_code = ?", "org-promo", "SAVE20").Error; err != nil {
		t.Fatalf("attribution attempt not persisted: %v", err)
	}
	if attempt.Status != "failed" || attempt.ErrorMessage == "" {
		t.Fatalf("platform failure was not recorded safely: %+v", attempt)
	}
}

func TestHandlerCreateCodeEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/incentives/referral-codes" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "code-handler", "program_id": "program-1", "product_code": "ecommerce", "code": "HANDLER20", "status": "active", "created_at": now, "updated_at": now})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "promotion-handler-test", InternalServiceSecret: "secret"})
	h := NewHandler(NewService(client, nil, config.AppConfig{ProductCode: "ecommerce", FrontendBaseURL: "https://console.example.test"}), nil)
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.POST("/codes", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.CreateCode(c) })
	req := httptest.NewRequest(http.MethodPost, "/codes", bytes.NewBufferString(`{"code":"HANDLER20"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-promotion-handler")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"request_id":"req-promotion-handler"`) || !strings.Contains(w.Body.String(), `"code":"HANDLER20"`) {
		t.Fatalf("unexpected create code response: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrapSeedsMissingAssetsAndDefaultProgram(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	createdAssets := map[string]bool{}
	createdProgram := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/assets":
			if r.URL.Query().Get("product_code") != "ecommerce" {
				t.Fatalf("unexpected asset product query: %s", r.URL.RawQuery)
			}
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "asset-cash", "asset_code": "ECOMMERCE_CREDIT", "product_code": "ecommerce", "asset_type": "cash_balance", "status": "active", "created_at": now, "updated_at": now}}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/assets":
			var req platform.CreateAssetDefinitionInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode asset definition: %v", err)
			}
			createdAssets[req.AssetCode] = true
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "asset-" + req.AssetCode, "asset_code": req.AssetCode, "product_code": req.ProductCode, "asset_type": req.AssetType, "status": req.Status, "created_at": now, "updated_at": now})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-programs":
			var req platform.CreateReferralProgramInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode referral program: %v", err)
			}
			if req.ProductCode != "ecommerce" || req.ProgramCode != "ecommerce_signup_default" || req.CommissionCurrency != "ECOMMERCE_CREDIT" {
				t.Fatalf("unexpected referral program payload: %+v", req)
			}
			createdProgram = true
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "program-created", "product_code": req.ProductCode, "program_code": req.ProgramCode, "name": req.Name, "status": req.Status, "created_at": now, "updated_at": now})
		default:
			t.Fatalf("unexpected bootstrap request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "promotion-bootstrap-test", InternalServiceSecret: "secret"})
	if err := NewService(client, nil, config.AppConfig{ProductCode: "ecommerce", CreditsAssetCode: "ECOMMERCE_CREDIT", RewardAssetCode: "ECOMMERCE_PROMO_CREDIT", AllowanceAssetCode: "ECOMMERCE_MONTHLY_ALLOWANCE"}).Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !createdAssets["ECOMMERCE_PROMO_CREDIT"] || !createdAssets["ECOMMERCE_MONTHLY_ALLOWANCE"] || createdAssets["ECOMMERCE_CREDIT"] || !createdProgram {
		t.Fatalf("bootstrap did not seed expected missing definitions/program: assets=%v program=%v", createdAssets, createdProgram)
	}
}

func TestHandlersReadEnsureAndConversionEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes/SAVE20/resolve":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"code": "SAVE20", "product_code": "ecommerce", "program_id": "program-1", "program_code": "ecommerce_signup_default", "status": "active", "promoter_subject_type": "organization", "promoter_subject_id": "org-promoter"})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "program-1", "product_code": "ecommerce", "program_code": "ecommerce_signup_default", "name": "Signup", "status": "active", "created_at": now, "updated_at": now}}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes":
			if r.URL.Query().Get("promoter_subject_id") != "org-handler" {
				t.Fatalf("unexpected code scope: %s", r.URL.RawQuery)
			}
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "code-1", "program_id": "program-1", "product_code": "ecommerce", "code": "HANDLER20", "status": "active", "created_at": now, "updated_at": now}}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-codes":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "code-created", "program_id": "program-1", "product_code": "ecommerce", "code": "CREATED20", "status": "active", "created_at": now, "updated_at": now})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-conversions":
			writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "conv-1", "product_code": "ecommerce", "status": "tracked", "created_at": now}}})
		default:
			t.Fatalf("unexpected handler request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "promotion-handler-test", InternalServiceSecret: "secret"})
	h := NewHandler(NewService(client, nil, config.AppConfig{ProductCode: "ecommerce", FrontendBaseURL: "https://console.example.test"}), nil)
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.GET("/codes/:code/resolve", h.ResolveCode)
	r.GET("/programs", h.ListPrograms)
	r.GET("/overview", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.Overview(c) })
	r.GET("/codes", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ListCodes(c) })
	r.POST("/codes/ensure", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.EnsureCode(c) })
	r.GET("/conversions", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ListConversions(c) })

	cases := []struct {
		method string
		path   string
		body   string
		needle string
	}{
		{http.MethodGet, "/codes/SAVE20/resolve", "", `"promoter_subject_id":"org-promoter"`},
		{http.MethodGet, "/programs", "", `"program_code":"ecommerce_signup_default"`},
		{http.MethodGet, "/overview", "", `"total_conversions":1`},
		{http.MethodGet, "/codes", "", `"code":"HANDLER20"`},
		{http.MethodPost, "/codes/ensure", `{}`, `"code":"HANDLER20"`},
		{http.MethodGet, "/conversions", "", `"id":"conv-1"`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "req-promotion-read-handler")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.needle) || !strings.Contains(w.Body.String(), `"request_id":"req-promotion-read-handler"`) {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSignupAttributionSuccessPersistsObservableAttempt(t *testing.T) {
	repo, db := newPromotionRepo(t)
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/incentives/referral-conversions" {
			t.Fatalf("unexpected attribution request: %s %s", r.Method, r.URL.String())
		}
		var req platform.CreateReferralConversionInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode conversion input: %v", err)
		}
		if req.ProductCode != "ecommerce" || req.ReferredSubjectID != "org-success" || req.SettlementSubjectID != "org-success" || req.CommissionCurrency != "ECOMMERCE_CREDIT" {
			t.Fatalf("unexpected conversion payload: %+v", req)
		}
		writePromotionEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "conversion-success", "product_code": "ecommerce", "referral_code": "SAVE20", "status": "tracked", "metadata": `{"platform":"ok"}`, "created_at": now, "updated_at": now})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "promotion-attribution-test", InternalServiceSecret: "secret"})
	svc := NewService(client, repo, config.AppConfig{ProductCode: "ecommerce", CreditsAssetCode: "ECOMMERCE_CREDIT"})
	svc.TrackSignupAttribution("org-success", "user-success", "")
	svc.TrackSignupAttribution("org-success", "user-success", "SAVE20")
	var attempts []models.PromotionAttributionAttempt
	if err := db.Find(&attempts, "organization_id = ?", "org-success").Error; err != nil {
		t.Fatalf("query attempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Status != "succeeded" || attempts[0].PlatformConversionID != "conversion-success" || attempts[0].MetadataJSON == "" {
		t.Fatalf("unexpected attribution attempts: %+v", attempts)
	}
}
