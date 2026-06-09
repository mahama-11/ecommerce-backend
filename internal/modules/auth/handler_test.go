package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/modules/authz"
	"ecommerce-service/internal/platform"

	"github.com/gin-gonic/gin"
)

func newAuthHandlerRouter(service *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandler(service, nil)
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.POST("/auth/register", h.Register)
	r.POST("/auth/login", h.Login)
	r.GET("/auth/session", func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		h.Session(c)
	})
	return r
}

func TestHandlerRegisterLoginAndSessionReturnStableEnvelope(t *testing.T) {
	service, cleanup := testAuthService(t, authPlatformFixture(t))
	defer cleanup()
	r := newAuthHandlerRouter(service)

	registerReq := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{"full_name":"Merchant","email":"merchant@example.com","password":"secret123","organization_name":"Merchant Workspace"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("X-Request-ID", "req-auth-register")
	registerResp := httptest.NewRecorder()
	r.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registerResp.Code, registerResp.Body.String())
	}
	assertAuthEnvelope(t, registerResp.Body.String(), "req-auth-register")
	if !strings.Contains(registerResp.Body.String(), `"access_token":"register-token"`) || strings.Contains(registerResp.Body.String(), "secret123") || strings.Contains(registerResp.Body.String(), "test-internal-secret") {
		t.Fatalf("register response did not project token safely: %s", registerResp.Body.String())
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"merchant@example.com","password":"secret123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("X-Request-ID", "req-auth-login")
	loginResp := httptest.NewRecorder()
	r.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK || !strings.Contains(loginResp.Body.String(), `"access_token":"login-token"`) {
		t.Fatalf("login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	assertAuthEnvelope(t, loginResp.Body.String(), "req-auth-login")
	if strings.Contains(loginResp.Body.String(), "secret123") || strings.Contains(loginResp.Body.String(), "test-internal-secret") {
		t.Fatalf("login response leaked request/internal secret: %s", loginResp.Body.String())
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	sessionReq.Header.Set("X-Request-ID", "req-auth-session")
	sessionResp := httptest.NewRecorder()
	r.ServeHTTP(sessionResp, sessionReq)
	if sessionResp.Code != http.StatusOK || !strings.Contains(sessionResp.Body.String(), `"authenticated":true`) || !strings.Contains(sessionResp.Body.String(), `"request_id":"req-auth-session"`) {
		t.Fatalf("session status=%d body=%s", sessionResp.Code, sessionResp.Body.String())
	}
}

func TestHandlerRejectsBadInputAndMapsUnauthorizedWithoutSecretLeak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "message": "invalid credentials", "error_code": "INVALID_CREDENTIALS", "error_hint": "Check your credentials.", "request_id": "platform-req", "timestamp": time.Now().Unix()})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "ecommerce-test", InternalServiceSecret: "test-internal-secret"})
	r := newAuthHandlerRouter(NewService(client, nil, authz.NewService(client), nil, config.AppConfig{ProductCode: "ecommerce", DefaultLanguage: "en", CreditsAssetCode: "ECOMMERCE_CREDIT"}))

	badReq := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(`{"email":"not-an-email"}`))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("X-Request-ID", "req-auth-bad-input")
	badResp := httptest.NewRecorder()
	r.ServeHTTP(badResp, badReq)
	if badResp.Code != http.StatusBadRequest || !strings.Contains(badResp.Body.String(), `"request_id":"req-auth-bad-input"`) || !strings.Contains(badResp.Body.String(), `"errors"`) {
		t.Fatalf("bad register response: status=%d body=%s", badResp.Code, badResp.Body.String())
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"merchant@example.com","password":"secret123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("X-Request-ID", "req-auth-unauthorized")
	loginResp := httptest.NewRecorder()
	r.ServeHTTP(loginResp, loginReq)
	body := loginResp.Body.String()
	if loginResp.Code != http.StatusUnauthorized || !strings.Contains(body, `"error_code":"INVALID_CREDENTIALS"`) || !strings.Contains(body, `"request_id":"req-auth-unauthorized"`) {
		t.Fatalf("unauthorized login response: status=%d body=%s", loginResp.Code, body)
	}
	if strings.Contains(body, "secret123") || strings.Contains(body, "test-internal-secret") || strings.Contains(body, "access_token") {
		t.Fatalf("unauthorized response leaked sensitive material: %s", body)
	}
}

func TestHandlerMapsConflictNotFoundAndFallbackPlatformErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		code      int
		errorCode string
		wantHTTP  int
	}{
		{name: "conflict", status: http.StatusConflict, code: 409, errorCode: "EMAIL_ALREADY_EXISTS", wantHTTP: http.StatusConflict},
		{name: "not_found", status: http.StatusNotFound, code: 404, errorCode: "PROFILE_NOT_FOUND", wantHTTP: http.StatusNotFound},
		{name: "timeout_like_external", status: http.StatusBadGateway, code: 2001, errorCode: "PLATFORM_TIMEOUT", wantHTTP: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": tc.code, "message": "platform failed", "error_code": tc.errorCode, "error_hint": "retry safely", "request_id": "platform-req", "timestamp": time.Now().Unix()})
			}))
			defer server.Close()
			client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "ecommerce-test", InternalServiceSecret: "test-internal-secret"})
			r := newAuthHandlerRouter(NewService(client, nil, authz.NewService(client), nil, config.AppConfig{ProductCode: "ecommerce", DefaultLanguage: "en", CreditsAssetCode: "ECOMMERCE_CREDIT"}))
			path := "/auth/register"
			method := http.MethodPost
			payload := `{"full_name":"Merchant","email":"merchant@example.com","password":"secret123"}`
			if tc.name == "not_found" {
				path = "/auth/session"
				method = http.MethodGet
				payload = ""
			}
			req := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", "req-auth-error")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantHTTP || !strings.Contains(w.Body.String(), `"error_code":"`+tc.errorCode+`"`) || !strings.Contains(w.Body.String(), `"request_id":"req-auth-error"`) {
				t.Fatalf("mapped error status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "test-internal-secret") || strings.Contains(w.Body.String(), "secret123") {
				t.Fatalf("mapped error leaked secret: %s", w.Body.String())
			}
		})
	}
}

func assertAuthEnvelope(t *testing.T, body, requestID string) {
	t.Helper()
	if !json.Valid([]byte(body)) || !strings.Contains(body, `"code":0`) || !strings.Contains(body, `"request_id":"`+requestID+`"`) {
		t.Fatalf("response is not a successful request-scoped envelope: %s", body)
	}
}
