package router

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	accessmodule "ecommerce-service/internal/modules/access"
	auditmodule "ecommerce-service/internal/modules/audit"
	authmodule "ecommerce-service/internal/modules/auth"
	billingmodule "ecommerce-service/internal/modules/billing"
	commercialmodule "ecommerce-service/internal/modules/commercial"
	commissionmodule "ecommerce-service/internal/modules/commission"
	imageruntimemodule "ecommerce-service/internal/modules/imageruntime"
	productcoremodule "ecommerce-service/internal/modules/productcore"
	promotionmodule "ecommerce-service/internal/modules/promotion"
	promptcentermodule "ecommerce-service/internal/modules/promptcenter"
	templatecentermodule "ecommerce-service/internal/modules/templatecenter"
	visualworkflowmodule "ecommerce-service/internal/modules/visualworkflow"
	walletmodule "ecommerce-service/internal/modules/wallet"
	"ecommerce-service/internal/modules/workspace"
	"ecommerce-service/internal/platform"

	"github.com/gin-gonic/gin"
)

func testRouter(t *testing.T) *gin.Engine {
	t.Helper()
	cfg := config.Config{
		GinMode: "test",
		App: config.AppConfig{
			FrontendBaseURL: "https://console.example.test",
			ProductName:     "Agent Ecommerce",
			ProductCode:     "ecommerce",
		},
		Security: config.SecurityConfig{ServiceSecretKey: "ecommerce-service-secret-for-tests"},
		Platform: config.PlatformConfig{
			BaseURL:               "https://platform.example.test",
			Timeout:               time.Second,
			ServiceName:           "v-ecommerce-backend-test",
			InternalServiceSecret: "platform-internal-secret-for-tests",
			JWTSecret:             "platform-jwt-secret-for-tests",
		},
		Monitoring: config.MonitoringConfig{
			Metrics: config.MetricsConfig{Namespace: "ecommerce_test", Subsystem: "router", Path: "/metrics", HistogramBuckets: []float64{0.1, 0.5, 1}},
			Tracing: config.TracingConfig{ServiceName: "ecommerce-service-test"},
		},
	}
	return New(
		cfg,
		platform.New(cfg.Platform),
		nil,
		nil,
		&authmodule.Handler{},
		&accessmodule.Handler{},
		&imageruntimemodule.Handler{},
		&workspace.Handler{},
		&auditmodule.Handler{},
		&templatecentermodule.Handler{},
		&promptcentermodule.Handler{},
		&walletmodule.Handler{},
		&promotionmodule.Handler{},
		&commissionmodule.Handler{},
		&billingmodule.Handler{},
		&commercialmodule.Handler{},
		&productcoremodule.Handler{},
		&visualworkflowmodule.Handler{},
	)
}

func TestRouteInventory(t *testing.T) {
	r := testRouter(t)
	actual := map[string]bool{}
	for _, route := range r.Routes() {
		actual[route.Method+" "+route.Path] = true
	}

	required := []string{
		"GET /healthz",
		"GET /readyz",
		"POST /api/v1/ecommerce/auth/register",
		"POST /api/v1/ecommerce/auth/login",
		"GET /api/v1/ecommerce/auth/session",
		"GET /api/v1/ecommerce/access/me",
		"GET /api/v1/ecommerce/wallet/summary",
		"GET /api/v1/ecommerce/wallet/history",
		"GET /api/v1/ecommerce/commercial/offerings",
		"POST /api/v1/ecommerce/commercial/orders",
		"POST /api/v1/ecommerce/commercial/orders/:orderID/confirm-payment",
		"GET /api/v1/ecommerce/billing/summary",
		"GET /api/v1/ecommerce/billing/charges",
		"POST /api/v1/ecommerce/assets/source",
		"POST /api/v1/ecommerce/prompts/preview",
		"POST /api/v1/ecommerce/products",
		"GET /api/v1/ecommerce/products/:product_id",
		"POST /api/v1/ecommerce/v2/visual-workflows/sessions",
		"GET /api/v1/ecommerce/v2/visual-workflows/:session_id/stage-view",
		"POST /api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions",
		"POST /api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions/:version_id/writeback-selected-asset",
		"POST /api/v1/ecommerce/template-center/catalog/:templateId/use",
		"POST /internal/v1/ecommerce/commercial/billing/charges",
		"POST /internal/v1/ecommerce/jobs/:jobID/runtime",
		"POST /internal/v1/ecommerce/jobs/:jobID/results",
	}
	sort.Strings(required)
	for _, route := range required {
		if !actual[route] {
			t.Fatalf("required route missing: %s", route)
		}
	}
}

func TestProtectedRoutesRejectMissingToken(t *testing.T) {
	r := testRouter(t)
	protected := []string{
		"/api/v1/ecommerce/auth/session",
		"/api/v1/ecommerce/access/me",
		"/api/v1/ecommerce/wallet/summary",
		"/api/v1/ecommerce/commercial/orders",
		"/api/v1/ecommerce/products",
		"/api/v1/ecommerce/v2/visual-workflows/sessions",
		"/api/v1/ecommerce/template-center/catalog/template-1/use",
	}
	for _, path := range protected {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if path == "/api/v1/ecommerce/commercial/orders" || path == "/api/v1/ecommerce/products" || path == "/api/v1/ecommerce/v2/visual-workflows/sessions" || path == "/api/v1/ecommerce/template-center/catalog/template-1/use" {
			req.Method = http.MethodPost
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: got status %d, want 401", req.Method, path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "TOKEN_INVALID") {
			t.Fatalf("%s %s without token body missing TOKEN_INVALID: %s", req.Method, path, w.Body.String())
		}
	}
}

func TestCORSAllowedOriginEchoOnly(t *testing.T) {
	r := testRouter(t)
	allowed := httptest.NewRequest(http.MethodOptions, "/api/v1/ecommerce/health", nil)
	allowed.Header.Set("Origin", "https://console.example.test")
	allowedRecorder := httptest.NewRecorder()
	r.ServeHTTP(allowedRecorder, allowed)
	if got := allowedRecorder.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.test" {
		t.Fatalf("allowed origin header = %q", got)
	}

	blocked := httptest.NewRequest(http.MethodOptions, "/api/v1/ecommerce/health", nil)
	blocked.Header.Set("Origin", "https://evil.example.test")
	blockedRecorder := httptest.NewRecorder()
	r.ServeHTTP(blockedRecorder, blocked)
	if got := blockedRecorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected blocked origin echo = %q", got)
	}
}
