package telemetry

import (
	"context"
	"strings"
	"testing"

	"ecommerce-service/internal/config"
)

func TestInitTracingDisabledInstallsNoopProviderAndShutdown(t *testing.T) {
	shutdown, err := InitTracing(config.TracingConfig{Enabled: false, ServiceName: "ecommerce-telemetry-test"})
	if err != nil {
		t.Fatalf("InitTracing disabled: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown disabled tracing: %v", err)
	}
}

func TestInitTracingRejectsUnsupportedBackendAndMissingOTLPEndpoint(t *testing.T) {
	if _, err := InitTracing(config.TracingConfig{Enabled: true, Backend: "unsupported", ServiceName: "ecommerce-telemetry-test"}); err == nil || !strings.Contains(err.Error(), "unsupported tracing backend") {
		t.Fatalf("expected unsupported backend error, got %v", err)
	}
	if _, err := InitTracing(config.TracingConfig{Enabled: true, Backend: "otlp", ServiceName: "ecommerce-telemetry-test"}); err == nil || !strings.Contains(err.Error(), "otlp_endpoint") {
		t.Fatalf("expected missing otlp endpoint error, got %v", err)
	}
}
