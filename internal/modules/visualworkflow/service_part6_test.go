package visualworkflow

import (
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
)

func TestSaveGenerationVersionAsTemplateValidation(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	if err := db.AutoMigrate(&models.SavedTemplate{}); err != nil {
		t.Fatalf("migrate saved template: %v", err)
	}
	service.WithWorkspaceRepository(repository.NewWorkspaceRepository(db))
	_, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_tpl_validate")
	completedStatus := "completed"
	completedStage := "result_available"
	selectedAssetID := "asset_tpl_validate"
	version, err := service.UpdateGenerationVersion("org_1", session.ID, version.VersionID, UpdateGenerationVersionRequest{Status: &completedStatus, Stage: &completedStage, ResultAssets: []ResultAssetDTO{{AssetID: selectedAssetID, AssetContentURL: "/api/v1/ecommerce/assets/" + selectedAssetID + "/content"}}, SelectedResultAssetID: &selectedAssetID})
	if err != nil {
		t.Fatalf("prepare completed version: %v", err)
	}
	if _, err := service.SaveGenerationVersionAsTemplate("", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{}); err == nil {
		t.Fatalf("expected user required")
	}
	if _, err := service.SaveGenerationVersionAsTemplate("user_1", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{AssetID: "not_in_version"}); err == nil {
		t.Fatalf("expected non-version asset rejection")
	}
	version.Status = "queued"
	versions := []GenerationVersionDTO{*version}
	encoded, err := marshalGenerationVersions(versions)
	if err != nil {
		t.Fatalf("marshal versions: %v", err)
	}
	session.GenerationVersionsJSON = encoded
	if err := service.repo.SaveSession(session); err != nil {
		t.Fatalf("save queued version: %v", err)
	}
	if _, err := service.SaveGenerationVersionAsTemplate("user_1", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{}); err == nil {
		t.Fatalf("expected incomplete version rejection")
	}
}

func TestGenerationProviderCodeAllowsFrontendSelectableProviders(t *testing.T) {
	metadata := map[string]any{
		"ui_execution_config": map[string]any{
			"provider_config": map[string]any{"generation_provider_code": "gemini_image_generation"},
		},
	}
	if got := generationProviderCode(metadata); got != "gemini_image_generation" {
		t.Fatalf("expected gemini provider code, got %q", got)
	}
	metadata = map[string]any{
		"ui_execution_config": map[string]any{
			"provider_config": map[string]any{"generation_provider_code": "minimax_image_generation"},
		},
	}
	if got := generationProviderCode(metadata); got != "minimax_image_generation" {
		t.Fatalf("expected minimax provider code, got %q", got)
	}
	metadata = map[string]any{
		"execution_config": map[string]any{
			"provider_config": map[string]any{"generation_provider_code": "minimax_image_generation"},
		},
	}
	if got := generationProviderCode(metadata); got != "minimax_image_generation" {
		t.Fatalf("expected minimax provider code from execution_config, got %q", got)
	}
	metadata["provider_code"] = "comfyui_bridge"
	if got := generationProviderCode(metadata); got != "comfyui_bridge" {
		t.Fatalf("expected explicit comfyui provider code, got %q", got)
	}
	metadata["provider_code"] = "gemini_visual_understanding"
	if got := generationProviderCode(metadata); got != "" {
		t.Fatalf("visual-understanding provider must not be accepted for generation, got %q", got)
	}
}
