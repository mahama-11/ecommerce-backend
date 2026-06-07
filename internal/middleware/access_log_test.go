package middleware

import (
	"strings"
	"testing"
)

func TestRedactLogErrorRemovesSensitiveMaterial(t *testing.T) {
	raw := "Authorization: " + "Bearer " + "eyJsecret.jwt" + " token=raw-token secret=raw-secret password=raw-password storage_key=private/provider/raw/object-key postgres://user:" + "dbpass" + "@10.0.0.1:5432/app"
	msg := redactLogError(raw)
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-token", "raw-secret", "raw-password", "private/provider/raw/object-key", "dbpass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("redactLogError leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Fatalf("redactLogError did not mark redaction: %q", msg)
	}
}
