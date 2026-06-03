package templatecenter

import (
	"errors"
	"os"
	"strings"
	"testing"

	"ecommerce-service/internal/observability"
)

func TestBatch3TemplateObservabilityEvents(t *testing.T) {
	source, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"ecommerce.template_center.template.use.started",
		"ecommerce.template_center.template.use.finished",
		"ecommerce.template_center.template.copy.failed",
		"ecommerce.template_center.template.favorite.started",
		"failure_category",
		"template_id",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("handler.go missing observability marker %q", want)
		}
	}
}

func TestTemplateFailureFieldsClassifiesPreconditions(t *testing.T) {
	fields := templateFailureFields(observability.Fields{}, errors.New("template not found or not published"))
	if fields["failure_category"] != "template_precondition" {
		t.Fatalf("failure_category=%v", fields["failure_category"])
	}
	fields = templateFailureFields(observability.Fields{}, errors.New("tool route unavailable"))
	if fields["failure_category"] != "template_route_precondition" {
		t.Fatalf("failure_category=%v", fields["failure_category"])
	}
}
