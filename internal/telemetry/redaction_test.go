package telemetry

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeErrorRedactsSensitiveTraceMaterial(t *testing.T) {
	raw := "Authorization: " + "Bearer " + "eyJsecret.jwt" + " token=raw-token secret=raw-secret password=raw-password storage_key=private/key postgres://user:***@10.0.0.1:5432/app"
	msg := SafeError(errors.New(raw))
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-token", "raw-secret", "raw-password", "private/key", "dbpass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("SafeError leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Fatalf("SafeError did not mark redaction: %q", msg)
	}
}
