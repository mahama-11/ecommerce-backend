package telemetry

import (
	"context"
	"fmt"
	"strings"

	"ecommerce-service/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
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
