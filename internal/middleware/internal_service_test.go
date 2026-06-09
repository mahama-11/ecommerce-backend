package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireInternalServiceFailsClosedAndAcceptsExactSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/internal", RequireInternalService("strong-internal-secret"), func(c *gin.Context) {
		if c.GetString("internalServiceName") != "platform-test" {
			t.Fatalf("internal service name not projected")
		}
		c.Status(http.StatusNoContent)
	})
	for _, tc := range []struct{ name, secret string }{{"missing", ""}, {"wrong", "wrong-secret"}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal", nil)
			if tc.secret != "" {
				req.Header.Set("X-Internal-Service-Secret", tc.secret)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "INTERNAL_AUTH_FAILED") {
				t.Fatalf("expected fail-closed internal auth, status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "strong-internal-secret") || strings.Contains(w.Body.String(), tc.secret) && tc.secret != "" {
				t.Fatalf("internal callback auth error leaked secret material: %s", w.Body.String())
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/internal", nil)
	req.Header.Set("X-Internal-Service-Secret", "strong-internal-secret")
	req.Header.Set("X-Internal-Service", "platform-test")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("valid internal auth status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRequestContextPropagatesRequestAndTraceIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestContext())
	r.GET("/context", func(c *gin.Context) {
		if c.GetString("requestID") != "req-propagated" || c.GetString("traceID") != "trace-propagated" {
			t.Fatalf("gin context ids not propagated: request=%q trace=%q", c.GetString("requestID"), c.GetString("traceID"))
		}
		if c.Request.Context().Value("request_id") != "req-propagated" || c.Request.Context().Value("trace_id") != "trace-propagated" {
			t.Fatalf("request context ids not propagated")
		}
		if c.GetTime("requestStartedAt").IsZero() {
			t.Fatalf("requestStartedAt not set")
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/context", nil)
	req.Header.Set("X-Request-ID", "req-propagated")
	req.Header.Set("X-Trace-ID", "trace-propagated")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || w.Header().Get("X-Request-ID") != "req-propagated" || w.Header().Get("X-Trace-ID") != "trace-propagated" {
		t.Fatalf("headers/status mismatch status=%d request=%q trace=%q", w.Code, w.Header().Get("X-Request-ID"), w.Header().Get("X-Trace-ID"))
	}
}

func TestMetricsMiddlewareExposesSampledRouteCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Metrics("ecommerce_midtest", "http", []float64{0.01, 0.1}))
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/metrics", MetricsHandler("ecommerce_midtest", "http", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "http_requests_total") || !strings.Contains(w.Body.String(), `path="/ok"`) {
		t.Fatalf("metrics output missing sampled route counter: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMetricsRouteLabelUsesTemplateNotRawIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Metrics("ecommerce_midtest", "http", []float64{0.01, 0.1}))
	r.GET("/products/:id/assets/:assetID", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/metrics", MetricsHandler("ecommerce_midtest", "http", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/products/prod-secret-1/assets/asset-secret-1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/products/prod-secret-2/assets/asset-secret-2", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := w.Body.String()
	if !strings.Contains(body, `path="/products/:id/assets/:assetID"`) {
		t.Fatalf("metrics output missing templated route label: %s", body)
	}
	for _, raw := range []string{"prod-secret-1", "asset-secret-1", "prod-secret-2", "asset-secret-2"} {
		if strings.Contains(body, raw) {
			t.Fatalf("metrics route label leaked raw dynamic id %q: %s", raw, body)
		}
	}
}
