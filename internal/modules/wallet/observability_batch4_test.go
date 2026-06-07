package wallet

import (
	"os"
	"strings"
	"testing"
)

func TestBatch4WalletObservabilityMarkers(t *testing.T) {
	body, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(body)
	for _, want := range []string{
		"ecommerce.wallet.summary",
		"wallet_summary_failed",
		"ecommerce.wallet.history",
		"wallet_history_failed",
		"request_id",
		"trace_id",
		"org_id",
		"user_id",
		"limit",
		"count",
		"account_present",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("wallet observability missing marker %q", want)
		}
	}
	for _, forbidden := range []string{"provider_payload", "payment_secret", "token", "url"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("wallet observability must not include forbidden field marker %q", forbidden)
		}
	}
}
