package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestClientWithContextPropagatesRequestTraceAndTraceparentToInternalCalls(t *testing.T) {
	seen := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen["request_id"] = r.Header.Get("X-Request-ID")
		seen["trace_id"] = r.Header.Get("X-Trace-ID")
		seen["traceparent"] = r.Header.Get("traceparent")
		seen["internal_service"] = r.Header.Get("X-Internal-Service")
		writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-ctx", "product_code": "ecommerce", "total_balance": 1}, "", "", "")
	}))
	defer server.Close()

	traceID := trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36}
	spanID := trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, Remote: true})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)
	ctx = context.WithValue(ctx, "request_id", "req-outbound-1")
	ctx = context.WithValue(ctx, "trace_id", traceID.String())

	_, err := newTestClient(server).WithContext(ctx).GetWalletSummary("organization", "org-ctx", "ecommerce")
	if err != nil {
		t.Fatalf("GetWalletSummary: %v", err)
	}
	if seen["request_id"] != "req-outbound-1" {
		t.Fatalf("X-Request-ID=%q", seen["request_id"])
	}
	if seen["trace_id"] != traceID.String() {
		t.Fatalf("X-Trace-ID=%q", seen["trace_id"])
	}
	if seen["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent=%q", seen["traceparent"])
	}
	if seen["internal_service"] != "v-ecommerce-backend-test" {
		t.Fatalf("X-Internal-Service=%q", seen["internal_service"])
	}
}
