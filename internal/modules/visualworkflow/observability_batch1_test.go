package visualworkflow

import (
	"errors"
	"testing"

	"ecommerce-service/internal/models"
)

func TestBatch1GenerationRuntimeJobObservabilityFieldsAreSafe(t *testing.T) {
	session := &models.EcommerceVisualWorkflowSession{ID: "session-1", OrganizationID: "org-1", UserID: "user-1", ProductID: "product-1", SKUCode: "SKU-1"}
	version := &GenerationVersionDTO{VersionID: "gv-1", RuntimeJobID: "runtime-1", Status: "processing", Stage: "running"}

	fields := generationRuntimeJobObservabilityFields(session, version, "comfyui_bridge", "charge-1")

	if fields["session_id"] != "session-1" || fields["generation_version_id"] != "gv-1" || fields["runtime_job_id"] != "runtime-1" || fields["provider"] != "comfyui_bridge" {
		t.Fatalf("missing required runtime job observability fields: %#v", fields)
	}
	if _, ok := fields["input_manifest"]; ok {
		t.Fatalf("runtime observability fields must not expose provider payloads: %#v", fields)
	}
}

func TestBatch1WritebackSelectedAssetFailureCategory(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("generation version not found in session"), "generation_version_missing"},
		{errors.New("product not found in organization"), "product_relation_missing"},
		{errors.New("selected result asset is required"), "selected_asset_missing"},
		{errors.New("asset_id is not in this generation version result_assets"), "selected_asset_not_in_version"},
		{errors.New("writeback metadata contains execution-owned field \"storage_key\""), "unsafe_writeback_metadata"},
	}
	for _, tc := range cases {
		if got := writebackSelectedAssetFailureCategory(tc.err); got != tc.want {
			t.Fatalf("category for %q = %q, want %q", tc.err, got, tc.want)
		}
	}
}
