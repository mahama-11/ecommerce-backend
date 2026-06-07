package observability

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeErrorRedactsSensitiveText(t *testing.T) {
	msg := safeError(errors.New("Bearer abc.def token=secret raw_prompt=hello prompt_plan=plan prompt_text=text provider_payload=blob https://example.test/signed?token=*** image_url=https://example.test/image.png user@example.test"))
	for _, leaked := range []string{"abc.def", "token=secret", "raw_prompt=hello", "prompt_plan=plan", "prompt_text=text", "provider_payload=blob", "https://example.test", "user@example.test"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("safeError leaked %q in %q", leaked, msg)
		}
	}
	for _, want := range []string{"Bearer [redacted]", "token=[redacted]", "raw_prompt=[redacted]", "prompt_plan=[redacted]", "prompt_text=[redacted]", "provider_payload=[redacted]", "[redacted_url]", "[redacted_email]"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("safeError missing %q in %q", want, msg)
		}
	}
}
