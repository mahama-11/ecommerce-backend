package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/modules/authz"
	"ecommerce-service/internal/platform"
)

type platformRoute func(w http.ResponseWriter, r *http.Request)

func testAuthService(t *testing.T, route platformRoute) (*Service, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(route))
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "ecommerce-test", InternalServiceSecret: "secret"})
	service := NewService(client, nil, authz.NewService(client), nil, config.AppConfig{ProductCode: "ecommerce", DefaultLanguage: "zh", CreditsAssetCode: "ECOMMERCE_CREDIT"})
	return service, server.Close
}

func writePlatformEnvelope(w http.ResponseWriter, status int, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "data": data, "message": "", "request_id": "req-test", "timestamp": time.Now().Unix()})
}

func authPlatformFixture(t *testing.T) platformRoute {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/register":
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"access_token": "register-token", "user": map[string]any{"id": "user-1", "email": "merchant@example.com", "full_name": "Merchant", "org_id": "org-1", "org_role": "owner", "status": "active", "orgs": []map[string]any{{"id": "org-1", "name": "Merchant Workspace", "role": "owner"}}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"access_token": "login-token", "user": map[string]any{"id": "user-1", "email": "merchant@example.com", "full_name": "Merchant", "org_id": "org-1", "org_role": "admin", "status": "active"}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/access/users/user-1/orgs/org-1":
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"user_id": "user-1", "org_id": "org-1", "org_role": "owner", "permissions": []string{"platform.access"}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/summary":
			if r.URL.Query().Get("product_code") != "ecommerce" {
				t.Fatalf("wallet product_code = %q", r.URL.Query().Get("product_code"))
			}
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "product_code": "ecommerce", "total_balance": 42, "permanent_balance": 40, "reward_balance": 2, "allowance_balance": 0, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "balance": 42}}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/users/user-1/profile":
			writePlatformEnvelope(w, http.StatusOK, 0, map[string]any{"id": "user-1", "email": "merchant@example.com", "full_name": "Merchant", "org_id": "org-1", "org_role": "owner", "status": "active"})
		default:
			t.Fatalf("unexpected platform request %s %s", r.Method, r.URL.String())
		}
	}
}

func TestRegisterProjectsPlatformTruthIntoProductAuthResult(t *testing.T) {
	service, cleanup := testAuthService(t, authPlatformFixture(t))
	defer cleanup()

	result, err := service.Register(RegisterInput{FullName: "Merchant", Email: "merchant@example.com", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if result.AccessToken != "register-token" || result.User.OrgID != "org-1" || result.User.OrgName != "Merchant Workspace" {
		t.Fatalf("unexpected auth result: %+v", result)
	}
	if result.Credits.Balance != 42 || result.Credits.AssetCode != "ECOMMERCE_CREDIT" {
		t.Fatalf("unexpected credits projection: %+v", result.Credits)
	}
	if !result.Access.HasAccess || len(result.Access.ProductPermissions) == 0 {
		t.Fatalf("access projection missing: %+v", result.Access)
	}
}

func TestLoginAndSessionFailClosedOnPlatformFailure(t *testing.T) {
	service, cleanup := testAuthService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "message": "unauthorized", "error_code": "UNAUTHORIZED"})
	})
	defer cleanup()

	if _, err := service.Login(LoginInput{Email: "merchant@example.com", Password: "bad-password"}); err == nil {
		t.Fatalf("expected login to fail closed on platform auth failure")
	}
	if _, err := service.Session("user-1", "org-1"); err == nil {
		t.Fatalf("expected session to fail closed on platform profile failure")
	}
}
