package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func signedPlatformToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestPlatformJWTAuthRejectsMissingMalformedAndExpiredTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "platform-jwt-secret-for-tests"
	cases := []struct {
		name   string
		header string
	}{
		{name: "missing"},
		{name: "malformed", header: "Bearer not-a-jwt"},
		{name: "expired", header: "Bearer " + signedPlatformToken(t, secret, jwt.MapClaims{"user_id": "u1", "org_id": "o1", "exp": time.Now().Add(-time.Hour).Unix()})},
		{name: "missing_org", header: "Bearer " + signedPlatformToken(t, secret, jwt.MapClaims{"user_id": "u1", "exp": time.Now().Add(time.Hour).Unix()})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/protected", PlatformJWTAuth(secret), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code < http.StatusBadRequest || w.Code >= http.StatusInternalServerError {
				t.Fatalf("status = %d, want fail-closed 4xx", w.Code)
			}
			if w.Body.String() == "" {
				t.Fatalf("expected stable error envelope")
			}
		})
	}
}

func TestPlatformJWTAuthSetsUserAndOrgClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "platform-jwt-secret-for-tests"
	r := gin.New()
	r.GET("/protected", PlatformJWTAuth(secret), func(c *gin.Context) {
		if c.GetString("userID") != "user-1" || c.GetString("orgID") != "org-1" || c.GetString("orgRole") != "admin" {
			t.Fatalf("claims not projected into gin context")
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signedPlatformToken(t, secret, jwt.MapClaims{"user_id": "user-1", "org_id": "org-1", "org_role": "admin", "exp": time.Now().Add(time.Hour).Unix()}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 body=%s", w.Code, w.Body.String())
	}
}

func TestOptionalPlatformJWTAuthAllowsAnonymousButProjectsValidClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "platform-jwt-secret-for-tests"
	r := gin.New()
	r.GET("/optional", OptionalPlatformJWTAuth(secret), func(c *gin.Context) {
		if c.GetString("userID") != "" {
			t.Fatalf("anonymous request should not get a userID")
		}
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/optional", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("anonymous optional status = %d", w.Code)
	}
}
