package imageruntime

import (
	"testing"

	"ecommerce-service/internal/models"
)

func TestImageJobMetricProviderUsesLowCardinalityProviderCode(t *testing.T) {
	item := &models.EcommerceImageJob{
		ProviderJobID: "provider-job-high-cardinality-123",
		Metadata:      `{"provider_code":"ComfyUI","runtime_provider_code":"volcengine"}`,
	}

	if got := imageJobMetricProvider(item); got != "comfyui" {
		t.Fatalf("expected provider_code label, got %q", got)
	}
}

func TestImageJobMetricProviderFallsBackWithoutProviderJobID(t *testing.T) {
	item := &models.EcommerceImageJob{ProviderJobID: "provider-job-high-cardinality-123", Metadata: `{}`}

	if got := imageJobMetricProvider(item); got != "unknown" {
		t.Fatalf("expected unknown fallback, got %q", got)
	}
	if got := imageJobMetricProvider(nil); got != "unknown" {
		t.Fatalf("expected nil fallback, got %q", got)
	}
}
