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

func TestMetricsMiddlewareExposesSampledRouteCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Metrics("ecommerce_midtest", "http", []float64{0.01, 0.1}))
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/metrics", MetricsHandler("ecommerce_midtest", "http", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ecommerce_midtest_http_http_requests_total") || !strings.Contains(w.Body.String(), `path="/ok"`) {
		t.Fatalf("metrics output missing sampled route counter: status=%d body=%s", w.Code, w.Body.String())
	}
}
