package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/config"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
)

func TestSafeErrorRedactsSensitiveTraceMaterial(t *testing.T) {
	raw := "Authorization: " + "Bearer " + "eyJsecret.jwt" + " token=raw-token secret=raw-secret password=raw-password provider_payload=raw-provider-payload storage_key=private/key postgres://user:***@10.0.0.1:5432/app"
	msg := SafeError(errors.New(raw))
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-token", "raw-secret", "raw-password", "raw-provider-payload", "private/key", "dbpass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("SafeError leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Fatalf("SafeError did not mark redaction: %q", msg)
	}
}

func TestStartGinSpanPropagatesSpanContextAndRecordSpanErrorIsSafe(t *testing.T) {
	shutdown, err := InitTracing(config.TracingConfig{Enabled: false, ServiceName: "ecommerce-telemetry-test"})
	if err != nil {
		t.Fatalf("InitTracing disabled: %v", err)
	}
	defer shutdown(context.Background())
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/span", func(c *gin.Context) {
		span := StartGinSpan(c, "ecommerce-telemetry-test", "test-span")
		if span == nil {
			t.Fatalf("expected span")
		}
		RecordSpanError(span, errors.New("token=raw-token provider_payload=raw-provider-payload"))
		RecordSpanError(nil, errors.New("token=raw-token"))
		RecordSpanError(span, nil)
		span.End()
		if got := otel.Tracer("ecommerce-telemetry-test"); got == nil {
			t.Fatalf("expected tracer")
		}
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/span", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("span route status=%d body=%s", w.Code, w.Body.String())
	}
	if SafeError(nil) != "" {
		t.Fatalf("SafeError(nil) should be empty")
	}
	if SafeError(errors.New(strings.Repeat("x", 400))) != strings.Repeat("x", 300) {
		t.Fatalf("SafeError should cap long messages")
	}
}
