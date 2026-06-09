package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/authz"
	promotionmodule "ecommerce-service/internal/modules/promotion"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLoginAndSessionProjectTokenUserCreditsAndAccess(t *testing.T) {
	service, cleanup := testAuthService(t, authPlatformFixture(t))
	defer cleanup()

	login, err := service.Login(LoginInput{Email: "merchant@example.com", Password: "secret123"})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if login.AccessToken != "login-token" || login.User.ID != "user-1" || login.User.OrgID != "org-1" || login.Credits.Balance != 42 || !login.Access.HasAccess {
		t.Fatalf("unexpected login projection: %+v", login)
	}

	session, err := service.Session("user-1", "org-1")
	if err != nil {
		t.Fatalf("Session returned error: %v", err)
	}
	if !session.Authenticated || session.User.ID != "user-1" || session.Access.ActiveOrgID != "org-1" || session.Credits.AssetCode != "ECOMMERCE_CREDIT" {
		t.Fatalf("unexpected session projection: %+v", session)
	}
}

func TestRegisterSendsOrganizationLanguageAndPromotionFailureIsNonFatal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth-promotion.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PromotionAttributionAttempt{}); err != nil {
		t.Fatalf("auto migrate promotion attempts: %v", err)
	}
	var registerCompany string
	var conversionCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/register":
			var payload platform.AuthRegisterInput
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode register payload: %v", err)
			}
			registerCompany = payload.Company
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"access_token": "register-token", "user": map[string]any{"id": "user-1", "email": payload.Email, "full_name": payload.FullName, "org_id": "org-1", "org_role": "owner", "status": "active", "orgs": []map[string]any{{"id": "org-1", "name": payload.Company, "role": "owner"}}}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-conversions":
			conversionCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 2001, "message": "promotion platform unavailable", "error_code": "PROMOTION_PLATFORM_DOWN", "error_hint": "Attribution can be retried asynchronously.", "request_id": "platform-promo", "timestamp": time.Now().Unix()})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/access/users/user-1/orgs/org-1":
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"user_id": "user-1", "org_id": "org-1", "org_role": "owner", "permissions": []string{"platform.access"}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/summary":
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "product_code": "ecommerce", "total_balance": 42, "permanent_balance": 40, "reward_balance": 2, "allowance_balance": 0, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "balance": 42}}})
		default:
			t.Fatalf("unexpected platform request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "ecommerce-test", InternalServiceSecret: "test-internal-secret"})
	promotionSvc := promotionmodule.NewService(client, repository.NewCommercialRepository(db), config.AppConfig{ProductCode: "ecommerce", CreditsAssetCode: "ECOMMERCE_CREDIT"})
	service := NewService(client, nil, authz.NewService(client), promotionSvc, config.AppConfig{ProductCode: "ecommerce", DefaultLanguage: "en", CreditsAssetCode: "ECOMMERCE_CREDIT"})

	result, err := service.Register(RegisterInput{FullName: "Merchant", Email: "merchant@example.com", Password: "secret123", OrganizationName: "Explicit Org", Language: "fr", PromotionCode: "SAVE20"})
	if err != nil {
		t.Fatalf("Register should remain successful when promotion attribution fails: %v", err)
	}
	if result.AccessToken != "register-token" || result.User.OrgName != "Explicit Org" || registerCompany != "Explicit Org" || conversionCalls != 1 {
		t.Fatalf("unexpected register projection/company/calls: result=%+v company=%q conversionCalls=%d", result, registerCompany, conversionCalls)
	}
	var attempt models.PromotionAttributionAttempt
	if err := db.First(&attempt, "organization_id = ? AND promotion_code = ?", "org-1", "SAVE20").Error; err != nil {
		t.Fatalf("promotion attribution attempt was not recorded: %v", err)
	}
	if attempt.Status != "failed" || attempt.ErrorCode != "PROMOTION_PLATFORM_DOWN" || strings.Contains(attempt.ErrorMessage, "test-internal-secret") {
		t.Fatalf("promotion failure was not captured safely: %+v", attempt)
	}
}

func TestCreditsSummaryFallsBackWhenWalletUnavailable(t *testing.T) {
	service, cleanup := testAuthService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "wallet unavailable", "error_code": "WALLET_DOWN"})
	})
	defer cleanup()

	credits := service.buildCreditsSummary("org-1")
	if credits.AssetCode != "ECOMMERCE_CREDIT" || credits.Balance != 0 || credits.RewardBalance != 0 {
		t.Fatalf("unexpected fallback credits summary: %+v", credits)
	}
}

func TestPlatformTimeoutPropagatesAsAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Millisecond, ServiceName: "ecommerce-timeout-test", InternalServiceSecret: "secret"})
	service := NewService(client, nil, authz.NewService(client), nil, config.AppConfig{ProductCode: "ecommerce", DefaultLanguage: "en", CreditsAssetCode: "ECOMMERCE_CREDIT"})

	if _, err := service.Login(LoginInput{Email: "merchant@example.com", Password: "secret123"}); err == nil {
		t.Fatalf("expected login to return platform timeout error")
	}
}

func TestUserSummaryHelpersUseCurrentOrgAndDefaults(t *testing.T) {
	service, cleanup := testAuthService(t, authPlatformFixture(t))
	defer cleanup()

	user := service.buildUserSummary(platform.PlatformUserProfile{ID: "user-2", Email: "two@example.com", FullName: "User Two", OrgID: "org-2", OrgRole: "viewer", Status: "active", Orgs: []platform.PlatformOrganizationLite{{ID: "org-1", Name: "Other Org"}, {ID: "org-2", Name: "Current Org"}}})
	if user.OrgName != "Current Org" || user.LanguagePreference != "zh" {
		t.Fatalf("unexpected user summary defaults: %+v", user)
	}
	if currentOrgName(platform.PlatformUserProfile{OrgID: "missing", Orgs: []platform.PlatformOrganizationLite{{ID: "org-1", Name: "Other"}}}) != "" {
		t.Fatalf("currentOrgName should return empty string when active org is absent")
	}
	if defaultString("  explicit  ", "fallback") != "explicit" || defaultString(" ", "fallback") != "fallback" {
		t.Fatalf("defaultString trimming/fallback contract changed")
	}
	if service.String() != "auth-service" {
		t.Fatalf("unexpected service String(): %s", service.String())
	}
}
