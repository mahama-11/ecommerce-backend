package visualworkflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/promptcenter"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
)

func TestWritebackSelectedGenerationAssetSanitizesExistingRelationMetadata(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_wb_sanitize_existing")
	unsafeMetadata := map[string]any{
		"business_note":        "keep",
		"storage_key":          "raw/object.png",
		"storageKey":           "raw/object-camel.png",
		"internal_storage_key": "raw/internal.png",
		"runtime_job_id":       "runtime-fake",
		"image_job_id":         "image-fake",
		"provider_job_id":      "provider-fake",
		"billing":              map[string]any{"charge_id": "charge-fake"},
		"nested":               map[string]any{"safe": "yes", "provider_response": "fake"},
	}
	unsafeRelation := models.EcomAssetRelation{ID: "rel_wb_unsafe_existing", OrganizationID: "org_1", AssetID: "asset_wb_sanitize_existing", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: product.ID, RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleDetailShot, Visibility: "library", Metadata: mustJSON(unsafeMetadata)}
	if err := db.Create(&unsafeRelation).Error; err != nil {
		t.Fatalf("seed unsafe relation: %v", err)
	}

	resp, err := service.WritebackSelectedGenerationAsset("user_1", "org_1", session.ID, version.VersionID, WritebackSelectedGenerationAssetRequest{IdempotencyKey: "sanitize-existing"})
	if err != nil {
		t.Fatalf("writeback existing unsafe relation: %v", err)
	}
	if resp.AssetRelation.ID != unsafeRelation.ID {
		t.Fatalf("expected existing relation update, got %+v", resp.AssetRelation)
	}
	encoded := mustJSON(resp)
	for _, forbidden := range []string{"storage_key", "storageKey", "internal_storage_key", "runtime_job_id", "image_job_id", "provider_job_id", "provider_response", "charge-fake", "raw/object"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("writeback response leaked forbidden metadata %q: %s", forbidden, encoded)
		}
	}
	if metadataString(resp.AssetRelation.Metadata, "business_note") != "keep" {
		t.Fatalf("safe existing metadata was not preserved: %#v", resp.AssetRelation.Metadata)
	}

	var reloaded models.EcomAssetRelation
	if err := db.First(&reloaded, "id = ?", unsafeRelation.ID).Error; err != nil {
		t.Fatalf("reload relation: %v", err)
	}
	stored := reloaded.Metadata
	for _, forbidden := range []string{"storage_key", "storageKey", "internal_storage_key", "runtime_job_id", "image_job_id", "provider_job_id", "provider_response", "charge-fake", "raw/object"} {
		if strings.Contains(stored, forbidden) {
			t.Fatalf("stored relation metadata was not sanitized for %q: %s", forbidden, stored)
		}
	}
}

func TestWritebackSelectedGenerationAssetHandler(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	_, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_wb_handler")
	router := visualWorkflowTestRouter(service)
	req := httptest.NewRequest(http.MethodPost, "/visual-sessions/"+session.ID+"/generation-versions/"+version.VersionID+"/writeback-selected-asset", bytes.NewBufferString(`{"idempotency_key":"handler-key"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected handler success, got status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, forbidden := range []string{"storage_key", "provider_response", "runtime_job_id", "compiled_prompt"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("writeback response leaked forbidden field %q: %s", forbidden, body)
		}
	}
	badReq := httptest.NewRequest(http.MethodPost, "/visual-sessions/"+session.ID+"/generation-versions/"+version.VersionID+"/writeback-selected-asset", bytes.NewBufferString(`{"asset_id":"wrong"}`))
	badReq.Header.Set("Content-Type", "application/json")
	badResp := httptest.NewRecorder()
	router.ServeHTTP(badResp, badReq)
	if badResp.Code != http.StatusBadRequest {
		t.Fatalf("expected handler bad asset rejection, got status=%d body=%s", badResp.Code, badResp.Body.String())
	}
}

func visualWorkflowTestRouter(service *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user_1")
		c.Set("orgID", "org_1")
		c.Next()
	})
	handler := NewHandler(service)
	router.POST("/products/:product_id/v2/visual-sessions", handler.CreateProductSession)
	router.GET("/visual-sessions", handler.ListSessions)
	router.GET("/visual-sessions/:session_id", handler.GetSession)
	router.PATCH("/visual-sessions/:session_id", handler.UpdateSession)
	router.POST("/visual-sessions/:session_id/cancel", handler.CancelSession)
	router.POST("/visual-sessions/:session_id/source-references", handler.CreateSourceReference)
	router.GET("/visual-sessions/:session_id/source-references", handler.ListSourceReferences)
	router.POST("/visual-sessions/:session_id/deconstruction-jobs", handler.CreateDeconstructionJob)
	router.GET("/visual-sessions/:session_id/deconstruction-jobs/:job_id", handler.GetDeconstructionJob)
	router.GET("/visual-sessions/:session_id/stage-view", handler.StageView)
	router.POST("/visual-sessions/:session_id/generation-versions", handler.CreateGenerationVersion)
	router.GET("/visual-sessions/:session_id/generation-versions", handler.ListGenerationVersions)
	router.GET("/visual-sessions/:session_id/generation-versions/:version_id", handler.GetGenerationVersion)
	router.PATCH("/visual-sessions/:session_id/generation-versions/:version_id", handler.UpdateGenerationVersion)
	router.POST("/visual-sessions/:session_id/generation-versions/:version_id/select", handler.SelectGenerationVersion)
	router.POST("/visual-sessions/:session_id/generation-versions/:version_id/writeback-selected-asset", handler.WritebackSelectedGenerationAsset)
	return router
}

func ptrInt(v int) *int { return &v }

func TestVisualWorkflowDefaultCompactReadProjectionsTrimHeavyGenerationPayloads(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_compact", "org_1", "SKU-COMPACT")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	heavy := strings.Repeat("x", 80_000)
	status := "completed"
	stage := "result_available"
	progress := 100
	version, err := service.CreateGenerationVersion("org_1", session.ID, CreateGenerationVersionRequest{Status: "queued", Stage: "queued", Metadata: map[string]any{
		"source": "sandbox_generation_fanout", "fanout_batch_id": "batch-1", "large_runtime_manifest": heavy,
	}})
	if err != nil {
		t.Fatalf("create generation version: %v", err)
	}
	_, err = service.UpdateGenerationVersion("org_1", session.ID, version.VersionID, UpdateGenerationVersionRequest{
		Status:   &status,
		Stage:    &stage,
		Progress: &progress,
		ResultAssets: []ResultAssetDTO{{AssetID: "asset_compact", AssetContentURL: "/api/v1/ecommerce/assets/asset_compact/content", Selected: true, Metadata: map[string]any{
			"width": 1024, "height": 768, "mime_type": "image/png", "description": heavy,
		}}},
		Metadata: map[string]any{"source": "sandbox_generation_fanout", "fanout_batch_id": "batch-1", "large_runtime_manifest": heavy},
	})
	if err != nil {
		t.Fatalf("update generation version: %v", err)
	}

	router := visualWorkflowTestRouter(service)
	full := performRawVisualWorkflowRequest(t, router, http.MethodGet, "/visual-sessions/"+session.ID+"/generation-versions?projection=full", "")
	if full.Body.Len() < 120_000 {
		t.Fatalf("expected full payload to include heavy fixture, got %d bytes", full.Body.Len())
	}
	compactVersions := performRawVisualWorkflowRequest(t, router, http.MethodGet, "/visual-sessions/"+session.ID+"/generation-versions", "")
	if compactVersions.Body.Len() > 30_000 || strings.Contains(compactVersions.Body.String(), heavy[:128]) || strings.Contains(compactVersions.Body.String(), "large_runtime_manifest") || strings.Contains(compactVersions.Body.String(), "description") {
		t.Fatalf("generation-versions default compact projection too large or leaked heavy fields: bytes=%d body=%s", compactVersions.Body.Len(), compactVersions.Body.String())
	}
	if !strings.Contains(compactVersions.Body.String(), "asset_compact") || !strings.Contains(compactVersions.Body.String(), "fanout_batch_id") || !strings.Contains(compactVersions.Body.String(), "width") {
		t.Fatalf("compact generation versions dropped required workshop summary fields: %s", compactVersions.Body.String())
	}

	compactSession := performRawVisualWorkflowRequest(t, router, http.MethodGet, "/visual-sessions/"+session.ID, "")
	if compactSession.Body.Len() > 35_000 || strings.Contains(compactSession.Body.String(), heavy[:128]) || strings.Contains(compactSession.Body.String(), "large_runtime_manifest") || strings.Contains(compactSession.Body.String(), "description") {
		t.Fatalf("session detail default compact projection too large or leaked heavy fields: bytes=%d body=%s", compactSession.Body.Len(), compactSession.Body.String())
	}

	stageSandbox := performRawVisualWorkflowRequest(t, router, http.MethodGet, "/visual-sessions/"+session.ID+"/stage-view?projection=sandbox", "")
	if strings.Contains(stageSandbox.Body.String(), "\"generation_versions\":[") || strings.Contains(stageSandbox.Body.String(), "\"deconstruction_elements\":[") || strings.Contains(stageSandbox.Body.String(), heavy[:128]) {
		t.Fatalf("sandbox stage projection retained heavy collections: %s", stageSandbox.Body.String())
	}
}

func performRawVisualWorkflowRequest(t *testing.T, router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
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
	return w
}

func TestCreateGenerationVersionWithPromptIDBuildsPlatformRuntimeInputManifest(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	if err := db.AutoMigrate(&models.EcommercePromptRun{}); err != nil {
		t.Fatalf("automigrate prompt: %v", err)
	}
	service = service.WithPromptRepository(repository.NewPromptCenterRepository(db))
	product := models.EcomProductSKU{ID: "prod_prompt", OrganizationID: "org_prompt", SKUCode: "SKU-P", Title: "Prompt", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady}
	asset := models.EcommerceAsset{ID: "asset_prompt", OrganizationID: "org_prompt", UserID: "user_prompt", AssetType: "image", SourceType: "upload", StorageKey: "ecommerce/source/asset.png", MimeType: "image/png", Width: 640, Height: 480, FileName: "asset.png", Metadata: "{}"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if err := db.Create(&models.EcomAssetRelation{ID: "rel_prompt", OrganizationID: "org_prompt", AssetID: asset.ID, OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: product.ID, RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleHero, Visibility: "library"}).Error; err != nil {
		t.Fatalf("seed relation: %v", err)
	}
	if err := db.Create(&models.EcommercePromptRun{ID: "prompt_1", OrganizationID: "org_prompt", UserID: "user_prompt", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_1", TemplateVersionID: "tv_1", TemplateCode: "tpl-code", SceneType: "hero", Status: "validated", SchemaVersion: "prompt.schema.v1", ContentHash: "hash", SourceMapHash: "sourcemap", InputPayloadJSON: "{}", SourceAssetBindingsJSON: `[{"slot":"hero","asset_id":"asset_prompt"}]`, VariablesJSON: "{}", CompiledPromptJSON: `{"strategy":"template","final_prompt":"provider executable prompt","final_negative_prompt":"no blur","sections":[]}`, SourceMapJSON: "{}", ValidationResultJSON: `{"valid":true}`}).Error; err != nil {
		t.Fatalf("seed prompt: %v", err)
	}
	session, err := service.CreateSession("user_prompt", "org_prompt", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	version, err := service.CreateGenerationVersion("org_prompt", session.ID, CreateGenerationVersionRequest{PromptID: "prompt_1"})
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if version.RuntimeJobID == "" || len(fake.createInputs) != 1 {
		t.Fatalf("expected runtime job, version=%+v inputs=%d", version, len(fake.createInputs))
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(fake.createInputs[0].InputManifest), &manifest); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	promptSnapshot := manifest["prompt_snapshot"].(map[string]any)
	if promptSnapshot["user_prompt"] != "provider executable prompt" {
		t.Fatalf("unexpected prompt snapshot: %#v", promptSnapshot)
	}
	if got := manifest["source_assets"].([]any)[0].(map[string]any)["storage_key"]; got != asset.StorageKey {
		t.Fatalf("unexpected source asset storage key: %#v", got)
	}
}

func TestCreateGenerationVersionWithInvalidPromptIDFailsClosed(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	if err := db.AutoMigrate(&models.EcommercePromptRun{}); err != nil {
		t.Fatalf("automigrate prompt: %v", err)
	}
	product := models.EcomProductSKU{ID: "prod_bad_prompt", OrganizationID: "org_bad_prompt", SKUCode: "SKU-B", Title: "Prompt", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	session, err := service.CreateSession("user_bad_prompt", "org_bad_prompt", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithPromptRepository(repository.NewPromptCenterRepository(db)).WithRuntimeOrchestrator(fake)
	version, err := service.CreateGenerationVersion("org_bad_prompt", session.ID, CreateGenerationVersionRequest{PromptID: "missing_prompt"})
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if version.Status != "contract_needed" || version.RuntimeJobID != "" || len(fake.createInputs) != 0 {
		t.Fatalf("expected fail-closed without runtime call, version=%+v inputs=%d", version, len(fake.createInputs))
	}
}

func TestCreateIntentPlannerJobCreatesPlatformTextRuntime(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_intent", "org_intent", "SKU-I")
	session, err := service.CreateSession("user_intent", "org_intent", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	element := models.EcommerceVisualDeconstructionElement{ID: "vde_1", OrganizationID: "org_intent", SessionID: session.ID, JobID: "vdj_1", ElementType: "style", ElementKey: "background", Label: "clean studio", Selected: true, Confirmed: true, Readiness: models.VisualReadinessReady, ValueJSON: toJSONForTest(map[string]any{"color": "white"}), Metadata: toJSONForTest(map[string]any{"decision": "keep"})}
	if err := db.Create(&element).Error; err != nil {
		t.Fatalf("seed element: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyIntentPlanningMatrix(), runtimeJob: &platform.RuntimeJob{ID: "runtime-intent-1", ProductCode: "ecommerce", TaskType: "intent_planning", Status: "queued", Stage: "queued"}}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateIntentPlannerJob("org_intent", session.ID, CreateIntentPlannerJobRequest{Marketplace: "amazon", Locale: "en-US", DriftControls: map[string]any{"reference_weight": 0.7}, IdempotencyKey: "intent-1"})
	if err != nil {
		t.Fatalf("create intent planner: %v", err)
	}
	if resp.RuntimeJobID != "runtime-intent-1" || len(fake.createInputs) != 1 {
		t.Fatalf("expected runtime job creation, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	input := fake.createInputs[0]
	if input.TaskType != "intent_planning" || input.SourceType != "visual_intent_planning" || input.SourceID != session.ID {
		t.Fatalf("unexpected runtime input: %+v", input)
	}
	if strings.Contains(input.InputManifest, "provider_job_id") || strings.Contains(input.InputManifest, "storage_key") {
		t.Fatalf("intent planner manifest leaked forbidden execution metadata: %s", input.InputManifest)
	}
}

func TestIntentPlannerResultUpdatesIntentSpecFromTrustedCallback(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_intent_result", "org_intent_result", "SKU-R")
	session, err := service.CreateSession("user_intent_result", "org_intent_result", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	result, err := service.InternalRecordIntentPlannerResults(session.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Stage: "completed", Variants: []map[string]any{{"inline_data": toJSONForTest(IntentSpecDTO{SchemaVersion: intentSpecSchemaVersion, SceneType: "hero", Requirements: map[string]any{"marketplace": "amazon"}, Selections: []IntentElementDTO{{ElementID: "vde_1", Decision: "keep", Label: "clean studio"}}})}}})
	if err != nil {
		t.Fatalf("record intent planner result: %v", err)
	}
	if result.IntentSpec.SceneType != "hero" || len(result.IntentSpec.Selections) != 1 || result.IntentSpec.Selections[0].Decision != "keep" {
		t.Fatalf("intent spec not updated from planner result: %+v", result.IntentSpec)
	}
}

func TestApplyAttentionTreePersistsDecisions(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_attention", "org_attention", "SKU-A")
	session, err := service.CreateSession("user_attention", "org_attention", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_keep", OrganizationID: "org_attention", SessionID: session.ID, JobID: "vdj_attention", ElementType: "style", ElementKey: "background", Label: "clean studio"},
		{ID: "vde_drop", OrganizationID: "org_attention", SessionID: session.ID, JobID: "vdj_attention", ElementType: "object", ElementKey: "prop", Label: "extra prop"},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	confidence := 0.86
	items, err := service.ApplyAttentionTree("org_attention", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{
		{ElementID: "vde_keep", Decision: "keep", GroupPath: []string{"composition", "background"}, Rationale: "brand background", Confidence: &confidence},
		{ElementID: "vde_drop", Decision: "drop", Rationale: "not part of SKU"},
	}})
	if err != nil {
		t.Fatalf("apply attention tree: %v", err)
	}
	if len(items) != 2 || !items[0].Selected || items[1].Selected {
		t.Fatalf("unexpected attention decisions: %+v", items)
	}
	metadata := decodeObject(items[0].Metadata)
	if metadata["decision"] != "keep" || metadata["confidence"].(float64) != confidence {
		t.Fatalf("attention metadata not persisted: %+v", metadata)
	}
	if _, err := service.ApplyAttentionTree("org_attention", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{{ElementID: "vde_keep", Decision: "overwrite"}}}); err == nil {
		t.Fatalf("expected invalid decision error")
	}
}

func TestCreateDeconstructionJobUsesDualTrackRuntimeManifest(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_dual_manifest", "org_dual_manifest", "SKU-DUAL")
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:     testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready"}),
		runtimeJob: &platform.RuntimeJob{ID: "runtime-dual", ProductCode: "ecommerce", TaskType: "image_understanding", Status: "processing", Stage: "queued"},
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_dual_manifest", "org_dual_manifest", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_dual_manifest", "org_dual_manifest", session.ID)
	job, err := service.CreateDeconstructionJob("user_dual_manifest", "org_dual_manifest", session.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID, RequestedElements: []string{"sku_facts", "reference_strategy"}})
	if err != nil {
		t.Fatalf("create deconstruction job: %v", err)
	}
	if job.SourceReferenceID != source.ID || len(orchestrator.createInputs) != 1 {
		t.Fatalf("expected runtime job with sku primary source, job=%+v inputs=%d", job, len(orchestrator.createInputs))
	}
	manifest := decodeObject(orchestrator.createInputs[0].InputManifest)
	if manifest["input_mode"] != "dual_track_sources" || manifest["source_role_output_required"] != true {
		t.Fatalf("runtime manifest missing dual-track contract: %#v", manifest)
	}
	sources, ok := manifest["source_references"].([]any)
	if !ok || len(sources) != 2 {
		t.Fatalf("expected two contextual source_references in manifest, got %#v", manifest["source_references"])
	}
	runtimeAssets, ok := manifest["source_assets"].([]any)
	if !ok || len(runtimeAssets) != 1 {
		t.Fatalf("expected exactly one source_asset for single-image understanding API, got %#v", manifest["source_assets"])
	}
	roles := map[string]bool{}
	for _, raw := range sources {
		item, _ := raw.(map[string]any)
		roles[fmt.Sprint(item["role"])] = true
	}
	if !roles["sku"] || !roles["reference"] {
		t.Fatalf("expected sku/reference source roles in manifest, got %#v", sources)
	}
	if strings.Contains(orchestrator.createInputs[0].InputManifest, "storage_key") || strings.Contains(orchestrator.createInputs[0].InputManifest, "provider_job_id") {
		t.Fatalf("dual-track manifest leaked forbidden artifacts: %s", orchestrator.createInputs[0].InputManifest)
	}
}

func TestCreateDeconstructionJobInjectsSharedFixedUnderstandingPrompt(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fixed_prompt", "org_fixed_prompt", "SKU-FIXED-PROMPT")
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:     testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready"}),
		runtimeJob: &platform.RuntimeJob{ID: "runtime-fixed-prompt", ProductCode: "ecommerce", TaskType: "image_understanding", Status: "processing", Stage: "queued"},
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_fixed_prompt", "org_fixed_prompt", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_fixed_prompt", "org_fixed_prompt", session.ID)
	_, err = service.CreateDeconstructionJob("user_fixed_prompt", "org_fixed_prompt", session.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID, RequestedElements: []string{"product_info", "background_info"}})
	if err != nil {
		t.Fatalf("create deconstruction job: %v", err)
	}
	if len(orchestrator.createInputs) != 1 {
		t.Fatalf("expected one runtime create input, got %d", len(orchestrator.createInputs))
	}
	manifest := decodeObject(orchestrator.createInputs[0].InputManifest)
	promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
	paramsSnapshot, _ := manifest["params_snapshot"].(map[string]any)
	userPrompt := fmt.Sprint(promptSnapshot["user_prompt"])
	understandingPrompt := fmt.Sprint(paramsSnapshot["understanding_prompt"])
	for _, prompt := range []string{userPrompt, understandingPrompt} {
		if !strings.Contains(prompt, "图片中的产品信息") || !strings.Contains(prompt, "图片中的背景信息") {
			t.Fatalf("shared image understanding prompt missing fixed product/background requirements: %s", prompt)
		}
		if !strings.Contains(prompt, "product_info") || !strings.Contains(prompt, "background_info") || !strings.Contains(prompt, "additional_observations") {
			t.Fatalf("shared image understanding prompt missing stable JSON keys: %s", prompt)
		}
	}
	if userPrompt != understandingPrompt {
		t.Fatalf("prompt_snapshot.user_prompt and params_snapshot.understanding_prompt should use the same shared prompt\nuser=%s\nunderstanding=%s", userPrompt, understandingPrompt)
	}
}

func TestAttentionDecisionRefreshesIntentInputManifest(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_intent_input", "org_intent_input", "SKU-I")
	session, err := service.CreateSession("user_intent_input", "org_intent_input", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	seedDualTrackSourceReferencesForTest(t, service, "user_intent_input", "org_intent_input", session.ID)
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_ref", OrganizationID: "org_intent_input", SessionID: session.ID, JobID: "vdj_intent_input", ElementType: "style", ElementKey: "mood", Label: "warm lifestyle", ValueJSON: toJSONForTest(map[string]any{"style": "warm"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference", "source_reference_id": "vsr_reference"})},
		{ID: "vde_sku", OrganizationID: "org_intent_input", SessionID: session.ID, JobID: "vdj_intent_input", ElementType: "product_fact", ElementKey: "shape", Label: "round bottle", ValueJSON: toJSONForTest(map[string]any{"shape": "round"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku", "source_reference_id": "vsr_sku"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	if _, err := service.ApplyAttentionTree("org_intent_input", session.ID, ApplyAttentionTreeRequest{DriftControls: map[string]any{"reference_bias": 80, "sku_bias": 20}, Decisions: []AttentionDecisionInput{
		{ElementID: "vde_ref", Decision: "keep", GroupPath: []string{"style"}},
		{ElementID: "vde_sku", Decision: "replace", TargetAssetID: "asset_sku"},
	}}); err != nil {
		t.Fatalf("apply attention tree: %v", err)
	}
	model, err := service.repo.GetSession("org_intent_input", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	intent := decodeIntentSpec(model.IntentSpecJSON, model)
	if len(intent.Source.SourceReferences) != 2 || len(intent.Selections) != 2 {
		t.Fatalf("intent spec missing dual-track sources/selections: %+v", intent)
	}
	if intent.Requirements["attribute_drift"] == nil {
		t.Fatalf("intent spec missing drift controls: %+v", intent.Requirements)
	}
	manifest, _ := intent.Metadata["input_manifest"].(map[string]any)
	if manifest["schema_version"] != "visual-intent-input.v1" || manifest["requires_prompt_diff"] != true {
		t.Fatalf("intent input manifest not persisted: %#v", manifest)
	}
	if manifest["sku_fact_count"] != float64(1) || manifest["reference_strategy_count"] != float64(1) {
		t.Fatalf("intent input manifest missing grouped sku/reference counts: %#v", manifest)
	}
	promptPlan := decodePromptPlan(model.PromptPlanJSON, model)
	if promptPlan.Status != "ready" || len(promptPlan.Blockers) != 0 {
		t.Fatalf("expected backend prompt plan readiness from intent fusion input, got %+v", promptPlan)
	}
	if promptPlan.Metadata["source"] != "backend_intent_fusion" {
		t.Fatalf("prompt plan missing backend intent fusion provenance: %+v", promptPlan.Metadata)
	}
	view, err := service.StageView("org_intent_input", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if view.Readiness.Prompt != models.VisualReadinessReady || hasBlocker(view.Readiness.Blockers, "CONTRACT_NEEDED") && containsBlockerTarget(view.Readiness.Blockers, "CONTRACT_NEEDED", "prompt_plan") {
		t.Fatalf("stage view prompt readiness should be ready after backend intent fusion: %+v", view.Readiness)
	}
	encoded, _ := json.Marshal(intent)
	for _, forbidden := range []string{"storage_key", "provider_job_id", "billing"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("intent input leaked forbidden key %q: %s", forbidden, encoded)
		}
	}
}

func TestCreatePromptPlannerJobComposesDeterministicPromptWithoutTextRuntime(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_prompt_planner", "org_prompt_planner", "SKU-P")
	session, err := service.CreateSession("user_prompt_planner", "org_prompt_planner", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_prompt_planner", "org_prompt_planner", session.ID, src); err != nil {
			t.Fatalf("create source: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_pp_sku", OrganizationID: "org_prompt_planner", SessionID: session.ID, JobID: "vdj_pp", ElementType: "product", ElementKey: "sku_product", Label: "SKU 木柄梳子", ValueJSON: toJSONForTest(map[string]any{"description": "木头柄气垫梳，白色背景"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_pp_ref", OrganizationID: "org_prompt_planner", SessionID: session.ID, JobID: "vdj_pp", ElementType: "background", ElementKey: "reference_background", Label: "参考柳条篮场景", ValueJSON: toJSONForTest(map[string]any{"description": "柳条编织篮与自然光背景"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	if _, err := service.ApplyAttentionTree("org_prompt_planner", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{
		{ElementID: "vde_pp_sku", Decision: "keep", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product"}},
		{ElementID: "vde_pp_ref", Decision: "keep", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}},
	}, DriftControls: map[string]any{"sku_weight": 30, "reference_weight": 70}}); err != nil {
		t.Fatalf("apply decisions: %v", err)
	}
	promptFake := &fakePromptSnapshotCreator{response: &promptcenter.PromptRunResponse{PromptID: "prompt_v1_deterministic", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_p1_t01", TemplateVersionID: "tpl_p1_t01_v1", SceneType: "product_composite", Status: "validated"}}
	service.WithPromptSnapshotCreator(promptFake)
	fakeRuntime := &fakeRuntimeCapabilityReader{matrix: readyPromptPlanningMatrix(), runtimeJob: &platform.RuntimeJob{ID: "runtime-should-not-be-created"}}
	service.WithRuntimeOrchestrator(fakeRuntime)
	resp, err := service.CreatePromptPlannerJob("org_prompt_planner", session.ID, CreatePromptPlannerJobRequest{Marketplace: "amazon", Locale: "zh-CN", IdempotencyKey: "prompt-plan-1"})
	if err != nil {
		t.Fatalf("create prompt planner: %v", err)
	}
	if resp.RuntimeJobID != "" || len(fakeRuntime.createInputs) != 0 {
		t.Fatalf("V1 prompt planner must not create text runtime, resp=%+v inputs=%d", resp, len(fakeRuntime.createInputs))
	}
	model, err := service.repo.GetSession("org_prompt_planner", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"SKU 解析结果", "参考素材解析结果", "四问选择", "侧重配置", "木头柄气垫梳", "柳条编织篮", "侧重参考素材 70%"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("deterministic prompt missing %q: %s", want, finalPrompt)
		}
	}
	if strings.Contains(finalPrompt, "{\"") || strings.Contains(finalPrompt, "prompt_id") {
		t.Fatalf("deterministic prompt leaked raw JSON/internal fields: %s", finalPrompt)
	}
	if plan.PromptID != "prompt_v1_deterministic" || metadataString(plan.Metadata, "planner_mode") != "deterministic_v1" {
		t.Fatalf("prompt plan snapshot/mode not persisted: %+v", plan)
	}
}

func TestCreatePromptPlannerJobDirectlyComposesWeakImageAnalysis(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_prompt_quality", "org_prompt_quality", "SKU-Q")
	session, err := service.CreateSession("user_prompt_quality", "org_prompt_quality", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_prompt_quality", "org_prompt_quality", session.ID, src); err != nil {
			t.Fatalf("create source: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_quality_sku", OrganizationID: "org_prompt_quality", SessionID: session.ID, JobID: "vdj_quality", ElementType: "product_fact", ElementKey: "provider_visual_description", Label: "Provider visual description", Confidence: 0.5, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"provider_text": "{\"deconstruction_elements\":[],\"source_role\":\"\",\"source_reference_id\":\"\"}"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku", "decision": "keep", "fixed_prompt_question": true, "prompt_slot": "sku_product"})},
		{ID: "vde_quality_ref", OrganizationID: "org_prompt_quality", SessionID: session.ID, JobID: "vdj_quality", ElementType: "reference_strategy", ElementKey: "style", Label: "Reference style", Confidence: 0, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "minimalist product photography on white background"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference", "decision": "keep", "fixed_prompt_question": true, "prompt_slot": "reference_background"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	service.WithPromptSnapshotCreator(&fakePromptSnapshotCreator{response: &promptcenter.PromptRunResponse{PromptID: "prompt_quality_direct", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_quality_direct", SceneType: "product_composite", Status: "validated"}})
	resp, err := service.CreatePromptPlannerJob("org_prompt_quality", session.ID, CreatePromptPlannerJobRequest{Marketplace: "amazon", Locale: "zh-CN", IdempotencyKey: "prompt-plan-quality"})
	if err != nil {
		t.Fatalf("create prompt planner: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("expected weak image analysis to compose directly, got %+v", resp)
	}
	model, err := service.repo.GetSession("org_prompt_quality", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	if len(plan.Blockers) != 0 {
		t.Fatalf("weak image analysis should not create quality blockers: %+v", plan.Blockers)
	}
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"按当前 SKU 图片直接生成", "minimalist product photography on white background"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("prompt missing %q: %s", want, finalPrompt)
		}
	}
	if strings.Contains(finalPrompt, "按当前参考素材直接生成") {
		t.Fatalf("non-empty weak reference analysis should not be replaced by fallback: %s", finalPrompt)
	}
	if strings.Contains(fmt.Sprint(plan.Variables), "deconstruction_elements") {
		t.Fatalf("raw provider payload leaked into prompt variables: %+v", plan.Variables)
	}
}

func TestCreatePromptPlannerJobUsesFallbackWhenImageAnalysisIsWeak(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_prompt_ref_override", "org_prompt_ref_override", "SKU-REF-OVERRIDE")
	session, err := service.CreateSession("user_prompt_ref_override", "org_prompt_ref_override", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_prompt_ref_override", "org_prompt_ref_override", session.ID, src); err != nil {
			t.Fatalf("create source: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_ref_override_sku", OrganizationID: "org_prompt_ref_override", SessionID: session.ID, JobID: "vdj_ref_override", ElementType: "product_fact", ElementKey: "sku_product", Label: "SKU 主体", Confidence: 0.92, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "白色头戴式耳机，主体清晰"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku", "decision": "keep", "fixed_prompt_question": true, "prompt_slot": "sku_product"})},
		{ID: "vde_ref_override_ref", OrganizationID: "org_prompt_ref_override", SessionID: session.ID, JobID: "vdj_ref_override", ElementType: "reference_strategy", ElementKey: "style", Label: "Reference style", Confidence: 0.3, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "dark lifestyle desk scene"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference", "decision": "keep", "fixed_prompt_question": true, "prompt_slot": "reference_background"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	promptFake := &fakePromptSnapshotCreator{response: &promptcenter.PromptRunResponse{PromptID: "prompt_ref_override", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_ref_override", SceneType: "product_composite", Status: "validated"}}
	service.WithPromptSnapshotCreator(promptFake)
	resp, err := service.CreatePromptPlannerJob("org_prompt_ref_override", session.ID, CreatePromptPlannerJobRequest{Marketplace: "amazon", Locale: "zh-CN", IdempotencyKey: "prompt-plan-ref-override"})
	if err != nil {
		t.Fatalf("create fallback prompt planner: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("expected fallback reference composition to complete, got %+v", resp)
	}
	model, err := service.repo.GetSession("org_prompt_ref_override", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	if !strings.Contains(finalPrompt, "dark lifestyle desk scene") {
		t.Fatalf("weak reference analysis missing from prompt: %s", finalPrompt)
	}
	if strings.Contains(finalPrompt, "按当前参考素材直接生成") {
		t.Fatalf("non-empty weak reference analysis should not be replaced by fallback: %s", finalPrompt)
	}

	blockedSession, err := service.CreateSession("user_prompt_ref_override", "org_prompt_ref_override", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku-low", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference-low", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_prompt_ref_override", "org_prompt_ref_override", blockedSession.ID, src); err != nil {
			t.Fatalf("create blocked source: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_ref_override_low_sku", OrganizationID: "org_prompt_ref_override", SessionID: blockedSession.ID, JobID: "vdj_ref_override_low", ElementType: "product_fact", ElementKey: "sku_product", Label: "SKU 低可信", Confidence: 0.2, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"description": "模糊耳机"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku", "decision": "keep", "fixed_prompt_question": true, "prompt_slot": "sku_product"})},
		{ID: "vde_ref_override_ok_ref", OrganizationID: "org_prompt_ref_override", SessionID: blockedSession.ID, JobID: "vdj_ref_override_low", ElementType: "reference_strategy", ElementKey: "style", Label: "参考风格", Confidence: 0.2, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "dark desk"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference", "decision": "keep", "fixed_prompt_question": true, "prompt_slot": "reference_background"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed blocked element: %v", err)
		}
	}
	overrideResp, err := service.CreatePromptPlannerJob("org_prompt_ref_override", blockedSession.ID, CreatePromptPlannerJobRequest{Marketplace: "amazon", Locale: "zh-CN", IdempotencyKey: "prompt-plan-fallback"})
	if err != nil {
		t.Fatalf("create fallback prompt planner: %v", err)
	}
	if overrideResp.Status != "completed" {
		t.Fatalf("expected fallback prompt composition to complete, got %+v", overrideResp)
	}
	model, err = service.repo.GetSession("org_prompt_ref_override", blockedSession.ID)
	if err != nil {
		t.Fatalf("reload override session: %v", err)
	}
	plan = decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ = plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"按当前 SKU 图片直接生成", "dark desk"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("weak/fallback analysis prompt missing %q: %s", want, finalPrompt)
		}
	}
	if strings.Contains(finalPrompt, "模糊耳机") {
		t.Fatalf("weak SKU analysis should not override safe SKU fallback: %s", finalPrompt)
	}
}

func TestMergeIntentSelectionsPreservesFixedPromptQuestions(t *testing.T) {
	primary := []IntentElementDTO{{
		ElementID:   "vde_sku_real",
		ElementKey:  "sku_product",
		ElementType: "product_fact",
		Decision:    "keep",
		Metadata:    map[string]any{"prompt_slot": "sku_product", "fixed_prompt_question": true},
	}}
	persisted := []IntentElementDTO{
		{ElementID: "fixed:sku_product", ElementKey: "sku_product", ElementType: "product_fact", Decision: "drop", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product"}},
		{ElementID: "fixed:reference_background", ElementKey: "reference_background", ElementType: "background", Decision: "keep", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}},
	}
	merged := mergeIntentSelections(primary, fixedPromptQuestionSelections(persisted))
	if len(merged) != 2 {
		t.Fatalf("expected real selection plus non-duplicate fixed prompt question, got %+v", merged)
	}
	if merged[0].ElementID != "vde_sku_real" || merged[1].ElementID != "fixed:reference_background" {
		t.Fatalf("unexpected merge order/content: %+v", merged)
	}
	if text := promptComposerSelectionText(merged); !strings.Contains(text, "采用参考素材背景风格") || strings.Contains(text, "fixed:sku_product") {
		t.Fatalf("fixed prompt question text did not preserve the expected virtual slot only: %s", text)
	}
}

func TestPromptPlannerResultUpdatesPromptPlanFromTrustedCallback(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_prompt_result", "org_prompt_result", "SKU-PR")
	session, err := service.CreateSession("user_prompt_result", "org_prompt_result", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	plan := PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_v2", SceneType: "hero", TemplateID: "tpl_prompt_result", Variables: map[string]any{"headline": "clean studio"}}
	promptFake := &fakePromptSnapshotCreator{response: &promptcenter.PromptRunResponse{PromptID: "prompt_v2", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_prompt_result", SceneType: "hero", Status: "validated"}}
	service.WithPromptSnapshotCreator(promptFake)
	result, err := service.InternalRecordPromptPlannerResults(session.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Stage: "completed", Variants: []map[string]any{{"inline_data": toJSONForTest(plan)}}})
	if err != nil {
		t.Fatalf("record prompt planner result: %v", err)
	}
	if result.PromptPlan.PromptID != "prompt_v2" || result.PromptPlan.Status != "ready" || result.PromptPlan.Variables["headline"] != "clean studio" {
		t.Fatalf("prompt plan not updated from planner result: %+v", result.PromptPlan)
	}
}
