package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func TestRequestContextPreservesRequestIDAndTraceparentTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(otelgin.Middleware("ecommerce-request-context-test"), RequestContext())
	r.GET("/trace", func(c *gin.Context) {
		if got := c.GetString("requestID"); got != "req-inbound-1" {
			t.Fatalf("requestID=%q", got)
		}
		if got := c.GetString("traceID"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("traceID=%q", got)
		}
		if got := c.Request.Context().Value("request_id"); got != "req-inbound-1" {
			t.Fatalf("context request_id=%v", got)
		}
		if got := c.Request.Context().Value("trace_id"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("context trace_id=%v", got)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/trace", nil)
	req.Header.Set("X-Request-ID", "req-inbound-1")
	req.Header.Set("X-Trace-ID", "legacy-trace-should-not-win")
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Request-ID"); got != "req-inbound-1" {
		t.Fatalf("response X-Request-ID=%q", got)
	}
	if got := w.Header().Get("X-Trace-ID"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("response X-Trace-ID=%q", got)
	}
}

func TestRequestContextGeneratesRequestIDWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestContext())
	r.GET("/generated", func(c *gin.Context) {
		if got := c.GetString("requestID"); !strings.HasPrefix(got, "req_") {
			t.Fatalf("generated requestID=%q", got)
		}
		if c.GetString("traceID") == "" {
			t.Fatalf("traceID should be generated or derived")
		}
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/generated", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("X-Request-ID"), "req_") {
		t.Fatalf("response generated request id=%q", w.Header().Get("X-Request-ID"))
	}
}
