package commercial

import (
	"os"
	"strings"
	"testing"
)

func TestBatch4CommercialObservabilityMarkers(t *testing.T) {
	handler, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	service, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(handler) + string(service)
	for _, want := range []string{
		"ecommerce.commercial.order.create",
		"commercial_order_create_failed",
		"ecommerce.commercial.payment.confirm",
		"commercial_payment_confirm_failed",
		"ecommerce.commercial.payment.idempotency.hit",
		"request_id",
		"trace_id",
		"org_id",
		"user_id",
		"order_id",
		"product_code",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("commercial observability missing marker %q", want)
		}
	}
	for _, forbidden := range []string{"card", "provider_payload", "payment_secret", "token", "url"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("commercial observability must not include forbidden field marker %q", forbidden)
		}
	}
}
