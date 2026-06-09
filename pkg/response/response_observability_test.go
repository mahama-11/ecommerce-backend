package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestErrorResponseIncludesRequestTraceAndSemanticErrorFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/error", func(c *gin.Context) {
		c.Set("requestID", "req-response-1")
		c.Set("traceID", "trace-response-1")
		JSONErrorSemantic(c, CodeForbidden, "forbidden", "ECOM_SCOPE_DENIED", "Check organization access.")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/error", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	for key, want := range map[string]string{
		"request_id": "req-response-1",
		"trace_id":   "trace-response-1",
		"error_code": "ECOM_SCOPE_DENIED",
		"error_hint": "Check organization access.",
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s]=%v want %q body=%s", key, got, want, w.Body.String())
		}
	}
}
