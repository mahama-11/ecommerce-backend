package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSEmptyAllowedOriginDoesNotEchoArbitraryOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cors(""))
	r.OPTIONS("/cors", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("empty allowed origin echoed arbitrary origin: %q", got)
	}
}

func TestCORSAllowsTraceparentForBrowserTracePropagation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cors("https://console.example.test"))
	r.OPTIONS("/cors", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set("Origin", "https://console.example.test")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowHeaders), "traceparent") {
		t.Fatalf("Access-Control-Allow-Headers=%q, want traceparent", allowHeaders)
	}
}
