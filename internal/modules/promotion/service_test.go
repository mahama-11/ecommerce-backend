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
