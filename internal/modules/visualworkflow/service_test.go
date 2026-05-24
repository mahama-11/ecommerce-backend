package visualworkflow

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/promptcenter"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeRuntimeCapabilityReader struct {
	matrix       *platform.RuntimeCapabilityMatrix
	err          error
	runtimeJob   *platform.RuntimeJob
	createErr    error
	createInputs []platform.CreateRuntimeJobInput
	calls        []struct {
		productCode string
		taskType    string
	}
}

func (f *fakeRuntimeCapabilityReader) ListRuntimeCapabilities(productCode, taskType string) (*platform.RuntimeCapabilityMatrix, error) {
	f.calls = append(f.calls, struct {
		productCode string
		taskType    string
	}{productCode: productCode, taskType: taskType})
	return f.matrix, f.err
}

func (f *fakeRuntimeCapabilityReader) CreateRuntimeJob(input platform.CreateRuntimeJobInput) (*platform.RuntimeJob, error) {
	f.createInputs = append(f.createInputs, input)
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.runtimeJob != nil {
		return f.runtimeJob, nil
	}
	return &platform.RuntimeJob{ID: "runtime-job-1", ProductCode: input.ProductCode, TaskType: input.TaskType, OrganizationID: input.OrganizationID, UserID: input.UserID, SourceType: input.SourceType, SourceID: input.SourceID, Status: "queued", Stage: "queued", StageMessage: "queued"}, nil
}

type fakePromptSnapshotCreator struct {
	response *promptcenter.PromptRunResponse
	err      error
	inputs   []promptcenter.PreviewPromptInput
}

func (f *fakePromptSnapshotCreator) Preview(userID, orgID string, input promptcenter.PreviewPromptInput) (*promptcenter.PromptRunResponse, error) {
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return nil, f.err
	}
	if f.response != nil {
		return f.response, nil
	}
	return &promptcenter.PromptRunResponse{PromptID: "prompt_snapshot_1", ProductID: input.ProductID, SKUCode: input.SKUCode, TemplateID: input.TemplateID, TemplateVersionID: input.TemplateVersionID, SceneType: input.SceneType, Status: "validated"}, nil
}

func setupVisualWorkflowTest(t *testing.T) (*Service, *repository.VisualWorkflowRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.EcomProductSKU{},
		&models.EcommerceAsset{},
		&models.EcomAssetRelation{},
		&models.EcommerceVisualWorkflowSession{},
		&models.EcommerceVisualSourceReference{},
		&models.EcommerceVisualDeconstructionJob{},
		&models.EcommerceVisualDeconstructionElement{},
		&models.EcommercePromptRun{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	vwRepo := repository.NewVisualWorkflowRepository(db)
	service := NewService(vwRepo, repository.NewProductCenterRepository(db), repository.NewImageRuntimeRepository(db))
	return service, vwRepo, db
}

func TestVisualWorkflowSourceReferenceArchiveAndListActive(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := models.EcomProductSKU{ID: "prod_sources", OrganizationID: "org_sources", SKUCode: "SKU-SRC", Title: "Source Test", Status: models.ProductStatusDraft}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	session, err := service.CreateSession("user_sources", "org_sources", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode, ToolSlug: "production-pipeline"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sku, err := service.CreateSourceReference("user_sources", "org_sources", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}})
	if err != nil {
		t.Fatalf("create sku source: %v", err)
	}
	ref, err := service.CreateSourceReference("user_sources", "org_sources", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://ref", Metadata: map[string]any{"source_role": "reference"}})
	if err != nil {
		t.Fatalf("create ref source: %v", err)
	}
	if _, err := service.ArchiveSourceReference("org_sources", session.ID, sku.ID); err != nil {
		t.Fatalf("archive source: %v", err)
	}
	items, err := service.ListSourceReferences("org_sources", session.ID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(items) != 1 || items[0].ID != ref.ID {
		t.Fatalf("expected only active reference source, got %+v", items)
	}
	if _, err := service.CreateDeconstructionJob("user_sources", "org_sources", session.ID, CreateDeconstructionJobRequest{}); err == nil || !strings.Contains(err.Error(), "ready sku and reference tracks") {
		t.Fatalf("expected archived sku source to be unavailable for deconstruction, got %v", err)
	}
}

func TestVisualWorkflowFoundationFlow(t *testing.T) {
	service, vwRepo, db := setupVisualWorkflowTest(t)
	product := models.EcomProductSKU{ID: "prod_1", OrganizationID: "org_1", SKUCode: "SKU-1", Title: "Test", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	asset := models.EcommerceAsset{ID: "asset_1", OrganizationID: "org_1", UserID: "user_1", AssetType: "image", SourceType: "upload", StorageKey: "store/source.png", MimeType: "image/png", FileName: "source.png", Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	rel := models.EcomAssetRelation{ID: "rel_1", OrganizationID: "org_1", AssetID: asset.ID, OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: product.ID, RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleHero, Visibility: "library"}
	if err := db.Create(&rel).Error; err != nil {
		t.Fatalf("seed relation: %v", err)
	}

	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode, ToolSlug: "product-scene-compositing"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.CurrentStage != models.VisualWorkflowStageSource {
		t.Fatalf("unexpected session stage: %s", session.CurrentStage)
	}

	source, err := service.CreateSourceReference("user_1", "org_1", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindProductAsset, AssetID: asset.ID, AssetRelationID: rel.ID, Metadata: map[string]any{"source_role": "sku"}})
	if err != nil {
		t.Fatalf("create source reference: %v", err)
	}
	if source.AssetID != asset.ID || source.Status != models.VisualSourceStatusReady {
		t.Fatalf("unexpected source: %+v", source)
	}
	if _, err := service.CreateSourceReference("user_1", "org_1", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}}); err != nil {
		t.Fatalf("create reference source: %v", err)
	}

	job, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, CreateDeconstructionJobRequest{RequestedElements: []string{"product_region"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.Status != models.VisualDeconstructionStatusContractNeeded {
		t.Fatalf("expected contract_needed job, got %s", job.Status)
	}

	element := models.EcommerceVisualDeconstructionElement{ID: "vde_1", OrganizationID: "org_1", SessionID: session.ID, JobID: job.ID, ProductID: product.ID, SKUCode: product.SKUCode, ElementType: "product_region", Label: "Product", Confidence: 0.9, ValueJSON: `{"note":"seeded"}`, Readiness: models.VisualReadinessNeedsReview}
	if err := vwRepo.ReplaceDeconstructionElements("org_1", session.ID, job.ID, []models.EcommerceVisualDeconstructionElement{element}); err != nil {
		t.Fatalf("replace elements: %v", err)
	}
	selected := true
	confirmed := true
	updated, err := service.UpdateElement("org_1", session.ID, element.ID, UpdateElementRequest{Selected: &selected, Confirmed: &confirmed, Readiness: models.VisualReadinessReady})
	if err != nil {
		t.Fatalf("update element: %v", err)
	}
	if !updated.Selected || !updated.Confirmed {
		t.Fatalf("selection not persisted: %+v", updated)
	}

	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if view.SessionID != session.ID || view.SourceReference == nil || len(view.DeconstructionElements) != 1 {
		t.Fatalf("unexpected stage view: %+v", view)
	}
	if view.DeconstructionJob == nil || view.DeconstructionJob.UnavailableReason != "contract-needed" {
		t.Fatalf("stage view missing contract-needed job: %+v", view.DeconstructionJob)
	}
	if view.DeconstructionJob.RuntimeTaskType != "image_understanding" {
		t.Fatalf("stage view exposed non-P0 runtime task type: %+v", view.DeconstructionJob)
	}
	if view.BusinessFlow == nil {
		t.Fatalf("stage view missing business flow DAG")
	}
	if view.BusinessFlow.SchemaVersion != "ecommerce_business_flow.v1" || view.BusinessFlow.FlowID != session.ID {
		t.Fatalf("unexpected business flow identity: %+v", view.BusinessFlow)
	}
	if len(view.BusinessFlow.Nodes) != 8 || len(view.BusinessFlow.Edges) != 7 {
		t.Fatalf("unexpected business flow shape: nodes=%d edges=%d flow=%+v", len(view.BusinessFlow.Nodes), len(view.BusinessFlow.Edges), view.BusinessFlow)
	}
	businessNodes := map[string]BusinessWorkflowNodeDTO{}
	for _, node := range view.BusinessFlow.Nodes {
		businessNodes[node.NodeID] = node
	}
	if businessNodes["source"].Status != models.VisualReadinessReady {
		t.Fatalf("source node should be ready: %+v", businessNodes["source"])
	}
	if businessNodes["deconstruction"].Status != models.VisualDeconstructionStatusContractNeeded {
		t.Fatalf("deconstruction node should expose real job status: %+v", businessNodes["deconstruction"])
	}
	if businessNodes["delivery_download"].Status != models.VisualReadinessMissing || businessNodes["charge_metering"].Status != models.VisualReadinessMissing {
		t.Fatalf("downstream nodes must not fake pass before evidence: delivery=%+v metering=%+v", businessNodes["delivery_download"], businessNodes["charge_metering"])
	}
	if view.IntegrationVerdict == nil || view.IntegrationVerdict.Status != "blocked" {
		t.Fatalf("stage view should expose fail-closed integration verdict: %+v", view.IntegrationVerdict)
	}
	if view.IntegrationVerdict.ReadyCount != 1 || view.IntegrationVerdict.TotalCount != 8 {
		t.Fatalf("integration verdict should count business DAG readiness honestly: %+v", view.IntegrationVerdict)
	}
	if view.RollbackSnapshot == nil || view.RollbackSnapshot.SessionID != session.ID || len(view.RollbackSnapshot.Scopes) == 0 {
		t.Fatalf("stage view should expose rollback scope snapshot: %+v", view.RollbackSnapshot)
	}
	if view.ReleaseReadiness == nil || view.ReleaseReadiness.Status != "blocked" || len(view.ReleaseReadiness.Gates) == 0 {
		t.Fatalf("stage view should expose release readiness gates: %+v", view.ReleaseReadiness)
	}
}

func TestVisualWorkflowInternalRuntimeCallbackUpdatesProductVisibleFields(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_callback", "org_1", "SKU-CB")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job := &models.EcommerceVisualDeconstructionJob{
		ID:                 "vdj_callback_runtime",
		OrganizationID:     "org_1",
		UserID:             "user_1",
		SessionID:          session.ID,
		ProductID:          product.ID,
		SKUCode:            product.SKUCode,
		Status:             models.VisualDeconstructionStatusQueued,
		Stage:              "queued",
		Progress:           0,
		CapabilityCode:     "visual_deconstruction",
		RuntimeTaskType:    "image_understanding",
		InputManifestJSON:  "{}",
		OutputManifestJSON: "{}",
		Metadata:           `{"provider_job_id":"provider-should-not-leak","safe":"keep"}`,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	progress := 150
	updated, err := service.InternalUpdateDeconstructionRuntime(job.ID, InternalRuntimeUpdateRequest{Status: "running", Stage: "provider_running", StageMessage: "analyzing", Progress: &progress, RuntimeJobID: "runtime-123", ErrorCode: "", ErrorMessage: ""})
	if err != nil {
		t.Fatalf("runtime update: %v", err)
	}
	if updated.Status != models.VisualDeconstructionStatusProcessing || updated.Stage != "provider_running" || updated.Progress != 100 || updated.RuntimeJobID != "runtime-123" {
		t.Fatalf("unexpected runtime update: %+v", updated)
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	encoded := mustJSON(view)
	for _, forbidden := range []string{"provider-should-not-leak", "provider_job_id", "storage_key", "provider_response"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("stage view leaked forbidden %q: %s", forbidden, encoded)
		}
	}
}

func TestInternalDeconstructionResultValidatesSourceRoleAndReference(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_role_ingest", "org_role_ingest", "SKU-RI")
	session, err := service.CreateSession("user_role_ingest", "org_role_ingest", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sku, err := service.CreateSourceReference("user_role_ingest", "org_role_ingest", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}})
	if err != nil {
		t.Fatalf("create sku source: %v", err)
	}
	ref, err := service.CreateSourceReference("user_role_ingest", "org_role_ingest", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}})
	if err != nil {
		t.Fatalf("create reference source: %v", err)
	}
	job := &models.EcommerceVisualDeconstructionJob{ID: "vdj_role_ingest", OrganizationID: "org_role_ingest", UserID: "user_role_ingest", SessionID: session.ID, ProductID: product.ID, SKUCode: product.SKUCode, Status: models.VisualDeconstructionStatusProcessing, Stage: "processing", Progress: 40, CapabilityCode: "visual_deconstruction", RuntimeTaskType: "image_understanding", SourceReferenceID: sku.ID, InputManifestJSON: toJSONForTest(map[string]any{"input_mode": "dual_track_sources"}), OutputManifestJSON: "{}", Metadata: "{}"}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	if _, err := service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Elements: []InternalResultElementRequest{
		{ElementType: "product_fact", ElementKey: "shape", Label: "Round bottle", SourceRole: "sku", SourceReferenceID: sku.ID, Value: map[string]any{"shape": "round"}, Metadata: map[string]any{"storage_key": "must-not-leak"}},
		{ElementType: "reference_strategy", ElementKey: "mood", Label: "Warm lifestyle", SourceRole: "reference", SourceReferenceID: ref.ID, Value: map[string]any{"mood": "warm"}},
	}}); err != nil {
		t.Fatalf("record source-role result: %v", err)
	}
	view, err := service.StageView("org_role_ingest", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if len(view.DeconstructionElements) != 2 {
		t.Fatalf("expected 2 elements, got %+v", view.DeconstructionElements)
	}
	roles := map[string]string{}
	for _, element := range view.DeconstructionElements {
		roles[element.SourceRole] = element.SourceReferenceID
		encoded, _ := json.Marshal(element)
		if strings.Contains(string(encoded), "storage_key") {
			t.Fatalf("element leaked storage key: %s", encoded)
		}
	}
	if roles["sku"] != sku.ID || roles["reference"] != ref.ID {
		t.Fatalf("source-role projection mismatch: roles=%+v sku=%s ref=%s", roles, sku.ID, ref.ID)
	}
	service.WithRuntimeCapabilityReader(&fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "unavailable", Available: false, UnavailableReason: "contract-needed", ContractStatus: "contract-needed"})})
	viewWithUnavailableCapability, err := service.StageView("org_role_ingest", session.ID)
	if err != nil {
		t.Fatalf("stage view with unavailable capability: %v", err)
	}
	if viewWithUnavailableCapability.Readiness.Deconstruction != models.VisualReadinessReady {
		t.Fatalf("completed deconstruction result should remain ready even when new runtime capability is unavailable: %+v", viewWithUnavailableCapability.Readiness)
	}

	cases := []struct {
		name string
		elem InternalResultElementRequest
	}{
		{name: "invalid role", elem: InternalResultElementRequest{ElementType: "product_fact", SourceRole: "third_party", SourceReferenceID: sku.ID}},
		{name: "unknown source reference", elem: InternalResultElementRequest{ElementType: "product_fact", SourceRole: "sku", SourceReferenceID: "vsr_missing"}},
		{name: "role mismatch", elem: InternalResultElementRequest{ElementType: "product_fact", SourceRole: "reference", SourceReferenceID: sku.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Elements: []InternalResultElementRequest{tc.elem, {ElementType: "reference_strategy", SourceRole: "reference", SourceReferenceID: ref.ID}}})
			if !IsInternalCallbackInvalid(err) {
				t.Fatalf("expected invalid callback, got %v", err)
			}
		})
	}
	_, err = service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Elements: []InternalResultElementRequest{{ElementType: "product_fact", SourceRole: "sku", SourceReferenceID: sku.ID}}})
	if !IsInternalCallbackInvalid(err) {
		t.Fatalf("expected missing reference role coverage invalid callback, got %v", err)
	}
}

func TestVisualWorkflowInternalResultCallbackReplacesElementsAndSanitizes(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_results", "org_1", "SKU-RES")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job := &models.EcommerceVisualDeconstructionJob{ID: "vdj_callback_results", OrganizationID: "org_1", UserID: "user_1", SessionID: session.ID, ProductID: product.ID, SKUCode: product.SKUCode, Status: models.VisualDeconstructionStatusProcessing, Stage: "processing", Progress: 30, CapabilityCode: "visual_deconstruction", RuntimeTaskType: "image_understanding", InputManifestJSON: "{}", OutputManifestJSON: "{}", Metadata: "{}"}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	first := InternalRecordResultsRequest{Status: "completed", Progress: 100, StageMessage: "done", Metadata: InternalResultMetadataRequest{DeconstructionElements: []InternalResultElementRequest{
		{ElementType: "product_region", ElementKey: "hero", Label: "Hero", Confidence: 1.4, BoundingBox: map[string]any{"x": 1, "provider_response": "raw"}, Value: map[string]any{"color": "red", "provider_job_id": "provider-secret"}, Readiness: models.VisualReadinessReady, Selected: true, SourceAssetID: "asset_src", Metadata: map[string]any{"safe": "yes", "storage_key": "secret/object.png"}},
		{ElementType: "logo", Key: "brand", Label: "Brand", Confidence: 0.8, Value: map[string]any{"text": "ACME"}, Readiness: models.VisualReadinessNeedsReview, SortOrder: 7},
	}}}
	if _, err := service.InternalRecordDeconstructionResults(job.ID, first); err != nil {
		t.Fatalf("record first result: %v", err)
	}
	var count int64
	if err := db.Model(&models.EcommerceVisualDeconstructionElement{}).Where("job_id = ?", job.ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("expected two elements after first callback, count=%d err=%v", count, err)
	}
	replay := InternalRecordResultsRequest{Status: "completed", Progress: 100, Elements: []InternalResultElementRequest{{ElementType: "product_region", ElementKey: "hero-replay", Label: "Hero replay", Confidence: 0.9, Value: map[string]any{"color": "blue"}, Readiness: models.VisualReadinessReady}}}
	if _, err := service.InternalRecordDeconstructionResults(job.ID, replay); err != nil {
		t.Fatalf("record replay result: %v", err)
	}
	if err := db.Model(&models.EcommerceVisualDeconstructionElement{}).Where("job_id = ?", job.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected replay replacement to leave one element, count=%d err=%v", count, err)
	}
	var stored models.EcommerceVisualDeconstructionElement
	if err := db.Where("job_id = ?", job.ID).First(&stored).Error; err != nil {
		t.Fatalf("load stored element: %v", err)
	}
	var reloaded models.EcommerceVisualDeconstructionJob
	if err := db.First(&reloaded, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	combined := stored.ValueJSON + stored.BoundingBoxJSON + stored.Metadata + reloaded.OutputManifestJSON
	for _, forbidden := range []string{"provider_job_id", "provider_response", "provider-secret", "storage_key", "secret/object.png"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("stored visual result leaked forbidden %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(reloaded.OutputManifestJSON, `"elements_count":1`) || strings.Contains(reloaded.OutputManifestJSON, "metadata") {
		t.Fatalf("unexpected sanitized output manifest: %s", reloaded.OutputManifestJSON)
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if view.DeconstructionJob == nil || view.DeconstructionJob.Status != models.VisualDeconstructionStatusCompleted || len(view.DeconstructionElements) != 1 {
		t.Fatalf("unexpected stage view after results: %+v", view)
	}
}

func TestVisualWorkflowInternalResultRejectsImageVariantsWithoutElements(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_variant_reject", "org_1", "SKU-VAR")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job := &models.EcommerceVisualDeconstructionJob{ID: "vdj_callback_variants", OrganizationID: "org_1", UserID: "user_1", SessionID: session.ID, ProductID: product.ID, SKUCode: product.SKUCode, Status: models.VisualDeconstructionStatusProcessing, Stage: "processing", Progress: 30, InputManifestJSON: "{}", OutputManifestJSON: "{}", Metadata: "{}"}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	_, err = service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Variants: []map[string]any{{"index": 0}}})
	if err == nil || !strings.Contains(err.Error(), "deconstruction elements") {
		t.Fatalf("expected clear no-elements rejection, got %v", err)
	}
}

func TestVisualWorkflowInternalResultRejectsUnknownStatusAndClearsElementsOnFailure(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_failed_clear", "org_1", "SKU-FAIL")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job := &models.EcommerceVisualDeconstructionJob{ID: "vdj_failed_clear", OrganizationID: "org_1", UserID: "user_1", SessionID: session.ID, ProductID: product.ID, SKUCode: product.SKUCode, Status: models.VisualDeconstructionStatusProcessing, Stage: "processing", Progress: 30, InputManifestJSON: "{}", OutputManifestJSON: "{}", Metadata: "{}"}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	if _, err := service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Elements: []InternalResultElementRequest{{ElementType: "product_region", Value: map[string]any{"ok": true}}}}); err != nil {
		t.Fatalf("record successful result: %v", err)
	}
	var count int64
	if err := db.Model(&models.EcommerceVisualDeconstructionElement{}).Where("job_id = ?", job.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected one element before failure, count=%d err=%v", count, err)
	}
	if _, err := service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "typo_status", Progress: 100, Elements: []InternalResultElementRequest{{ElementType: "product_region"}}}); !IsInternalCallbackInvalid(err) {
		t.Fatalf("expected invalid status contract error, got %v", err)
	}
	if _, err := service.InternalRecordDeconstructionResults(job.ID, InternalRecordResultsRequest{Status: "failed", Progress: 100, ErrorCode: "PROVIDER_FAILED", ErrorMessage: "normalized failure"}); err != nil {
		t.Fatalf("record failed result: %v", err)
	}
	if err := db.Model(&models.EcommerceVisualDeconstructionElement{}).Where("job_id = ?", job.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expected failed no-element result to clear stale elements, count=%d err=%v", count, err)
	}
	var reloaded models.EcommerceVisualDeconstructionJob
	if err := db.First(&reloaded, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if reloaded.Status != models.VisualDeconstructionStatusFailed || reloaded.Progress != 100 || !strings.Contains(reloaded.OutputManifestJSON, `"elements_count":0`) {
		t.Fatalf("unexpected failed result state: %+v", reloaded)
	}
	var sessionReloaded models.EcommerceVisualWorkflowSession
	if err := db.First(&sessionReloaded, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if sessionReloaded.Status != models.VisualWorkflowStatusFailed {
		t.Fatalf("expected transactional session failed status, got %+v", sessionReloaded)
	}
}

func TestCreateSessionValidatesSKU(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := models.EcomProductSKU{ID: "prod_1", OrganizationID: "org_1", SKUCode: "SKU-1", Title: "Test", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusMissing, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: "WRONG"}); err == nil {
		t.Fatalf("expected SKU validation error")
	}
}

func TestURLSourceReferenceResolvesOpenGraphMetadata(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := models.EcomProductSKU{ID: "prod_1", OrganizationID: "org_1", SKUCode: "SKU-1", Title: "Test", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusMissing, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	allowPrivateSourceResolverHosts = true
	defer func() { allowPrivateSourceResolverHosts = false }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Example Product</title><meta property="og:title" content="OG Product"><meta name="description" content="Source description"><meta property="og:image" content="https://cdn.example/img.jpg"></head><body>ok</body></html>`))
	}))
	defer server.Close()
	source, err := service.CreateSourceReference("user_1", "org_1", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindURL, SourceURL: server.URL + "/item"})
	if err != nil {
		t.Fatalf("create url source: %v", err)
	}
	if source.Status != models.VisualSourceStatusReady || source.ResolveStatus != models.VisualSourceStatusReady {
		t.Fatalf("expected ready resolved source, got %+v", source)
	}
	metadata := decodeObject(source.Metadata)
	urlMetadata := metadata["url_metadata"].(map[string]any)
	if urlMetadata["title"] != "Example Product" {
		t.Fatalf("expected title metadata, got %+v", urlMetadata)
	}
}

func TestCreateProductSessionRejectsBodyPathProductMismatch(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	seedProduct(t, db, "prod_path", "org_1", "SKU-1")
	seedProduct(t, db, "prod_body", "org_1", "SKU-1")
	router := visualWorkflowTestRouter(service)

	req := httptest.NewRequest(http.MethodPost, "/products/prod_path/v2/visual-sessions", bytes.NewBufferString(`{"product_id":"prod_body","sku_code":"SKU-1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected mismatch rejection, got status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCreateProductSessionUsesPathProductWhenBodyOmitsProductID(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	seedProduct(t, db, "prod_path", "org_1", "SKU-1")
	router := visualWorkflowTestRouter(service)

	req := httptest.NewRequest(http.MethodPost, "/products/prod_path/v2/visual-sessions", bytes.NewBufferString(`{"sku_code":"SKU-1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected path product create, got status=%d body=%s", resp.Code, resp.Body.String())
	}
	var decoded struct {
		Data models.EcommerceVisualWorkflowSession `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Data.ProductID != "prod_path" {
		t.Fatalf("expected path product_id, got %+v", decoded.Data)
	}
}

func TestSourceReferencePublicJSONDoesNotLeakStorageKey(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	asset := models.EcommerceAsset{ID: "asset_1", OrganizationID: "org_1", UserID: "user_1", AssetType: "image", SourceType: "upload", StorageKey: "internal/storage/source.png", MimeType: "image/png", FileName: "source.png", Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	rel := models.EcomAssetRelation{ID: "rel_1", OrganizationID: "org_1", AssetID: asset.ID, OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: product.ID, RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleHero, Visibility: "library"}
	if err := db.Create(&rel).Error; err != nil {
		t.Fatalf("seed relation: %v", err)
	}
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	router := visualWorkflowTestRouter(service)
	req := httptest.NewRequest(http.MethodPost, "/visual-sessions/"+session.ID+"/source-references", bytes.NewBufferString(`{"source_kind":"product_asset","asset_id":"asset_1","asset_relation_id":"rel_1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected source reference create, got status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, "storage_key") || strings.Contains(body, asset.StorageKey) {
		t.Fatalf("public response leaked storage key: %s", body)
	}
}

func TestSourceReferenceMetadataSanitizedAcrossCreateListAndStageView(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	router := visualWorkflowTestRouter(service)
	body := `{"source_kind":"upload","source_ref":"upload://1","metadata":{"storage_key":"secret/storage.png","provider_job_id":"provider-secret","provider_response":{"raw":true},"billing":{"truth":true},"charge_id":"charge-secret","nested":{"platform_runtime_idempotency_key":"runtime-secret"},"safe_note":"keep"}}`
	req := httptest.NewRequest(http.MethodPost, "/visual-sessions/"+session.ID+"/source-references", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected source create, got status=%d body=%s", resp.Code, resp.Body.String())
	}
	listReq := httptest.NewRequest(http.MethodGet, "/visual-sessions/"+session.ID+"/source-references", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected source list, got status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	stageReq := httptest.NewRequest(http.MethodGet, "/visual-sessions/"+session.ID+"/stage-view", nil)
	stageResp := httptest.NewRecorder()
	router.ServeHTTP(stageResp, stageReq)
	if stageResp.Code != http.StatusOK {
		t.Fatalf("expected stage view, got status=%d body=%s", stageResp.Code, stageResp.Body.String())
	}
	combined := resp.Body.String() + listResp.Body.String() + stageResp.Body.String()
	for _, forbidden := range []string{"storage_key", "secret/storage.png", "provider_job_id", "provider-secret", "provider_response", "billing", "charge_id", "charge-secret", "platform_runtime_idempotency_key", "runtime-secret"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("source public DTO leaked forbidden %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(combined, "safe_note") || !strings.Contains(combined, "keep") {
		t.Fatalf("safe source metadata was not preserved: %s", combined)
	}
}

func TestStageViewProjectsDualTrackSourceReferences(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := service.CreateSourceReference("user_1", "org_1", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku", "safe_note": "sku"}}); err != nil {
		t.Fatalf("create sku source: %v", err)
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view sku-only: %v", err)
	}
	if len(view.SourceReferences) != 1 || view.Readiness.Source != models.VisualReadinessBlocked || !containsBlockerTarget(view.Readiness.Blockers, "DUAL_TRACK_REFERENCE_SOURCE_REQUIRED", "source_references") {
		t.Fatalf("expected sku-only stage view to require reference source, got readiness=%+v sources=%+v", view.Readiness, view.SourceReferences)
	}
	if view.SourceReference == nil || view.SourceReference.Metadata["source_role"] != "sku" {
		t.Fatalf("expected backward-compatible source_reference to remain latest sku, got %+v", view.SourceReference)
	}

	if _, err := service.CreateSourceReference("user_1", "org_1", session.ID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference", "safe_note": "reference"}}); err != nil {
		t.Fatalf("create reference source: %v", err)
	}
	view, err = service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view dual-track: %v", err)
	}
	if len(view.SourceReferences) != 2 {
		t.Fatalf("expected both source references in stage view, got %+v", view.SourceReferences)
	}
	roles := map[string]bool{}
	for _, source := range view.SourceReferences {
		role, _ := source.Metadata["source_role"].(string)
		roles[role] = true
	}
	if !roles["sku"] || !roles["reference"] {
		t.Fatalf("expected sku and reference roles in stage view, got %+v", view.SourceReferences)
	}
	if view.Readiness.Source != models.VisualReadinessReady {
		t.Fatalf("expected dual-track source readiness ready, got %+v", view.Readiness)
	}
	if containsBlockerTarget(view.Readiness.Blockers, "SOURCE_MISSING", "source_reference") || containsBlockerTarget(view.Readiness.Blockers, "DUAL_TRACK_REFERENCE_SOURCE_REQUIRED", "source_references") {
		t.Fatalf("expected stale source blockers to be cleared after dual-track ready, got %+v", view.Readiness.Blockers)
	}
}

func seedDualTrackSourceReferencesForTest(t *testing.T, service *Service, userID, orgID, sessionID string) *models.EcommerceVisualSourceReference {
	t.Helper()
	sku, err := service.CreateSourceReference(userID, orgID, sessionID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}})
	if err != nil {
		t.Fatalf("create sku source: %v", err)
	}
	if _, err := service.CreateSourceReference(userID, orgID, sessionID, CreateSourceReferenceRequest{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}}); err != nil {
		t.Fatalf("create reference source: %v", err)
	}
	return sku
}
