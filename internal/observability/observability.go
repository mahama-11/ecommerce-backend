package observability

import (
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/logger"
	"ecommerce-service/pkg/metrics"

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
	module    string
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
	metrics.IncBusinessCounter(eventBase + ".started")
	if module == "visual_workflow" {
		metrics.IncVisualWorkflowRun("started")
	}
	log.Info(eventBase+".started", "status", "started")
	return &Lifecycle{span: span, log: log, eventBase: eventBase, module: module, startedAt: startedAt}
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
	metrics.IncBusinessCounter(l.eventBase + ".finished")
	if l.module == "visual_workflow" {
		metrics.IncVisualWorkflowRun("finished")
	}
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
		sanitizedErr := errors.New(safeError(err))
		l.span.RecordError(sanitizedErr)
		l.span.SetStatus(codes.Error, sanitizedErr.Error())
		attrs = append(attrs, attribute.String("error_message", sanitizedErr.Error()))
	}
	for key, value := range fields {
		attrs = append(attrs, attr(key, value))
	}
	l.span.SetAttributes(attrs...)
	metrics.IncBusinessCounter(l.eventBase + ".failed")
	if l.module == "visual_workflow" {
		metrics.IncVisualWorkflowRun("failed")
	}
	args := append(slogArgs(fields), "status", "failed", "latency_ms", latency, "error_code", errorCode)
	if err != nil {
		args = append(args, "error", safeError(err))
	}
	l.log.Error(l.eventBase+".failed", args...)
	l.span.End()
}

func Event(eventName string, module string, operation string, fields Fields) {
	metrics.IncBusinessCounter(eventName)
	baseFields := Fields{"module": module, "operation": operation}
	for key, value := range fields {
		baseFields[key] = value
	}
	logger.With(slogArgs(baseFields)...).Info(eventName)
}

func ErrorEvent(eventName string, module string, operation string, err error, errorCode string, fields Fields) {
	metrics.IncBusinessCounter(eventName)
	baseFields := Fields{"module": module, "operation": operation, "status": "failed", "error_code": errorCode}
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
		return attribute.String(key, redactSensitive(v))
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
	for _, part := range []string{"token", "secret", "password", "raw_prompt", "prompt_text", "provider_key", "provider_payload", "storage_key", "image_url", "url", "privacy"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	msg := redactSensitive(err.Error())
	if len(msg) > 300 {
		return msg[:300]
	}
	return msg
}

var sensitiveValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)((?:token|secret|password|provider_key|provider_payload|storage_key)=)[^\s,;]+`),
	regexp.MustCompile(`(?i)((?:token|secret|password|provider_key|provider_payload|storage_key)":")[^"]+`),
	regexp.MustCompile(`(?i)((?:postgres|postgresql|mysql)://[^:]+:)[^@\s]+(@)`),
}

func redactSensitive(msg string) string {
	for _, pattern := range sensitiveValuePatterns {
		msg = pattern.ReplaceAllString(msg, `${1}[redacted]${2}`)
	}
	return msg
}
