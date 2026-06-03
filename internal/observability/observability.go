package observability

import (
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const ServiceEcommerce = "ecommerce-service"

type Fields map[string]any

type Lifecycle struct {
	span      trace.Span
	log       *slog.Logger
	eventBase string
	startedAt time.Time
}

func StartGin(c *gin.Context, tracerName, spanName, eventBase, module, operation string, fields Fields) *Lifecycle {
	span := telemetry.StartGinSpan(c, tracerName, spanName)
	startedAt := time.Now()
	baseFields := Fields{
		"request_id": c.GetString("requestID"),
		"trace_id":   c.GetString("traceID"),
		"service":    ServiceEcommerce,
		"module":     module,
		"operation":  operation,
	}
	for key, value := range fields {
		baseFields[key] = value
	}
	attrs := []attribute.KeyValue{
		attribute.String("service", ServiceEcommerce),
		attribute.String("module", module),
		attribute.String("operation", operation),
		attribute.String("request_id", c.GetString("requestID")),
		attribute.String("trace_id", c.GetString("traceID")),
	}
	for key, value := range fields {
		attrs = append(attrs, attr(key, value))
	}
	span.SetAttributes(attrs...)
	log := logger.With(slogArgs(baseFields)...)
	log.Info(eventBase+".started", "status", "started")
	return &Lifecycle{span: span, log: log, eventBase: eventBase, startedAt: startedAt}
}

func (l *Lifecycle) Finish(fields Fields) {
	if l == nil {
		return
	}
	latency := time.Since(l.startedAt).Milliseconds()
	attrs := []attribute.KeyValue{attribute.String("status", "finished"), attribute.Int64("latency_ms", latency)}
	for key, value := range fields {
		attrs = append(attrs, attr(key, value))
	}
	l.span.SetAttributes(attrs...)
	l.log.Info(l.eventBase+".finished", append(slogArgs(fields), "status", "finished", "latency_ms", latency)...)
	l.span.End()
}

func (l *Lifecycle) Fail(err error, errorCode string, fields Fields) {
	if l == nil {
		return
	}
	latency := time.Since(l.startedAt).Milliseconds()
	attrs := []attribute.KeyValue{attribute.String("status", "failed"), attribute.String("error_code", errorCode), attribute.Int64("latency_ms", latency)}
	if err != nil {
		safe := safeError(err)
		l.span.RecordError(errors.New(safe))
		l.span.SetStatus(codes.Error, safe)
		attrs = append(attrs, attribute.String("error_message", safe))
	}
	for key, value := range fields {
		attrs = append(attrs, attr(key, value))
	}
	l.span.SetAttributes(attrs...)
	args := append(slogArgs(fields), "status", "failed", "latency_ms", latency, "error_code", errorCode)
	if err != nil {
		args = append(args, "error", safeError(err))
	}
	l.log.Error(l.eventBase+".failed", args...)
	l.span.End()
}

func Event(eventName string, module string, operation string, fields Fields) {
	baseFields := Fields{"service": ServiceEcommerce, "module": module, "operation": operation}
	for key, value := range fields {
		baseFields[key] = value
	}
	logger.With(slogArgs(baseFields)...).Info(eventName)
}

func ErrorEvent(eventName string, module string, operation string, err error, errorCode string, fields Fields) {
	baseFields := Fields{"service": ServiceEcommerce, "module": module, "operation": operation, "status": "failed", "error_code": errorCode}
	if err != nil {
		baseFields["error"] = safeError(err)
	}
	for key, value := range fields {
		baseFields[key] = value
	}
	logger.With(slogArgs(baseFields)...).Error(eventName)
}

func slogArgs(fields Fields) []any {
	args := make([]any, 0, len(fields)*2)
	for key, value := range fields {
		if key == "" || value == nil || forbiddenField(key) {
			continue
		}
		args = append(args, key, value)
	}
	return args
}

func attr(key string, value any) attribute.KeyValue {
	if forbiddenField(key) {
		return attribute.String(key, "[redacted]")
	}
	switch v := value.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case bool:
		return attribute.Bool(key, v)
	default:
		return attribute.String(key, "")
	}
}

func forbiddenField(key string) bool {
	k := strings.ToLower(key)
	for _, part := range []string{"token", "secret", "password", "authorization", "api_key", "apikey", "auth_header", "cookie", "session_cookie", "prompt", "provider_key", "provider_payload", "storage_key", "image_url", "url", "privacy", "payload", "card", "payment_secret", "ledger_id", "wallet_ledger_id", "reference_id", "source_id", "source_ref", "source_path"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

var (
	bearerPattern   = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)
	urlPattern      = regexp.MustCompile(`(?i)https?://[^\s,]+`)
	secretKVPattern = regexp.MustCompile(`(?i)(token|secret|password|authorization|api[_-]?key)[:=]([^\s,]+)`)
	promptKVPattern = regexp.MustCompile(`(?i)(raw_prompt|prompt_text|prompt_plan|prompt|provider_payload)[:=]([^\s,]+)`)
	emailPattern    = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

func safeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = bearerPattern.ReplaceAllString(msg, "Bearer [redacted]")
	msg = urlPattern.ReplaceAllString(msg, "[redacted_url]")
	msg = secretKVPattern.ReplaceAllString(msg, "$1=[redacted]")
	msg = promptKVPattern.ReplaceAllString(msg, "$1=[redacted]")
	msg = emailPattern.ReplaceAllString(msg, "[redacted_email]")
	if len(msg) > 300 {
		return msg[:300]
	}
	return msg
}
