package promptcenter

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestBatch3PromptPreviewObservabilityEvents(t *testing.T) {
	source, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"ecommerce.prompt_center.preview.started",
		"ecommerce.prompt_center.preview.finished",
		"ecommerce.prompt_center.preview.failed",
		"failure_category",
		"source_asset_count",
		"variable_count",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("handler.go missing observability marker %q", want)
		}
	}
	for _, forbidden := range []string{"raw_prompt", "prompt_text", "image_url"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("handler observability must not include forbidden field %q", forbidden)
		}
	}
}

func TestClassifyPromptPreviewFailure(t *testing.T) {
	cases := map[string]string{
		"bound product not found":                 "product_precondition",
		"template not found or not published":     "template_precondition",
		"source asset not found":                  "source_asset_precondition",
		"prompt center dependencies are required": "dependency_precondition",
	}
	for input, want := range cases {
		if got := classifyPromptPreviewFailure(errors.New(input)); got != want {
			t.Fatalf("classifyPromptPreviewFailure(%q)=%q want %q", input, got, want)
		}
	}
}
