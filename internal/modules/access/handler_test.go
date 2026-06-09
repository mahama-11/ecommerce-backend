package access

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/modules/authz"
	"ecommerce-service/internal/platform"

	"github.com/gin-gonic/gin"
)

func TestMeReturnsAccessProjectionFromPlatformContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/access/users/user-1/orgs/org-1" {
			t.Fatalf("unexpected access path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"user_id": "user-1", "org_id": "org-1", "org_role": "viewer", "permissions": []string{"platform.access"}}})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "ecommerce-test", InternalServiceSecret: "secret"})
	handler := NewHandler(authz.NewService(client))
	r := gin.New()
	r.GET("/access/me", func(c *gin.Context) { c.Set("userID", "user-1"); c.Set("orgID", "org-1") }, handler.Me)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/access/me", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !json.Valid(w.Body.Bytes()) || !contains(w.Body.String(), "ecommerce.viewer") || !contains(w.Body.String(), "ecommerce.template.read") {
		t.Fatalf("access response missing expected product projection: %s", w.Body.String())
	}
}

func contains(s, needle string) bool {
	return len(needle) == 0 || (len(s) >= len(needle) && jsonContainsString(s, needle))
}

func jsonContainsString(s, needle string) bool {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
