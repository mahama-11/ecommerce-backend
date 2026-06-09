package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBusinessMetricsExposeLowCardinalityBackendStabilitySignals(t *testing.T) {
	Configure("ecommerce_test", "service", []float64{0.01, 0.1})
	IncBusinessCounter("ecommerce.platform.call.finished")
	RecordPlatformCall("/runtime/jobs", "200", "", 15*time.Millisecond)
	IncImageJob("completed", "comfyui", "product_composite")
	IncVisualWorkflowRun("completed")
	IncBillingChargeSession("settled")
	IncRuntimeCallback("completed")
	IncProviderCall("comfyui", "image_generation", "completed")

	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	text := string(body)
	for _, name := range []string{
		"ecommerce_test_service_business_events_total",
		"ecommerce_test_service_platform_calls_total",
		"ecommerce_test_service_platform_call_duration_seconds",
		"ecommerce_test_service_image_jobs_total",
		"ecommerce_test_service_visual_workflow_runs_total",
		"ecommerce_test_service_billing_charge_sessions_total",
		"ecommerce_test_service_runtime_callbacks_total",
		"ecommerce_test_service_provider_calls_total",
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("metrics output missing %s:\n%s", name, text)
		}
	}
	for _, forbiddenLabel := range []string{"user_id=", "org_id=", "request_id=", "product_id="} {
		if strings.Contains(text, forbiddenLabel) {
			t.Fatalf("metrics output contains high-cardinality label %s:\n%s", forbiddenLabel, text)
		}
	}
}
