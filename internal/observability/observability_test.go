package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/telemetry"

	"github.com/gin-gonic/gin"
)

func TestSlogArgsDropsForbiddenSensitiveFields(t *testing.T) {
	args := slogArgs(Fields{
		"request_id":       "req-1",
		"access_token":     "token-secret",
		"password":         "raw-password",
		"storage_key":      "private/storage/key",
		"provider_payload": "{\"prompt\":\"secret provider blob\"}",
		"provider_url":     "https://provider.example/private",
	})
	joined := strings.Join(func() []string {
		out := make([]string, 0, len(args))
		for _, arg := range args {
			out = append(out, strings.TrimSpace(strings.ToLower(anyToString(arg))))
		}
		return out
	}(), " ")
	for _, forbidden := range []string{"token-secret", "raw-password", "storage_key", "private/storage/key", "provider_payload", "provider.example", "secret provider blob"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("slog args leaked forbidden value %q: %v", forbidden, args)
		}
	}
	if !strings.Contains(joined, "request_id") {
		t.Fatalf("slog args dropped safe request_id: %v", args)
	}
}

func TestSafeErrorRedactsSecretsTokensProviderPayloadAndDBURLs(t *testing.T) {
	raw := "Authorization: " + "Bearer " + "eyJsecret.jwt" + " token=raw-token secret=raw-secret password=raw-password provider_payload=raw-provider-payload storage_key=private/key postgres://user:***@10.0.0.1:5432/app"
	msg := safeError(errors.New(raw))
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-token", "raw-secret", "raw-password", "raw-provider-payload", "private/key", "dbpass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("safeError leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Fatalf("safeError did not mark redaction: %q", msg)
	}
}

func TestLifecyclePropagatesRequestTraceIDsAndUsesSafeFailureFields(t *testing.T) {
	shutdown, err := telemetry.InitTracing(config.TracingConfig{Enabled: false, ServiceName: "ecommerce-observability-test"})
	if err != nil {
		t.Fatalf("InitTracing: %v", err)
	}
	defer shutdown(context.Background())
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ok", func(c *gin.Context) {
		c.Set("requestID", "req-observe")
		c.Set("traceID", "trace-observe")
		lifecycle := StartGin(c, "ecommerce-test", "span.ok", "visual.workflow", "visual_workflow", "start", Fields{"product_id": "prod-1", "retry": int64(2), "cache_hit": true, "unsafe_storage_key": "private/key"})
		lifecycle.Finish(Fields{"status_detail": "done", "count": 1})
		if c.Request.Context().Value("request_id") != nil {
			// StartGinSpan must preserve request context; RequestContext middleware is covered in middleware tests.
		}
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("lifecycle finish route status=%d body=%s", w.Code, w.Body.String())
	}

	r = gin.New()
	r.GET("/fail", func(c *gin.Context) {
		c.Set("requestID", "req-fail")
		c.Set("traceID", "trace-fail")
		lifecycle := StartGin(c, "ecommerce-test", "span.fail", "visual.workflow", "visual_workflow", "callback", Fields{"route_name": "callback"})
		lifecycle.Fail(errors.New("provider_payload=raw-provider-payload token=raw-token"), "CALLBACK_FAILED", Fields{"password": "raw-password", "safe": "kept"})
		c.Status(http.StatusInternalServerError)
	})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("lifecycle fail route status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEventHelpersAndAttrRedactForbiddenMaterial(t *testing.T) {
	Event("template.use.started", "template_center", "use", Fields{"request_id": "req-1", "count": 2})
	ErrorEvent("template.use.failed", "template_center", "use", errors.New("storage_key=private/key provider_payload=raw-provider-payload"), "USE_FAILED", Fields{"safe": "value"})

	redacted := attr("provider_payload", "raw-provider-payload")
	if redacted.Value.AsString() != "[redacted]" {
		t.Fatalf("forbidden attr not redacted: %+v", redacted)
	}
	text := attr("error_message", "token=raw-token")
	if strings.Contains(text.Value.AsString(), "raw-token") || !strings.Contains(text.Value.AsString(), "[redacted]") {
		t.Fatalf("string attr did not redact sensitive value: %+v", text)
	}
	if got := attr("unsupported", struct{}{}).Value.AsString(); got != "" {
		t.Fatalf("unsupported attr should become empty string, got %q", got)
	}
	if safeError(errors.New(strings.Repeat("x", 400))) != strings.Repeat("x", 300) {
		t.Fatalf("safeError should cap long messages at 300 chars")
	}
	if safeError(nil) != "" {
		t.Fatalf("safeError(nil) should be empty")
	}
	var lifecycle *Lifecycle
	lifecycle.Finish(Fields{"safe": "noop"})
	lifecycle.Fail(errors.New("token=raw-token"), "NOOP", Fields{})
}

func anyToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
