package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRedactLogErrorRemovesSensitiveMaterial(t *testing.T) {
	raw := "Authorization: " + "Bearer " + "eyJsecret.jwt" + " token=raw-token secret=raw-secret password=raw-password provider_payload=raw-provider-payload storage_key=private/provider/raw/object-key postgres://user:***@10.0.0.1:5432/app"
	msg := redactLogError(raw)
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-token", "raw-secret", "raw-password", "raw-provider-payload", "private/provider/raw/object-key", "dbpass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("redactLogError leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Fatalf("redactLogError did not mark redaction: %q", msg)
	}
}

func TestAccessLogDoesNotEmitSensitiveErrorMaterial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writePipe
	defer func() { os.Stdout = oldStdout }()

	r := gin.New()
	r.Use(RequestContext(), AccessLog())
	r.GET("/callback/:id", func(c *gin.Context) {
		_ = c.Error(errors.New("Authorization: Bearer eyJsecret.jwt password=raw-password storage_key=private/object provider_payload=raw-provider-payload"))
		c.Status(http.StatusBadGateway)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback/raw-id?token=raw-query-token", nil)
	req.Header.Set("X-Request-ID", "req-log")
	req.Header.Set("X-Trace-ID", "trace-log")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := writePipe.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	out, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	logOutput := string(out)
	for _, forbidden := range []string{"eyJsecret.jwt", "raw-password", "private/object", "raw-provider-payload", "raw-query-token"} {
		if strings.Contains(logOutput, forbidden) {
			t.Fatalf("access log leaked %q in output: %s", forbidden, logOutput)
		}
	}
	if !strings.Contains(logOutput, "req-log") || !strings.Contains(logOutput, "trace-log") || !strings.Contains(logOutput, "[redacted]") {
		t.Fatalf("access log missing expected safe context/redaction: %s", logOutput)
	}
}
