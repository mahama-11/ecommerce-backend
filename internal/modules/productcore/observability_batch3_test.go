package productcore

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestBatch3ProductDeliveryObservabilityEvents(t *testing.T) {
	source, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}
	content := string(source)
	required := []string{
		"ecommerce.product_center.asset.add.started",
		"ecommerce.product_center.asset.update.finished",
		"ecommerce.product_center.asset.delete.failed",
		"ecommerce.product_center.listing.version.create.started",
		"ecommerce.product_center.listing.version.update.failed",
		"ecommerce.product_center.listing.version.adopt.finished",
		"ecommerce.product_center.listing.version.delete.finished",
		"ecommerce.product_center.export.task.create.finished",
		"ecommerce.product_center.export.package.create.failed",
		"ecommerce.product_center.download.content.started",
		"failure_category",
		"asset_count",
		"listing_version_id",
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			t.Fatalf("handler.go missing observability marker %q", want)
		}
	}
}

func TestClassifyProductDeliveryFailure(t *testing.T) {
	cases := map[string]string{
		"asset not found":                            "asset_precondition",
		"adopted listing version is required":        "listing_precondition",
		"product not found":                          "product_precondition",
		"billing platform client is required":        "billing_precondition",
		"download content is not available":          "download_not_ready",
		"some unexpected repository writeback error": "delivery_operation",
	}
	for input, want := range cases {
		if got := classifyProductDeliveryFailure(errors.New(input)); got != want {
			t.Fatalf("classifyProductDeliveryFailure(%q)=%q want %q", input, got, want)
		}
	}
}
