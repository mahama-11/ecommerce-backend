package authz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/platform"
)

func testAuthzService(t *testing.T, handler http.HandlerFunc) (*Service, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "ecommerce-test", InternalServiceSecret: "secret"})
	return NewService(client), server.Close
}

func TestResolveMapsPlatformOrgRoleToProductPermissions(t *testing.T) {
	service, cleanup := testAuthzService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/access/users/user-1/orgs/org-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"user_id": "user-1", "org_id": "org-1", "org_role": "admin", "permissions": []string{"platform.wallet.read"}}})
	})
	defer cleanup()

	result, err := service.Resolve("user-1", "org-1")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !result.HasAccess || result.ActiveOrgID != "org-1" {
		t.Fatalf("unexpected access summary: %+v", result)
	}
	want := "ecommerce.billing.manage"
	for _, permission := range result.ProductPermissions {
		if permission == want {
			return
		}
	}
	t.Fatalf("admin permissions missing %s: %+v", want, result.ProductPermissions)
}

func TestResolveFailsClosedWhenPlatformAccessContextFails(t *testing.T) {
	service, cleanup := testAuthzService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "message": "unauthorized", "error_code": "UNAUTHORIZED"})
	})
	defer cleanup()

	if _, err := service.Resolve("user-1", "org-1"); err == nil {
		t.Fatalf("expected platform access failure")
	}
}
