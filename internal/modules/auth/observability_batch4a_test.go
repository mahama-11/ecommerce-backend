package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBatch4AAuthObservabilityEvents(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(file), "handler.go"))
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		"ecommerce.auth.",
		"register",
		"login",
		"session.verify",
		"request_validation",
		"credential_invalid",
		"user_exists",
		"session_invalid",
		"token_issue_failed",
		"platform_sync_failed",
		"auth_internal",
		"has_email",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("handler.go missing observability marker %q", want)
		}
	}
	for _, forbidden := range []string{"password", "cookie", "Authorization"} {
		if strings.Contains(got, "\""+forbidden+"\"") {
			t.Fatalf("handler.go must not log sensitive field %q", forbidden)
		}
	}
}
