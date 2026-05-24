package visualworkflow

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func hasBlockerTarget(blockers []ReadinessBlocker, code, target string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code && blocker.Target == target {
			return true
		}
	}
	return false
}

func TestGenerationVersionPersistenceRefinementSelectionAndReadiness(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode, ToolSlug: "hero-generator"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = service.UpdateSession("org_1", session.ID, UpdateSessionRequest{
		IntentSpec: &IntentSpecDTO{SchemaVersion: intentSpecSchemaVersion, SceneType: "hero", Requirements: map[string]any{"tone": "premium"}},
		PromptPlan: &PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "blocked", PromptID: "prompt_1", Variables: map[string]any{"style": "clean"}},
	})
	if err != nil {
		t.Fatalf("update session prompt/intent: %v", err)
	}

	created, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{
		IdempotencyKey:        "idem-1",
		ParentVersionID:       "gv_parent",
		SourceVersionID:       "gv_source",
		RefinementInstruction: "make background brighter",
		MaskAssetID:           "mask_1",
	})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	if created.VersionID == "" || created.PromptID != "prompt_1" || created.PromptPlanStatus != "blocked" {
		t.Fatalf("missing prompt snapshot: %+v", created)
	}
	if created.IntentSpecSnapshot["scene_type"] != "hero" || created.Status != "contract_needed" || !containsReadinessBlocker(created.Blockers, "CONTRACT_NEEDED") {
		t.Fatalf("missing intent snapshot/contract blocker: %+v", created)
	}
	if created.ParentVersionID != "gv_parent" || created.SourceVersionID != "gv_source" || created.RefinementInstruction == "" || created.MaskAssetID != "mask_1" {
		t.Fatalf("missing refinement metadata: %+v", created)
	}

	again, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatalf("idempotent create: %v", err)
	}
	if again.VersionID != created.VersionID {
		t.Fatalf("idempotency did not return existing version: first=%s second=%s", created.VersionID, again.VersionID)
	}
	versions, err := service.ListGenerationVersions("org_1", session.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("expected one version after idempotent create, versions=%+v err=%v", versions, err)
	}

	status := "processing"
	stage := "running"
	progress := 42
	updated, err := service.UpdateGenerationVersion("org_1", session.ID, created.VersionID, UpdateGenerationVersionRequest{
		Status:       &status,
		Stage:        &stage,
		Progress:     &progress,
		ResultAssets: []ResultAssetDTO{{AssetID: "asset_result_1", AssetContentURL: "/api/v1/ecommerce/assets/asset_result_1/content", Role: "primary"}},
		Metadata:     map[string]any{"note": "candidate"},
	})
	if err != nil {
		t.Fatalf("update generation version: %v", err)
	}
	if updated.Status != "processing" || updated.Stage != "running" || updated.Progress != 42 || len(updated.ResultAssets) != 1 {
		t.Fatalf("update not persisted: %+v", updated)
	}
	selected, err := service.SelectGenerationVersion("org_1", session.ID, created.VersionID, SelectGenerationVersionRequest{SelectedResultAssetID: "asset_result_1"})
	if err != nil {
		t.Fatalf("select generation version: %v", err)
	}
	if selected.SelectedResultAssetID != "asset_result_1" || selected.Stage != "selected" || !selected.ResultAssets[0].Selected {
		t.Fatalf("selection not persisted: %+v", selected)
	}
	if _, err := service.SelectGenerationVersion("org_1", session.ID, created.VersionID, SelectGenerationVersionRequest{SelectedResultAssetID: "missing"}); err == nil {
		t.Fatalf("expected missing selected_result_asset_id rejection")
	}
	if _, err := service.GetGenerationVersion("other_org", session.ID, created.VersionID); err == nil {
		t.Fatalf("expected org-scoped get rejection")
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if len(view.GenerationVersions) != 1 || view.GenerationVersions[0].SelectedResultAssetID != "asset_result_1" {
		t.Fatalf("stage view missing generation versions: %+v", view.GenerationVersions)
	}
	if view.Readiness.Generation != models.VisualReadinessBlocked || !containsBlockerTarget(view.Readiness.Blockers, "CONTRACT_NEEDED", "generation") {
		t.Fatalf("stage view weakened generation readiness: %+v", view.Readiness)
	}
}

func TestGenerationVersionRejectsNestedRuntimeProviderStorageArtifacts(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_gen_artifacts", "org_1", "SKU-GEN-ARTIFACTS")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Metadata: map[string]any{"nested": map[string]any{"runtime_job_id": "fake-runtime"}}}); err == nil {
		t.Fatalf("expected nested runtime_job_id metadata rejection")
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{ResultAssets: []ResultAssetDTO{{AssetID: "asset_1", AssetContentURL: "/content", Metadata: map[string]any{"storage_key": "secret/storage"}}}}); err == nil {
		t.Fatalf("expected result asset storage_key metadata rejection")
	}
	if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{PromptPlan: &PromptPlanDTO{Status: "ready", Variables: map[string]any{"provider_job_id": "fake-provider"}}}); err == nil {
		t.Fatalf("expected prompt plan provider artifact rejection")
	}
}

func TestGenerationRuntimeManifestSanitizesPersistedPromptArtifacts(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_gen_manifest", "org_1", "SKU-GEN-MANIFEST")
	orchestrator := &fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_generation", Status: "available", Available: true, ContractStatus: "ready"})}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	session.PromptPlanJSON = mustJSON(PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", Variables: map[string]any{"style": "clean", "storage_key": "secret/storage"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_1", Metadata: map[string]any{"provider_job_id": "fake-provider"}}}})
	if err := service.repo.SaveSession(session); err != nil {
		t.Fatalf("save stale prompt plan: %v", err)
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{IdempotencyKey: "manifest-sanitize"}); err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	if len(orchestrator.createInputs) != 0 {
		t.Fatalf("stale prompt artifacts without Prompt Center snapshot should fail closed before runtime create: %+v", orchestrator.createInputs)
	}
}

func TestGenerationVersionRejectsArtifactsBeforeRuntimeCreate(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_gen_prevalidate", "org_1", "SKU-GEN-PREVALIDATE")
	orchestrator := &fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_generation", Status: "available", Available: true, ContractStatus: "ready"})}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Metadata: map[string]any{"nested": map[string]any{"provider_job_id": "fake-provider"}}}); err == nil {
		t.Fatalf("expected provider artifact rejection")
	}
	if len(orchestrator.createInputs) != 0 {
		t.Fatalf("invalid generation payload should not create runtime job: %+v", orchestrator.createInputs)
	}
}

func TestCreateGenerationFanoutRepeatedExplicitRunsUseFreshVersionIdempotency(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_repeat", "org_fanout_repeat", "SKU-FANOUT-REPEAT")
	if err := db.Create(&models.EcommerceAsset{ID: "asset_fanout_repeat", OrganizationID: "org_fanout_repeat", UserID: "user_fanout_repeat", AssetType: "image", SourceType: "upload", StorageKey: "source/fanout-repeat.png", MimeType: "image/png", Width: 640, Height: 640, FileName: "fanout-repeat.png", Metadata: "{}"}).Error; err != nil {
		t.Fatalf("seed source asset: %v", err)
	}
	session, err := service.CreateSession("user_fanout_repeat", "org_fanout_repeat", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.PromptPlanJSON = `{"schema_version":"visual_prompt_plan.v1","status":"ready","prompt_id":"prompt_fanout_repeat","variables":{"composed_prompt_text":"ready prompt"}}`
	if err := db.Save(session).Error; err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	req := CreateGenerationFanoutRequest{
		IdempotencyKey: "same-ui-batch-key",
		TemplateSlots:  []GenerationFanoutTemplateSlotRequest{{SourceAssetID: "asset_fanout_repeat", TemplateID: "amazon-hero", SceneTag: "主图"}},
	}
	first, err := service.CreateGenerationFanout("org_fanout_repeat", session.ID, req)
	if err != nil {
		t.Fatalf("create first fanout: %v", err)
	}
	second, err := service.CreateGenerationFanout("org_fanout_repeat", session.ID, req)
	if err != nil {
		t.Fatalf("create second fanout: %v", err)
	}
	firstVersion := first.Items[0].GenerationVersion
	secondVersion := second.Items[0].GenerationVersion
	if firstVersion.VersionID == secondVersion.VersionID {
		t.Fatalf("repeated explicit fanout should create a fresh generation version, got same %s", firstVersion.VersionID)
	}
	firstKey := metadataString(firstVersion.Metadata, "idempotency_key")
	secondKey := metadataString(secondVersion.Metadata, "idempotency_key")
	if firstKey == "" || secondKey == "" || firstKey == secondKey {
		t.Fatalf("expected fresh per-run idempotency keys, got first=%q second=%q", firstKey, secondKey)
	}
}

func TestCreateGenerationVersionCreatesPlatformRuntimeJobWhenCapabilityReady(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_gen_ready", "org_1", "SKU-GEN-READY")
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:     testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_generation", Status: "available", Available: true, ContractStatus: "ready", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_image_generation", Configured: true}}),
		runtimeJob: &platform.RuntimeJob{ID: "runtime-gen-1", ProductCode: "ecommerce", TaskType: "image_generation", OrganizationID: "org_1", UserID: "user_1", SourceType: "visual_generation", Status: "processing", Stage: "runtime_queued", StageMessage: "accepted"},
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	service = service.WithPromptRepository(repository.NewPromptCenterRepository(db))
	if err := db.Create(&models.EcommerceAsset{ID: "asset_gen_ready", OrganizationID: "org_1", UserID: "user_1", AssetType: "image", SourceType: "upload", StorageKey: "source/gen-ready.png", MimeType: "image/png", Width: 640, Height: 640, FileName: "gen-ready.png", Metadata: "{}"}).Error; err != nil {
		t.Fatalf("seed prompt asset: %v", err)
	}
	if err := db.Create(&models.EcommercePromptRun{ID: "prompt_gen_ready", OrganizationID: "org_1", UserID: "user_1", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_gen", TemplateVersionID: "tv_gen", TemplateCode: "tpl-gen", SceneType: "hero", Status: "validated", SchemaVersion: "prompt.schema.v1", ContentHash: "hash", SourceMapHash: "sourcehash", InputPayloadJSON: "{}", SourceAssetBindingsJSON: `[{"slot":"hero","asset_id":"asset_gen_ready"}]`, VariablesJSON: "{}", CompiledPromptJSON: `{"strategy":"template","final_prompt":"ready prompt","final_negative_prompt":"no blur","sections":[]}`, SourceMapJSON: "{}", ValidationResultJSON: `{"valid":true}`}).Error; err != nil {
		t.Fatalf("seed prompt run: %v", err)
	}
	created, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{IdempotencyKey: "gen-ready-1", PromptID: "prompt_gen_ready"})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	if created.RuntimeJobID != "runtime-gen-1" || created.Status != "processing" || created.Stage != "running" {
		t.Fatalf("expected platform runtime projection, got %+v", created)
	}
	if len(orchestrator.calls) != 1 || orchestrator.calls[0].productCode != "ecommerce" || orchestrator.calls[0].taskType != "image_generation" {
		t.Fatalf("unexpected capability calls: %+v", orchestrator.calls)
	}
	if len(orchestrator.createInputs) != 1 {
		t.Fatalf("expected one runtime create call, got %d", len(orchestrator.createInputs))
	}
	input := orchestrator.createInputs[0]
	if input.ProductCode != "ecommerce" || input.TaskType != "image_generation" || input.SourceType != "visual_generation" || input.SourceID != created.VersionID {
		t.Fatalf("unexpected runtime create input: %+v", input)
	}
	if input.OrganizationID != "org_1" || input.UserID != "user_1" || input.IdempotencyKey == "" {
		t.Fatalf("runtime create missing boundary/idempotency: %+v", input)
	}
	if strings.Contains(input.InputManifest, "provider_response") || strings.Contains(input.Metadata, "provider_response") {
		t.Fatalf("runtime create leaked forbidden provider artifacts: input=%+v", input)
	}
}

func TestCreateGenerationVersionBlocksWhenGenerationCapabilityUnavailable(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_gen_blocked", "org_1", "SKU-GEN-BLOCKED")
	orchestrator := &fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_generation", Status: "unavailable", Available: false, UnavailableReason: "contract-needed", ContractStatus: "contract-needed", Reasons: []platform.RuntimeCapabilityReason{{Code: "contract-needed", Message: "generation contract missing"}}})}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	created, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{IdempotencyKey: "gen-blocked-1"})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	if created.RuntimeJobID != "" || created.Status != "contract_needed" || !containsReadinessBlocker(created.Blockers, "PLATFORM_CAPABILITY_UNAVAILABLE") {
		t.Fatalf("expected blocked generation without runtime job, got %+v", created)
	}
	if len(orchestrator.createInputs) != 0 {
		t.Fatalf("should not create runtime job when capability unavailable: %+v", orchestrator.createInputs)
	}
}

func TestGenerationVersionValidationRejectsArtifactsAndVocabulary(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Metadata: map[string]any{"nested": map[string]any{"provider_response": "nope"}}}); err == nil {
		t.Fatalf("expected forbidden provider_response rejection")
	}
	created, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	badStatus := "ready"
	if _, err := service.UpdateGenerationVersion("org_1", session.ID, created.VersionID, UpdateGenerationVersionRequest{Status: &badStatus}); err == nil {
		t.Fatalf("expected invalid status rejection")
	}
	badStage := "provider_running"
	if _, err := service.UpdateGenerationVersion("org_1", session.ID, created.VersionID, UpdateGenerationVersionRequest{Stage: &badStage}); err == nil {
		t.Fatalf("expected invalid stage rejection")
	}
	badProgress := 101
	if _, err := service.UpdateGenerationVersion("org_1", session.ID, created.VersionID, UpdateGenerationVersionRequest{Progress: &badProgress}); err == nil {
		t.Fatalf("expected invalid progress rejection")
	}
}

func TestGenerationVersionRejectsClientSuppliedJobReferences(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{RuntimeJobID: "fake_runtime_1"}); err == nil {
		t.Fatalf("expected create runtime_job_id rejection")
	}
	if _, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{ImageJobID: "fake_image_1"}); err == nil {
		t.Fatalf("expected create image_job_id rejection")
	}

	created, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	runtimeJobID := "fake_runtime_2"
	if _, err := service.UpdateGenerationVersion("org_1", session.ID, created.VersionID, UpdateGenerationVersionRequest{RuntimeJobID: &runtimeJobID}); err == nil {
		t.Fatalf("expected update runtime_job_id rejection")
	}
	imageJobID := "fake_image_2"
	if _, err := service.UpdateGenerationVersion("org_1", session.ID, created.VersionID, UpdateGenerationVersionRequest{ImageJobID: &imageJobID}); err == nil {
		t.Fatalf("expected update image_job_id rejection")
	}
	if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{GenerationVersions: []GenerationVersionDTO{{VersionID: "gv_fake", Status: "contract_needed", Stage: "contract_needed", RuntimeJobID: "fake_runtime_3"}}}); err == nil {
		t.Fatalf("expected generic session generation_versions runtime_job_id rejection")
	}

	router := visualWorkflowTestRouter(service)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "handler-create-runtime", method: http.MethodPost, path: "/visual-sessions/" + session.ID + "/generation-versions", body: `{"runtime_job_id":"fake_runtime_http"}`},
		{name: "handler-update-image", method: http.MethodPatch, path: "/visual-sessions/" + session.ID + "/generation-versions/" + created.VersionID, body: `{"image_job_id":"fake_image_http"}`},
		{name: "handler-session-patch-runtime", method: http.MethodPatch, path: "/visual-sessions/" + session.ID, body: `{"generation_versions":[{"version_id":"gv_http","status":"contract_needed","stage":"contract_needed","runtime_job_id":"fake_runtime_patch"}]}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s expected 400, got status=%d body=%s", tc.name, resp.Code, resp.Body.String())
		}
	}

	versions, err := service.ListGenerationVersions("org_1", session.ID)
	if err != nil {
		t.Fatalf("list generation versions: %v", err)
	}
	if len(versions) != 1 || versions[0].RuntimeJobID != "" || versions[0].ImageJobID != "" {
		t.Fatalf("fake job ids persisted after rejection: %+v", versions)
	}
}

func TestGenerationVersionHandlerRejectsForbiddenArtifactPayload(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	router := visualWorkflowTestRouter(service)
	req := httptest.NewRequest(http.MethodPost, "/visual-sessions/"+session.ID+"/generation-versions", bytes.NewBufferString(`{"metadata":{"nested":{"run_response":"forbidden"}}}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected forbidden artifact rejection, got status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func seedProduct(t *testing.T, db *gorm.DB, id, orgID, sku string) models.EcomProductSKU {
	t.Helper()
	product := models.EcomProductSKU{ID: id, OrganizationID: orgID, SKUCode: sku, Title: "Test", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusMissing, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product %s: %v", id, err)
	}
	return product
}

func stringField(t *testing.T, item map[string]any, field string) string {
	t.Helper()
	value, ok := item[field].(string)
	if !ok || value == "" {
		t.Fatalf("expected non-empty string field %q in %#v", field, item)
	}
	return value
}

func performVisualWorkflowRequest(t *testing.T, router *gin.Engine, method, path, body string) map[string]any {
	t.Helper()
	var requestBody *bytes.Reader
	if body == "" {
		requestBody = bytes.NewReader(nil)
	} else {
		requestBody = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, requestBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code < http.StatusOK || w.Code >= http.StatusMultipleChoices {
		t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body.String())
	}
	for _, rawField := range []string{"intent_spec_json", "prompt_plan_json"} {
		if strings.Contains(w.Body.String(), rawField) {
			t.Fatalf("response leaked raw storage field %q: %s", rawField, w.Body.String())
		}
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v; body=%s", err, w.Body.String())
	}
	if envelope.Data == nil {
		t.Fatalf("missing response data: %s", w.Body.String())
	}
	return envelope.Data
}

func assertTypedSessionProjection(t *testing.T, session map[string]any, expectedSceneType, expectedPromptID string) {
	t.Helper()
	intent, ok := session["intent_spec"].(map[string]any)
	if !ok {
		t.Fatalf("expected typed intent_spec object, got %#v in %#v", session["intent_spec"], session)
	}
	if _, ok := session["intent_spec_json"]; ok {
		t.Fatalf("session projection exposed raw intent_spec_json: %#v", session)
	}
	if intent["schema_version"] != "visual_intent_spec.v1" {
		t.Fatalf("intent_spec missing schema defaults: %#v", intent)
	}
	if expectedSceneType != "" && intent["scene_type"] != expectedSceneType {
		t.Fatalf("intent_spec scene_type mismatch: %#v", intent)
	}
	promptPlan, ok := session["prompt_plan"].(map[string]any)
	if !ok {
		t.Fatalf("expected typed prompt_plan object, got %#v in %#v", session["prompt_plan"], session)
	}
	if _, ok := session["prompt_plan_json"]; ok {
		t.Fatalf("session projection exposed raw prompt_plan_json: %#v", session)
	}
	if promptPlan["schema_version"] != "visual_prompt_plan.v1" {
		t.Fatalf("prompt_plan missing schema defaults: %#v", promptPlan)
	}
	if expectedPromptID != "" && promptPlan["prompt_id"] != expectedPromptID {
		t.Fatalf("prompt_plan prompt_id mismatch: %#v", promptPlan)
	}
}

func TestGenerationRuntimeCallbacksUpdateVersionAndIngestResultAssets(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := models.EcomProductSKU{ID: "prod_gen_result", OrganizationID: "org_1", SKUCode: "SKU-GEN-RESULT", Title: "Generation result", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	version, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Status: "processing", Stage: "running"})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	updated, err := service.InternalUpdateGenerationRuntime(version.VersionID, InternalRuntimeUpdateRequest{Status: "processing", Stage: "provider_running", Progress: ptrInt(66), RuntimeJobID: "rt-gen-result-1"})
	if err != nil {
		t.Fatalf("runtime update: %v", err)
	}
	if updated.Status != "processing" || updated.Stage != "running" || updated.Progress != 66 || updated.RuntimeJobID != "rt-gen-result-1" {
		t.Fatalf("unexpected runtime update: %+v", updated)
	}
	result, err := service.InternalRecordGenerationResults(version.VersionID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Stage: "completed", Variants: []map[string]any{{
		"status":      "completed",
		"is_selected": true,
		"asset": map[string]any{
			"storage_key":     "generated/version-1.png",
			"mime_type":       "image/png",
			"width":           1024,
			"height":          768,
			"file_name":       "version-1.png",
			"provider_job_id": "must-not-project",
		},
	}}})
	if err != nil {
		t.Fatalf("record results: %v", err)
	}
	if result.Status != "completed" || result.Progress != 100 || len(result.ResultAssets) != 1 || result.SelectedResultAssetID == "" {
		t.Fatalf("unexpected result projection: %+v", result)
	}
	if result.ResultAssets[0].AssetContentURL == "" || result.ResultAssets[0].Selected != true {
		t.Fatalf("missing selected asset projection: %+v", result.ResultAssets[0])
	}
	if strings.Contains(toJSONForTest(result), "storage_key") || strings.Contains(toJSONForTest(result), "provider_job_id") {
		t.Fatalf("generation version projection leaked raw runtime/storage artifact: %s", toJSONForTest(result))
	}
	var asset models.EcommerceAsset
	if err := db.Where("id = ? AND organization_id = ?", result.SelectedResultAssetID, "org_1").First(&asset).Error; err != nil {
		t.Fatalf("result asset not created: %v", err)
	}
	if asset.StorageKey != "generated/version-1.png" || asset.SourceType != "generated" {
		t.Fatalf("unexpected asset persistence: %+v", asset)
	}
	result2, err := service.InternalRecordGenerationResults(version.VersionID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Stage: "completed", Variants: []map[string]any{{"status": "completed", "is_selected": true, "asset": map[string]any{"storage_key": "generated/version-1.png", "mime_type": "image/png"}}}})
	if err != nil {
		t.Fatalf("record replay: %v", err)
	}
	if len(result2.ResultAssets) != 1 || result2.SelectedResultAssetID != result.SelectedResultAssetID {
		t.Fatalf("replay duplicated assets: before=%+v after=%+v", result, result2)
	}
}

func TestGenerationRuntimeCallbacksValidateMissingAndVariantPayloads(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := models.EcomProductSKU{ID: "prod_gen_invalid", OrganizationID: "org_1", SKUCode: "SKU-GEN-INVALID", Title: "Generation invalid", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	version, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Status: "processing", Stage: "running"})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	if _, err := service.InternalUpdateGenerationRuntime(version.VersionID, InternalRuntimeUpdateRequest{Status: "provider_weird", Progress: ptrInt(10)}); !IsInternalCallbackInvalid(err) {
		t.Fatalf("unsupported runtime status err = %v, want invalid", err)
	}
	if _, err := service.InternalRecordGenerationResults("missing-version", InternalRecordResultsRequest{Status: "completed", Progress: 100}); !IsInternalCallbackNotFound(err) {
		t.Fatalf("missing version err = %v, want not found", err)
	}
	if _, err := service.InternalRecordGenerationResults(version.VersionID, InternalRecordResultsRequest{Status: "completed", Progress: 100}); !IsInternalCallbackInvalid(err) {
		t.Fatalf("completed without variants err = %v, want invalid", err)
	}
	if _, err := service.InternalRecordGenerationResults(version.VersionID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Variants: []map[string]any{{"status": "completed"}}}); !IsInternalCallbackInvalid(err) {
		t.Fatalf("completed variant without asset err = %v, want invalid", err)
	}
}

func seedWritebackVisualWorkflow(t *testing.T, service *Service, db *gorm.DB, selectedAssetID string) (*models.EcomProductSKU, *models.EcommerceVisualWorkflowSession, *GenerationVersionDTO) {
	t.Helper()
	product := models.EcomProductSKU{ID: "prod_writeback", OrganizationID: "org_1", SKUCode: "SKU-WB", Title: "Writeback", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	asset := models.EcommerceAsset{ID: selectedAssetID, OrganizationID: "org_1", UserID: "user_1", AssetType: "image", SourceType: "generated", StorageKey: "generated/result.png", MimeType: "image/png", FileName: "result.png", Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	version, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Status: "completed", Stage: "result_available", ResultAssets: []ResultAssetDTO{{AssetID: selectedAssetID, AssetContentURL: "/api/v1/ecommerce/assets/" + selectedAssetID + "/content"}}, SelectedResultAssetID: selectedAssetID})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	return &product, session, version
}

func TestWritebackSelectedGenerationAssetHappyPathAndIdempotency(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_wb_1")
	resp, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if resp.ProductID != product.ID || resp.SelectedResultAssetID != "asset_wb_1" || resp.AssetRelation.RelationType != models.AssetRelationTypeResult || resp.AssetRelation.AssetRole != models.AssetRoleHero || resp.AssetRelation.OwnerType != models.AssetRelationOwnerTypeProduct || resp.AssetRelation.OwnerID != product.ID {
		t.Fatalf("unexpected writeback response: %+v", resp)
	}
	metadata := resp.AssetRelation.Metadata
	if metadataString(metadata, "origin") != "visual_workflow_selected_generation_asset_writeback" || metadataString(metadata, "visual_workflow_session_id") != session.ID || metadataString(metadata, "generation_version_id") != version.VersionID || metadataString(metadata, "idempotency_key") != "idem-1" {
		t.Fatalf("missing writeback metadata: %#v", metadata)
	}
	if resp.GenerationVersion.Metadata["writeback"] == nil {
		t.Fatalf("missing generation version writeback projection: %#v", resp.GenerationVersion.Metadata)
	}
	replay, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatalf("writeback replay: %v", err)
	}
	if !replay.Idempotent || replay.AssetRelation.ID != resp.AssetRelation.ID {
		t.Fatalf("expected idempotent replay on same relation: %+v", replay)
	}
	var count int64
	if err := db.Model(&models.EcomAssetRelation{}).Where("organization_id = ? AND owner_id = ? AND asset_id = ?", "org_1", product.ID, "asset_wb_1").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected one relation, count=%d err=%v", count, err)
	}
}

func TestWritebackSelectedGenerationAssetPrimaryAndExistingRelation(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_wb_primary")
	otherAsset := models.EcommerceAsset{ID: "asset_other_primary", OrganizationID: "org_1", UserID: "user_1", AssetType: "image", SourceType: "upload", MimeType: "image/png", Metadata: "{}"}
	if err := db.Create(&otherAsset).Error; err != nil {
		t.Fatalf("seed other asset: %v", err)
	}
	otherRel := models.EcomAssetRelation{ID: "rel_other_primary", OrganizationID: "org_1", AssetID: otherAsset.ID, OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: product.ID, RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleHero, IsPrimary: true, Visibility: "library", Metadata: "{}"}
	if err := db.Create(&otherRel).Error; err != nil {
		t.Fatalf("seed other relation: %v", err)
	}
	primary := true
	resp, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{AssetRole: models.AssetRoleSceneShot, IsPrimary: &primary})
	if err != nil {
		t.Fatalf("writeback primary: %v", err)
	}
	if !resp.AssetRelation.IsPrimary || resp.AssetRelation.AssetRole != models.AssetRoleSceneShot {
		t.Fatalf("target relation not primary scene shot: %+v", resp.AssetRelation)
	}
	var reloadedOther models.EcomAssetRelation
	if err := db.First(&reloadedOther, "id = ?", otherRel.ID).Error; err != nil {
		t.Fatalf("reload other relation: %v", err)
	}
	if reloadedOther.IsPrimary {
		t.Fatalf("expected previous primary cleared: %+v", reloadedOther)
	}
}

func TestWritebackSelectedGenerationAssetValidation(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	_, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_wb_validate")
	if _, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{AssetID: "not_in_version"}); err == nil {
		t.Fatalf("expected wrong asset rejection")
	}
	version.SelectedResultAssetID = ""
	version.ResultAssets = []ResultAssetDTO{{AssetID: "asset_wb_validate"}, {AssetID: "asset_missing_from_assets"}}
	encoded, _ := marshalGenerationVersions([]GenerationVersionDTO{*version})
	if err := db.Model(&models.EcommerceVisualWorkflowSession{}).Where("id = ?", session.ID).Update("generation_versions_json", encoded).Error; err != nil {
		t.Fatalf("clear selected asset: %v", err)
	}
	if _, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{}); err == nil {
		t.Fatalf("expected missing selection rejection")
	}
	if _, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{AssetID: "asset_wb_validate", Metadata: map[string]any{"provider_response": "nope"}}); err == nil {
		t.Fatalf("expected forbidden provider artifact rejection")
	}
	if _, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{AssetID: "asset_missing_from_assets"}); err == nil {
		t.Fatalf("expected non ecommerce asset rejection")
	}
}

func TestWritebackSelectedGenerationAssetRejectsUnsafeMetadata(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	_, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_wb_reject_metadata")

	unsafeRequests := []WritebackSelectedGenerationAssetRequest{
		{Metadata: map[string]any{"storage_key": "raw/object.png"}},
		{Metadata: map[string]any{"storageKey": "raw/object.png"}},
		{Metadata: map[string]any{"nested": map[string]any{"internal_storage_key": "raw/object.png"}}},
		{Metadata: map[string]any{"runtime_job_id": "runtime-fake"}},
		{Metadata: map[string]any{"image_job_id": "image-fake"}},
		{Metadata: map[string]any{"provider_job_id": "provider-fake"}},
		{Metadata: map[string]any{"billing": map[string]any{"charge_id": "charge-fake"}}},
	}
	for _, req := range unsafeRequests {
		if _, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, req); err == nil {
			t.Fatalf("expected unsafe writeback metadata rejection for %#v", req.Metadata)
		}
	}
}
