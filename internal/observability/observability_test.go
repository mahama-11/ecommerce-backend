package observability

import (
	"errors"
	"strings"
	"testing"
)

func TestSlogArgsDropsForbiddenSensitiveFields(t *testing.T) {
	args := slogArgs(Fields{
		"request_id":   "req-1",
		"access_token": "token-secret",
		"storage_key":  "private/storage/key",
		"provider_url": "https://provider.example/private",
	})
	joined := strings.Join(func() []string {
		out := make([]string, 0, len(args))
		for _, arg := range args {
			out = append(out, strings.TrimSpace(strings.ToLower(anyToString(arg))))
		}
		return out
	}(), " ")
	if !strings.Contains(joined, "request_id") || strings.Contains(joined, "token-secret") || strings.Contains(joined, "storage_key") || strings.Contains(joined, "provider.example") {
		t.Fatalf("slog args did not redact/drop forbidden fields: %v", args)
	}
}

func TestSafeErrorRedactsSecretsTokensAndDBURLs(t *testing.T) {
	raw := "Authorization: " + "Bearer " + "eyJsecret.jwt" + " token=raw-token secret=raw-secret password=raw-password postgres://user:***@10.0.0.1:5432/app"
	msg := safeError(errors.New(raw))
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-token", "raw-secret", "raw-password", "dbpass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("safeError leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Fatalf("safeError did not mark redaction: %q", msg)
	}
}

func anyToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
