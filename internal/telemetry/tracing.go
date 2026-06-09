package telemetry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"ecommerce-service/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials/insecure"
)

func InitTracing(cfg config.TracingConfig) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !cfg.Enabled {
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return tp.Shutdown, nil
	}

	exp, err := newTraceExporter(context.Background(), cfg)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentName(cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func RecordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	sanitized := errors.New(SafeError(err))
	span.RecordError(sanitized)
	span.SetStatus(codes.Error, sanitized.Error())
}

func SafeError(err error) string {
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

func newTraceExporter(ctx context.Context, cfg config.TracingConfig) (sdktrace.SpanExporter, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "jaeger"
	}
	switch backend {
	case "otlp", "tempo":
		endpoint := strings.TrimSpace(cfg.OTLPEndpoint)
		if endpoint == "" {
			return nil, fmt.Errorf("monitoring.tracing.otlp_endpoint is required when backend=%s", backend)
		}
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
		if cfg.OTLPInsecure {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
		}
		return otlptracegrpc.New(ctx, opts...)
	case "jaeger":
		return jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(cfg.JaegerEndpoint)))
	default:
		return nil, fmt.Errorf("unsupported tracing backend %q", cfg.Backend)
	}
}
