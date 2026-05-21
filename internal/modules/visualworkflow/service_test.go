package visualworkflow

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestCreateDeconstructionJobValidatesSourceReferenceScope(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session1, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session1: %v", err)
	}
	session2, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session2: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session1.ID)

	if _, err := service.CreateDeconstructionJob("user_1", "org_1", session1.ID, CreateDeconstructionJobRequest{SourceReferenceID: "missing"}); err == nil {
		t.Fatalf("expected missing source_reference_id validation error")
	}
	if _, err := service.CreateDeconstructionJob("user_1", "org_1", session2.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID}); err == nil {
		t.Fatalf("expected other-session source_reference_id validation error")
	}
	job, err := service.CreateDeconstructionJob("user_1", "org_1", session1.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID})
	if err != nil {
		t.Fatalf("expected same-session source_reference_id success: %v", err)
	}
	if job.SourceReferenceID != source.ID {
		t.Fatalf("expected job source_reference_id=%s got %+v", source.ID, job)
	}
}

func TestCreateDeconstructionJobBlocksWhenRuntimeCapabilityUnavailable(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	orchestrator := &fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(
		platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "unavailable", Available: false, UnavailableReason: "contract-needed", ContractStatus: "contract-needed", Reasons: []platform.RuntimeCapabilityReason{{Code: "contract-needed", Message: "contract missing"}}},
	)}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	job, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID, IdempotencyKey: "idem-blocked"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.Status != models.VisualDeconstructionStatusContractNeeded || job.RuntimeJobID != "" || job.ErrorCode != "PLATFORM_CAPABILITY_UNAVAILABLE" {
		t.Fatalf("expected blocked job without runtime id, got %+v", job)
	}
	if len(orchestrator.createInputs) != 0 {
		t.Fatalf("platform runtime job should not be created when capability unavailable: %+v", orchestrator.createInputs)
	}
	metadata := decodeObject(job.Metadata)
	if metadata["platform_blocker"] == nil || metadata["unavailable_reason"] != "contract-needed" {
		t.Fatalf("expected platform blocker metadata, got %+v", metadata)
	}
}

func TestCreateDeconstructionJobCreatesRuntimeJobAndPreservesIdempotency(t *testing.T) {
	service, repo, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:     testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_image_understanding", Configured: true}}),
		runtimeJob: &platform.RuntimeJob{ID: "runtime-vdj-1", ProductCode: "ecommerce", TaskType: "image_understanding", OrganizationID: "org_1", UserID: "user_1", SourceType: "visual_deconstruction", Status: "processing", Stage: "runtime_queued", StageMessage: "accepted"},
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	req := CreateDeconstructionJobRequest{SourceReferenceID: source.ID, IdempotencyKey: "client-retry-1", RequestedElements: []string{"product_region"}, Metadata: map[string]any{"storage_key": "must-not-forward", "provider_job_id": "provider-unsafe", "nested": map[string]any{"billing": map[string]any{"truth": true}}, "safe_note": "keep"}}
	job, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, req)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.RuntimeJobID != "runtime-vdj-1" || job.Status != models.VisualDeconstructionStatusProcessing || job.Progress < 5 {
		t.Fatalf("expected processing job with runtime reference, got %+v", job)
	}
	if len(orchestrator.calls) != 1 || orchestrator.calls[0].productCode != "ecommerce" || orchestrator.calls[0].taskType != "image_understanding" {
		t.Fatalf("unexpected capability calls: %+v", orchestrator.calls)
	}
	if len(orchestrator.createInputs) != 1 {
		t.Fatalf("expected one runtime create, got %+v", orchestrator.createInputs)
	}
	input := orchestrator.createInputs[0]
	if input.ProductCode != "ecommerce" || input.TaskType != "image_understanding" || input.SourceType != "visual_deconstruction" || input.SourceID != job.ID {
		t.Fatalf("unexpected runtime create input: %+v", input)
	}
	if !strings.Contains(input.IdempotencyKey, "client-retry-1") {
		t.Fatalf("expected stable runtime idempotency key to include client key, got %s", input.IdempotencyKey)
	}
	if strings.Contains(input.InputManifest, "storage_key") || strings.Contains(input.Metadata, "storage_key") {
		t.Fatalf("runtime create leaked storage key: input=%+v", input)
	}
	storedMetadata := decodeObject(job.Metadata)
	encodedStoredMetadata, _ := json.Marshal(storedMetadata)
	for _, forbidden := range []string{"storage_key", "provider_job_id", "billing"} {
		if strings.Contains(string(encodedStoredMetadata), forbidden) {
			t.Fatalf("stored deconstruction metadata leaked forbidden key %q: %s", forbidden, encodedStoredMetadata)
		}
	}
	if metadataString(storedMetadata, "safe_note") != "keep" {
		t.Fatalf("safe deconstruction metadata was not preserved: %#v", storedMetadata)
	}
	replayed, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, req)
	if err != nil {
		t.Fatalf("replay create job: %v", err)
	}
	if replayed.ID != job.ID || len(orchestrator.createInputs) != 1 {
		t.Fatalf("idempotent replay should return existing job without duplicate runtime create: replay=%+v creates=%+v", replayed, orchestrator.createInputs)
	}
	stored, err := repo.GetDeconstructionJob("org_1", session.ID, job.ID)
	if err != nil {
		t.Fatalf("reload deconstruction job: %v", err)
	}
	if stored.RuntimeJobID != "runtime-vdj-1" {
		t.Fatalf("runtime_job_id was not persisted: %+v", stored)
	}
	storedJSON, _ := json.Marshal(decodeObject(stored.Metadata))
	for _, forbidden := range []string{"runtime_job_id", "platform_runtime_idempotency_key", "platform_runtime_job_status", "billable_item_code", "billing_configured", "charge"} {
		if strings.Contains(string(storedJSON), forbidden) {
			t.Fatalf("stored deconstruction metadata leaked %q: %s", forbidden, storedJSON)
		}
	}
}

func TestCreateDeconstructionJobCapabilityErrorIsSafeAndDoesNotCreateRuntime(t *testing.T) {
	service, repo, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	secret := "SECRET_TOKEN_SHOULD_NOT_LEAK"
	orchestrator := &fakeRuntimeCapabilityReader{err: errors.New("platform down token=" + secret)}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	job, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.Status != models.VisualDeconstructionStatusContractNeeded || job.ErrorCode != "PLATFORM_CAPABILITY_ERROR" || job.RuntimeJobID != "" {
		t.Fatalf("expected safe blocked capability error job, got %+v", job)
	}
	if len(orchestrator.createInputs) != 0 {
		t.Fatalf("runtime create should not be called on capability error: %+v", orchestrator.createInputs)
	}
	stored, err := repo.GetDeconstructionJob("org_1", session.ID, job.ID)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	public, _ := json.Marshal(jobDTO(stored))
	metadata, _ := json.Marshal(decodeObject(stored.Metadata))
	if strings.Contains(string(public), secret) || strings.Contains(string(metadata), secret) || strings.Contains(stored.ErrorMessage, secret) || strings.Contains(stored.StageMessage, secret) {
		t.Fatalf("capability error leaked raw secret: public=%s metadata=%s stored=%+v", public, metadata, stored)
	}
}

func TestCreateDeconstructionJobRuntimeCreateErrorIsSafe(t *testing.T) {
	service, repo, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	secret := "RUNTIME_SECRET_SHOULD_NOT_LEAK"
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:    testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready"}),
		createErr: errors.New("runtime create failed bearer=" + secret),
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	job, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, CreateDeconstructionJobRequest{SourceReferenceID: source.ID, Metadata: map[string]any{"billing": "no", "provider_response": secret, "safe_note": "keep"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.Status != models.VisualDeconstructionStatusContractNeeded || job.ErrorCode != "PLATFORM_RUNTIME_JOB_CREATE_FAILED" || job.RuntimeJobID != "" {
		t.Fatalf("expected safe blocked runtime create failure, got %+v", job)
	}
	if len(orchestrator.createInputs) != 1 {
		t.Fatalf("expected one runtime create attempt, got %+v", orchestrator.createInputs)
	}
	stored, err := repo.GetDeconstructionJob("org_1", session.ID, job.ID)
	if err != nil {
		t.Fatalf("reload job: %v", err)
	}
	public, _ := json.Marshal(jobDTO(stored))
	metadata, _ := json.Marshal(decodeObject(stored.Metadata))
	if strings.Contains(string(public), secret) || strings.Contains(string(metadata), secret) || strings.Contains(stored.ErrorMessage, secret) || strings.Contains(stored.StageMessage, secret) {
		t.Fatalf("runtime create error leaked raw secret: public=%s metadata=%s stored=%+v", public, metadata, stored)
	}
	for _, forbidden := range []string{"provider_response", "billing", "runtime_job_id", "platform_runtime_idempotency_key", "charge"} {
		if strings.Contains(string(metadata), forbidden) {
			t.Fatalf("stored failure metadata leaked %q: %s", forbidden, metadata)
		}
	}
}

func TestCreateDeconstructionJobDerivesStableIdempotencyWithoutClientKey(t *testing.T) {
	service, repo, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:     testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready"}),
		runtimeJob: &platform.RuntimeJob{ID: "runtime-stable", ProductCode: "ecommerce", TaskType: "image_understanding", Status: "processing", Stage: "runtime_queued", StageMessage: "accepted"},
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	req := CreateDeconstructionJobRequest{SourceReferenceID: source.ID, RequestedElements: []string{"product_region", "logo"}}
	job, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, req)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if job.IdempotencyKey == "" || !strings.HasPrefix(job.IdempotencyKey, "server-") {
		t.Fatalf("expected deterministic server idempotency key, got %+v", job)
	}
	replayed, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, req)
	if err != nil {
		t.Fatalf("replay job: %v", err)
	}
	if replayed.ID != job.ID || len(orchestrator.createInputs) != 1 {
		t.Fatalf("expected deterministic replay without duplicate runtime create: replay=%+v creates=%+v", replayed, orchestrator.createInputs)
	}
	dupe := *job
	dupe.ID = "vdj_duplicate"
	dupe.RuntimeJobID = ""
	if err := repo.CreateDeconstructionJob(&dupe); err != nil {
		t.Fatalf("duplicate create should be conflict-safe: %v", err)
	}
	if dupe.ID != job.ID {
		t.Fatalf("duplicate create should reload existing row, got %+v want %s", dupe, job.ID)
	}
	var count int64
	if err := db.Model(&models.EcommerceVisualDeconstructionJob{}).Where("organization_id = ? AND session_id = ? AND idempotency_key = ?", "org_1", session.ID, job.IdempotencyKey).Count(&count).Error; err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected unique idempotency row, got %d", count)
	}
}

func TestDeconstructionJobPublicJSONDoesNotLeakRawArtifacts(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	orchestrator := &fakeRuntimeCapabilityReader{
		matrix:     testCapabilityMatrix(platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready"}),
		runtimeJob: &platform.RuntimeJob{ID: "runtime-vdj-public", ProductCode: "ecommerce", TaskType: "image_understanding", OrganizationID: "org_1", UserID: "user_1", SourceType: "visual_deconstruction", Status: "processing", Stage: "runtime_queued", StageMessage: "accepted"},
	}
	service.WithRuntimeOrchestrator(orchestrator)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	router := visualWorkflowTestRouter(service)
	body := `{"source_reference_id":"` + source.ID + `","idempotency_key":"client-public-key","runtime_job_id":"client-runtime","provider_job_id":"client-provider","storage_key":"client-storage","billing":{"truth":true},"charge_id":"client-charge","metadata":{"storage_key":"raw/object.png","provider_job_id":"provider-fake","provider":{"payload":true},"nested":{"platform_runtime_idempotency_key":"runtime-secret","billing_truth":true},"safe_note":"keep"}}`
	req := httptest.NewRequest(http.MethodPost, "/visual-sessions/"+session.ID+"/deconstruction-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected deconstruction create, got status=%d body=%s", resp.Code, resp.Body.String())
	}
	createBody := resp.Body.String()
	for _, forbidden := range []string{"storage_key", "client-storage", "client-provider", "client-runtime", "client-charge", "provider_job_id", "idempotency_key", "platform_runtime_idempotency_key", "provider\":", "billing_truth", "metadata", "input_manifest", "output_manifest"} {
		if strings.Contains(createBody, forbidden) {
			t.Fatalf("public create response leaked forbidden field %q: %s", forbidden, createBody)
		}
	}
	if strings.Contains(createBody, "client-public-key") {
		t.Fatalf("public create response leaked client idempotency key: %s", createBody)
	}

	var decoded struct {
		Data DeconstructionJobDTO `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	getReq := httptest.NewRequest(http.MethodGet, "/visual-sessions/"+session.ID+"/deconstruction-jobs/"+decoded.Data.JobID, nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected deconstruction get, got status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	getBody := getResp.Body.String()
	for _, forbidden := range []string{"storage_key", "client-storage", "client-provider", "client-runtime", "client-charge", "provider_job_id", "idempotency_key", "platform_runtime_idempotency_key", "provider\":", "billing_truth", "metadata", "input_manifest", "output_manifest"} {
		if strings.Contains(getBody, forbidden) {
			t.Fatalf("public get response leaked forbidden field %q: %s", forbidden, getBody)
		}
	}
	if strings.Contains(getBody, "client-public-key") {
		t.Fatalf("public get response leaked client idempotency key: %s", getBody)
	}
	stageReq := httptest.NewRequest(http.MethodGet, "/visual-sessions/"+session.ID+"/stage-view", nil)
	stageResp := httptest.NewRecorder()
	router.ServeHTTP(stageResp, stageReq)
	if stageResp.Code != http.StatusOK {
		t.Fatalf("expected stage-view, got status=%d body=%s", stageResp.Code, stageResp.Body.String())
	}
	stageBody := stageResp.Body.String()
	for _, forbidden := range []string{"storage_key", "client-storage", "client-provider", "client-runtime", "client-charge", "provider_job_id", "idempotency_key", "platform_runtime_idempotency_key", "provider\":", "billing_truth", "input_manifest", "output_manifest"} {
		if strings.Contains(stageBody, forbidden) {
			t.Fatalf("stage-view leaked forbidden field %q: %s", forbidden, stageBody)
		}
	}
}

func TestVisualWorkflowRejectsInvalidVocabulary(t *testing.T) {
	service, vwRepo, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	source := seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	element := models.EcommerceVisualDeconstructionElement{ID: "vde_invalid", OrganizationID: "org_1", SessionID: session.ID, JobID: "job_1", ProductID: product.ID, SKUCode: product.SKUCode, ElementType: "product_region", ValueJSON: `{}`, Readiness: models.VisualReadinessNeedsReview}
	if err := vwRepo.ReplaceDeconstructionElements("org_1", session.ID, "job_1", []models.EcommerceVisualDeconstructionElement{element}); err != nil {
		t.Fatalf("seed element: %v", err)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{"invalid session stage", func() error {
			_, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{CurrentStage: "bogus"})
			return err
		}},
		{"invalid session status", func() error {
			_, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{Status: "bogus"})
			return err
		}},
		{"invalid readiness map", func() error {
			_, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{Readiness: map[string]any{"overall": "bogus"}})
			return err
		}},
		{"invalid source status", func() error {
			_, err := service.UpdateSourceReference("org_1", session.ID, source.ID, UpdateSourceReferenceRequest{Status: "bogus"})
			return err
		}},
		{"invalid resolve status", func() error {
			_, err := service.UpdateSourceReference("org_1", session.ID, source.ID, UpdateSourceReferenceRequest{ResolveStatus: "bogus"})
			return err
		}},
		{"invalid element readiness", func() error {
			_, err := service.UpdateElement("org_1", session.ID, element.ID, UpdateElementRequest{Readiness: "bogus"})
			return err
		}},
		{"invalid source kind", func() error {
			_, err := service.CreateSourceReference("user_1", "org_1", session.ID, CreateSourceReferenceRequest{SourceKind: "bogus"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestStageViewProjectsRuntimeCapabilities(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	reader := &fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(
		platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "available", Available: true, ContractStatus: "ready", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_image_understanding"}},
		platform.RuntimeCapabilityItem{TaskType: "image_generation", Status: "available", Available: true, ContractStatus: "ready", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_image_generation"}, ProviderBindings: []platform.RuntimeProviderBinding{{ProviderCode: "secret-provider", Metadata: map[string]any{"do_not_expose": "x"}}}},
		platform.RuntimeCapabilityItem{TaskType: "image_inpainting", Status: "unavailable", Available: false, UnavailableReason: "contract-needed", ContractStatus: "contract-needed", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_image_inpainting"}, Reasons: []platform.RuntimeCapabilityReason{{Code: "contract-needed", Message: "not ready"}}},
		platform.RuntimeCapabilityItem{TaskType: "video_keyframe", Status: "unavailable", Available: false, UnavailableReason: "contract-needed", ContractStatus: "contract-needed", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_video_keyframe"}},
	)}
	service.WithRuntimeCapabilityReader(reader)
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if len(reader.calls) != 1 || reader.calls[0].productCode != "ecommerce" || reader.calls[0].taskType != "" {
		t.Fatalf("unexpected capability reader calls: %+v", reader.calls)
	}
	if len(view.RuntimeCapabilities) != 4 {
		t.Fatalf("expected 4 runtime capability rows, got %+v", view.RuntimeCapabilities)
	}
	encoded, _ := json.Marshal(view.RuntimeCapabilities)
	if strings.Contains(string(encoded), "billable_item_code") || strings.Contains(string(encoded), "ecommerce_runtime_image_generation") || strings.Contains(string(encoded), "provider_bindings") || strings.Contains(string(encoded), "secret-provider") || strings.Contains(string(encoded), "do_not_expose") {
		t.Fatalf("runtime capability projection leaked provider/billing details: %s", encoded)
	}
}

func TestStageViewUnavailableImageUnderstandingAddsCapabilityBlocker(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	service.WithRuntimeCapabilityReader(&fakeRuntimeCapabilityReader{matrix: testCapabilityMatrix(
		platform.RuntimeCapabilityItem{TaskType: "image_understanding", Status: "unavailable", Available: false, UnavailableReason: "contract-needed", ContractStatus: "contract-needed", Billing: platform.RuntimeBillingCapability{BillableItemCode: "ecommerce_runtime_image_understanding"}, Reasons: []platform.RuntimeCapabilityReason{{Code: "contract-needed", Message: "Runtime task contract is not implemented for this task_type yet."}}},
	)})
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	seedDualTrackSourceReferencesForTest(t, service, "user_1", "org_1", session.ID)
	if _, err := service.CreateDeconstructionJob("user_1", "org_1", session.ID, CreateDeconstructionJobRequest{RequestedElements: []string{"product_region"}}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if view.Readiness.Deconstruction != models.VisualReadinessBlocked {
		t.Fatalf("expected deconstruction blocked, got %+v", view.Readiness)
	}
	if !hasBlocker(view.Readiness.Blockers, "CONTRACT_NEEDED") || !hasBlocker(view.Readiness.Blockers, "PLATFORM_CAPABILITY_UNAVAILABLE") {
		t.Fatalf("expected contract-needed and platform capability blockers, got %+v", view.Readiness.Blockers)
	}
	if len(view.RuntimeCapabilities) != 1 || view.RuntimeCapabilities[0].UnavailableReason != "contract-needed" {
		t.Fatalf("expected unavailable capability row, got %+v", view.RuntimeCapabilities)
	}
}

func TestStageViewRuntimeCapabilityErrorDoesNotFail(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	service.WithRuntimeCapabilityReader(&fakeRuntimeCapabilityReader{err: errors.New("platform down")})
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view should not fail on platform capability error: %v", err)
	}
	if view.RuntimeCapabilityError == nil || view.RuntimeCapabilityError.Code != "PLATFORM_CAPABILITY_ERROR" {
		t.Fatalf("expected runtime capability error metadata, got %+v", view.RuntimeCapabilityError)
	}
	if !hasBlocker(view.Readiness.Blockers, "PLATFORM_CAPABILITY_ERROR") {
		t.Fatalf("expected platform capability error blocker, got %+v", view.Readiness.Blockers)
	}
}

func TestVisualWorkflowPersistsTypedIntentSpecAndPromptPlan(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode, ToolSlug: "product-scene-compositing", TemplateID: "tmpl_1", TemplateVersionID: "tmv_1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	intent := &IntentSpecDTO{
		SchemaVersion: "visual_intent_spec.v1",
		SceneType:     "hero_scene",
		Selections: []IntentElementDTO{{
			ElementID:   "vde_1",
			ElementType: "product_region",
			ElementKey:  "hero",
			Label:       "Hero product",
			Value:       map[string]any{"color": "red"},
		}},
		Requirements: map[string]any{"aspect_ratio": "1:1"},
		Metadata:     map[string]any{"business_note": "safe"},
	}
	promptPlan := &PromptPlanDTO{
		SchemaVersion:     "visual_prompt_plan.v1",
		Status:            "contract_needed",
		PromptID:          "prompt_run_existing_1",
		SceneType:         "hero_scene",
		TemplateID:        "tmpl_1",
		TemplateVersionID: "tmv_1",
		Variables:         map[string]any{"tone": "premium"},
		SourceAssets:      []PromptPlanSourceAssetDTO{{AssetID: "asset_1", Role: "source"}},
		Metadata:          map[string]any{"planner": "visual-workflow"},
	}
	if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{IntentSpec: intent, PromptPlan: promptPlan}); err != nil {
		t.Fatalf("update typed state: %v", err)
	}

	reloaded, err := service.GetSession("org_1", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	decodedIntent := decodeIntentSpec(reloaded.IntentSpecJSON, reloaded)
	if decodedIntent.SchemaVersion != "visual_intent_spec.v1" || decodedIntent.ProductID != product.ID || decodedIntent.SKUCode != product.SKUCode || decodedIntent.ToolSlug != "product-scene-compositing" {
		t.Fatalf("intent defaults/fields did not survive reload: %+v", decodedIntent)
	}
	if decodedIntent.SceneType != "hero_scene" || decodedIntent.Selections[0].ElementType != "product_region" || decodedIntent.Requirements["aspect_ratio"] != "1:1" {
		t.Fatalf("intent business state lost: %+v", decodedIntent)
	}
	decodedPlan := decodePromptPlan(reloaded.PromptPlanJSON, reloaded)
	if decodedPlan.SchemaVersion != "visual_prompt_plan.v1" || decodedPlan.PromptID != "prompt_run_existing_1" || decodedPlan.Variables["tone"] != "premium" || len(decodedPlan.SourceAssets) != 1 {
		t.Fatalf("prompt plan did not survive reload: %+v", decodedPlan)
	}
}

func TestStageViewProjectsTypedStateAndContractNeededReadiness(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode, ToolSlug: "product-scene-compositing"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{
		IntentSpec: &IntentSpecDTO{SceneType: "lifestyle", Requirements: map[string]any{"marketplace": "amazon"}},
		PromptPlan: &PromptPlanDTO{Status: "contract_needed", PromptID: "prompt_run_existing_1", Variables: map[string]any{"audience": "new buyers"}, SourceAssets: []PromptPlanSourceAssetDTO{{SourceReferenceID: "vsr_1", Role: "main"}}},
	}); err != nil {
		t.Fatalf("update typed state: %v", err)
	}

	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if view.IntentSpec.SchemaVersion != "visual_intent_spec.v1" || view.IntentSpec.SceneType != "lifestyle" || view.IntentSpec.Requirements["marketplace"] != "amazon" {
		t.Fatalf("stage-view missing typed intent: %+v", view.IntentSpec)
	}
	if view.PromptPlan.SchemaVersion != "visual_prompt_plan.v1" || view.PromptPlan.PromptID != "prompt_run_existing_1" || view.PromptPlan.Variables["audience"] != "new buyers" || len(view.PromptPlan.SourceAssets) != 1 {
		t.Fatalf("stage-view missing typed prompt plan: %+v", view.PromptPlan)
	}
	if view.Readiness.Prompt == models.VisualReadinessReady || view.Readiness.Generation != models.VisualReadinessBlocked || view.Readiness.Overall != models.VisualReadinessBlocked {
		t.Fatalf("expected prompt/generation blocked readiness, got %+v", view.Readiness)
	}
	if !hasBlockerTarget(view.Readiness.Blockers, "CONTRACT_NEEDED", "prompt_plan") || !hasBlockerTarget(view.Readiness.Blockers, "CONTRACT_NEEDED", "generation") {
		t.Fatalf("expected prompt_plan and generation contract blockers, got %+v", view.Readiness.Blockers)
	}
}

func TestStageViewBlocksOptimisticPromptReadyWithoutContract(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode, ToolSlug: "product-scene-compositing"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{
		CurrentStage: models.VisualWorkflowStageReview,
		Readiness: map[string]any{
			"overall":        models.VisualReadinessReady,
			"source":         models.VisualReadinessReady,
			"deconstruction": models.VisualReadinessReady,
			"prompt":         models.VisualReadinessReady,
			"generation":     models.VisualReadinessReady,
		},
		PromptPlan: &PromptPlanDTO{Status: "ready", PromptID: "prompt_run_existing_1"},
	}); err != nil {
		t.Fatalf("update optimistic ready state: %v", err)
	}

	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	if view.PromptPlan.Status != "ready" {
		t.Fatalf("test setup expected persisted ready prompt plan, got %+v", view.PromptPlan)
	}
	if view.Readiness.Prompt == models.VisualReadinessReady || view.Readiness.Prompt != models.VisualReadinessBlocked {
		t.Fatalf("stage-view projected prompt ready without contract: %+v", view.Readiness)
	}
	if view.Readiness.Overall != models.VisualReadinessBlocked {
		t.Fatalf("expected overall blocked from prompt contract blocker, got %+v", view.Readiness)
	}
	if !hasBlockerTarget(view.Readiness.Blockers, "CONTRACT_NEEDED", "prompt_plan") {
		t.Fatalf("expected prompt_plan contract-needed blocker, got %+v", view.Readiness.Blockers)
	}
}

func TestSessionHandlersProjectTypedIntentSpecAndPromptPlan(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	router := visualWorkflowTestRouter(service)

	createBody := `{"sku_code":"SKU-1","tool_slug":"product-scene-compositing","template_id":"tmpl_1","template_version_id":"tmv_1"}`
	createResp := performVisualWorkflowRequest(t, router, http.MethodPost, "/products/"+product.ID+"/v2/visual-sessions", createBody)
	assertTypedSessionProjection(t, createResp, "", "")
	sessionID := stringField(t, createResp, "id")

	updateBody := `{
		"intent_spec":{"scene_type":"lifestyle","requirements":{"marketplace":"amazon"}},
		"prompt_plan":{"status":"contract_needed","prompt_id":"prompt_run_existing_1","variables":{"audience":"new buyers"}}
	}`
	updateResp := performVisualWorkflowRequest(t, router, http.MethodPatch, "/visual-sessions/"+sessionID, updateBody)
	assertTypedSessionProjection(t, updateResp, "lifestyle", "prompt_run_existing_1")

	getResp := performVisualWorkflowRequest(t, router, http.MethodGet, "/visual-sessions/"+sessionID, "")
	assertTypedSessionProjection(t, getResp, "lifestyle", "prompt_run_existing_1")

	listResp := performVisualWorkflowRequest(t, router, http.MethodGet, "/visual-sessions?product_id="+product.ID, "")
	items, ok := listResp["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one listed session, got %#v", listResp["items"])
	}
	listed, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected listed session object, got %#v", items[0])
	}
	assertTypedSessionProjection(t, listed, "lifestyle", "prompt_run_existing_1")

	cancelResp := performVisualWorkflowRequest(t, router, http.MethodPost, "/visual-sessions/"+sessionID+"/cancel", "")
	assertTypedSessionProjection(t, cancelResp, "lifestyle", "prompt_run_existing_1")
	if cancelResp["status"] != models.VisualWorkflowStatusCanceled {
		t.Fatalf("expected canceled response status, got %#v", cancelResp["status"])
	}
}

func TestPromptPlanRejectsPromptCenterArtifactsAndDoesNotLeak(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_1", "org_1", "SKU-1")
	session, err := service.CreateSession("user_1", "org_1", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{PromptPlan: &PromptPlanDTO{Status: "contract_needed", PromptID: "prompt_run_existing_1", Variables: map[string]any{"cta": "buy now"}}}); err != nil {
		t.Fatalf("prompt_id reference-only update should succeed: %v", err)
	}
	forbiddenPlans := []*PromptPlanDTO{
		{Status: "draft", Metadata: map[string]any{"compiled_prompt": "do not store"}},
		{Status: "draft", Variables: map[string]any{"nested": map[string]any{"source_map": map[string]any{"x": "y"}}}},
		{Status: "draft", SourceAssets: []PromptPlanSourceAssetDTO{{Metadata: map[string]any{"provider_response": "nope"}}}},
	}
	for _, plan := range forbiddenPlans {
		if _, err := service.UpdateSession("org_1", session.ID, UpdateSessionRequest{PromptPlan: plan}); err == nil {
			t.Fatalf("expected prompt center artifact rejection for %+v", plan)
		}
	}

	view, err := service.StageView("org_1", session.ID)
	if err != nil {
		t.Fatalf("stage view: %v", err)
	}
	encoded, _ := json.Marshal(view)
	body := string(encoded)
	for _, forbidden := range []string{"compiled_prompt", "source_map", "content_hash", "source_map_hash", "schema_hash", "provider_response", "run_response", "fake execution"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("stage-view leaked Prompt Center/execution artifact %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "prompt_run_existing_1") {
		t.Fatalf("stage-view should keep prompt_id reference: %s", body)
	}
}

func testCapabilityMatrix(items ...platform.RuntimeCapabilityItem) *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: items}
}

func hasBlocker(blockers []ReadinessBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

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
	// Simulate stale persisted data from before stricter validation existed.
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

func TestCreateStrategyReportJobCreatesPlatformTextRuntime(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_strategy", "org_strategy", "SKU-S")
	session, err := service.CreateSession("user_strategy", "org_strategy", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyStrategyReportMatrix(), runtimeJob: &platform.RuntimeJob{ID: "runtime-strategy-1", ProductCode: "ecommerce", TaskType: "strategy_report", Status: "queued", Stage: "queued"}}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateStrategyReportJob("org_strategy", session.ID, CreateStrategyReportJobRequest{Marketplace: "amazon", Locale: "en-US", ReportGoal: "positioning", SourceFacts: map[string]any{"competitor_count": 3}, IdempotencyKey: "strategy-1"})
	if err != nil {
		t.Fatalf("create strategy report: %v", err)
	}
	if resp.RuntimeJobID != "runtime-strategy-1" || len(fake.createInputs) != 1 {
		t.Fatalf("expected strategy runtime, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	input := fake.createInputs[0]
	if input.TaskType != "strategy_report" || input.SourceType != "visual_strategy_report" || input.SourceID != session.ID {
		t.Fatalf("unexpected strategy runtime input: %+v", input)
	}
	if strings.Contains(input.InputManifest, "storage_key") || strings.Contains(input.InputManifest, "provider_job_id") {
		t.Fatalf("strategy manifest leaked execution metadata: %s", input.InputManifest)
	}
}

func TestStrategyReportResultPersistsSanitizedMetadata(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_strategy_result", "org_strategy_result", "SKU-SR")
	session, err := service.CreateSession("user_strategy_result", "org_strategy_result", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	report := map[string]any{"schema_version": "ecommerce_strategy_report.v1", "status": "ready", "summary": "Premium positioning", "recommendations": []string{"lead with material"}}
	result, err := service.InternalRecordStrategyReportResults(session.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Stage: "completed", Variants: []map[string]any{{"inline_data": toJSONForTest(report)}}})
	if err != nil {
		t.Fatalf("record strategy report: %v", err)
	}
	metadata := result.Metadata
	strategy := metadata["strategy_report"].(map[string]any)
	if strategy["status"] != "completed" {
		t.Fatalf("strategy report not completed in metadata: %+v", strategy)
	}
	persisted := strategy["report"].(map[string]any)
	if persisted["summary"] != "Premium positioning" {
		t.Fatalf("strategy report summary not persisted: %+v", persisted)
	}
}

func TestCreateGenerationFanoutCreatesMatrixRuntimeJobs(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout", "org_fanout", "SKU-F")
	assets := []models.EcommerceAsset{
		{ID: "asset_fanout_1", OrganizationID: "org_fanout", UserID: "user_fanout", AssetType: "image", SourceType: "upload", StorageKey: "store/a.png", MimeType: "image/png", FileName: "a.png", Width: 1024, Height: 1024, Metadata: "{}"},
		{ID: "asset_fanout_2", OrganizationID: "org_fanout", UserID: "user_fanout", AssetType: "image", SourceType: "upload", StorageKey: "store/b.png", MimeType: "image/png", FileName: "b.png", Width: 1024, Height: 1024, Metadata: "{}"},
	}
	for i := range assets {
		if err := db.Create(&assets[i]).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
	}
	session, err := service.CreateSession("user_fanout", "org_fanout", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_fanout", TemplateID: "tpl_base", Variables: map[string]any{"prompt": "Generate ecommerce hero"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_fanout_1", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateGenerationFanout("org_fanout", session.ID, CreateGenerationFanoutRequest{IdempotencyKey: "fanout-1", SourceAssetIDs: []string{"asset_fanout_1", "asset_fanout_2"}, TemplateIDs: []string{"tpl_a", "tpl_b"}, RequestedVariants: 1, ProviderConfig: map[string]any{"resolution_id": "1024-square"}})
	if err != nil {
		t.Fatalf("create fanout: %v", err)
	}
	if len(resp.Items) != 4 || len(fake.createInputs) != 4 {
		t.Fatalf("expected 4 fanout items/runtime jobs, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	seen := map[string]bool{}
	for _, input := range fake.createInputs {
		manifest := decodeObject(input.InputManifest)
		params, _ := manifest["params_snapshot"].(map[string]any)
		if params["fanout_id"] != "fanout-1" || params["template_id"] == "" || params["source_asset_id"] == "" {
			t.Fatalf("fanout params missing from manifest: %#v", params)
		}
		seen[fmt.Sprint(params["source_asset_id"])+":"+fmt.Sprint(params["template_id"])] = true
		sourceIDs, _ := manifest["source_asset_ids"].([]any)
		if len(sourceIDs) != 1 || fmt.Sprint(sourceIDs[0]) != fmt.Sprint(params["source_asset_id"]) {
			t.Fatalf("fanout runtime should use exactly its selected source asset: manifest=%#v params=%#v", manifest["source_asset_ids"], params)
		}
		if strings.Contains(input.InputManifest, "provider_job_id") {
			t.Fatalf("fanout manifest leaked execution artifact: %s", input.InputManifest)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("fanout matrix did not cover all source/template pairs: %#v", seen)
	}
}

func TestCreateGenerationFanoutAllowsStoredSourceAssetWithUnknownDimensions(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_unknown_dims", "org_fanout_unknown_dims", "SKU-FU")
	asset := models.EcommerceAsset{ID: "asset_unknown_dims", OrganizationID: "org_fanout_unknown_dims", UserID: "user_fanout_unknown_dims", AssetType: "image", SourceType: "upload", StorageKey: "store/unknown.png", MimeType: "image/png", FileName: "unknown.png", Width: 0, Height: 0, Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	session, err := service.CreateSession("user_fanout_unknown_dims", "org_fanout_unknown_dims", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_unknown_dims", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_unknown_dims", TemplateID: "tpl_base", Variables: map[string]any{"prompt": "Generate ecommerce hero"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_unknown_dims", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateGenerationFanout("org_fanout_unknown_dims", session.ID, CreateGenerationFanoutRequest{
		TemplateSlots:     []GenerationFanoutTemplateSlotRequest{{SourceAssetID: "asset_unknown_dims", TemplateID: "tpl_a", SceneTag: "主图"}},
		RequestedVariants: 1,
	})
	if err != nil {
		t.Fatalf("create fanout with unknown dimensions should pass: %v", err)
	}
	if len(resp.Items) != 1 || len(fake.createInputs) != 1 {
		t.Fatalf("expected one runtime job for unknown dimension asset, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
}

func TestPromptComposerFromFixedQuestionsPersistsSections(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_prompt_composer", "org_prompt_composer", "SKU-PC")
	session, err := service.CreateSession("user_prompt_composer", "org_prompt_composer", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_prompt_composer", "org_prompt_composer", session.ID, src); err != nil {
			t.Fatalf("create source: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_pc_sku_product", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "product", ElementKey: "sku_product", Label: "SKU 梳子主体", ValueJSON: toJSONForTest(map[string]any{"description": "木柄宽齿梳主体，保留完整轮廓"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_pc_sku_bg", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "background", ElementKey: "sku_background", Label: "SKU 白底", ValueJSON: toJSONForTest(map[string]any{"description": "普通白色拍摄背景"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_pc_ref_product", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "product", ElementKey: "reference_product", Label: "参考香薰瓶", ValueJSON: toJSONForTest(map[string]any{"description": "参考图里的香薰瓶不要进入画面"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
		{ID: "vde_pc_ref_bg", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "background", ElementKey: "reference_background", Label: "参考木质浴室", ValueJSON: toJSONForTest(map[string]any{"description": "暖色木质浴室台面与柔和自然光"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	_, err = service.ApplyAttentionTree("org_prompt_composer", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{
		{ElementID: "vde_pc_sku_product", Decision: "keep", Question: "要不要 SKU 产品？", Answer: "yes", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product"}},
		{ElementID: "vde_pc_sku_bg", Decision: "drop", Question: "要不要 SKU 背景？", Answer: "no", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_background"}},
		{ElementID: "vde_pc_ref_product", Decision: "drop", Question: "要不要参考素材产品？", Answer: "no", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_product"}},
		{ElementID: "vde_pc_ref_bg", Decision: "keep", Question: "要不要参考素材背景？", Answer: "yes", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}},
	}, DriftControls: map[string]any{"sku_weight": 30, "reference_weight": 70}})
	if err != nil {
		t.Fatalf("apply fixed question decisions: %v", err)
	}
	model, err := service.repo.GetSession("org_prompt_composer", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"SKU 解析结果", "参考素材解析结果", "四问选择", "侧重配置", "SKU 梳子主体", "参考木质浴室", "不要使用 SKU 原图背景", "侧重参考素材 70%"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("composed prompt missing %q: %s", want, finalPrompt)
		}
	}
	sections, _ := plan.Variables["prompt_sections"].([]any)
	if len(sections) < 4 {
		t.Fatalf("expected prompt_sections to persist composer parts, got %#v", plan.Variables["prompt_sections"])
	}
}

func TestBuildIntentFusionInputManifestKeepsNonEmptyNeedsReviewReferenceAnalysis(t *testing.T) {
	manifest := buildIntentFusionInputManifest(
		[]IntentSourceReferenceDTO{{SourceReferenceID: "vsr_ref", Role: "reference"}, {SourceReferenceID: "vsr_sku", Role: "sku"}},
		[]IntentElementDTO{{ElementID: "fixed:reference_background", Decision: "keep", Label: "采用参考背景", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}}},
		[]models.EcommerceVisualDeconstructionElement{
			{ID: "vde_sku", ElementType: "product_fact", ElementKey: "visual_description", Label: "Visual description", Confidence: 0.95, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "purple earbuds"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
			{ID: "vde_ref", ElementType: "reference_strategy", ElementKey: "style", Label: "Reference style", Confidence: 0, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "minimalist product photography against white background with clean lines and soft shadows"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference", "source_reference_id": "vsr_ref"})},
		},
		nil,
	)
	entries := manifestEntries(manifest["reference_strategies"])
	if len(entries) != 1 {
		t.Fatalf("expected one reference strategy, got %#v", manifest["reference_strategies"])
	}
	text := promptComposerAnalysisText("参考素材解析结果", entries)
	if !strings.Contains(text, "minimalist product photography against white background with clean lines and soft shadows") {
		t.Fatalf("expected real weak reference analysis in prompt text, got %q from %#v", text, entries)
	}
	if strings.Contains(text, "按当前参考素材直接生成出图方案") {
		t.Fatalf("non-empty reference analysis should not be replaced by fallback: %q", text)
	}
}

func TestPromptComposerIncludesWeakReferenceAnalysisWhenUserKeepsReferenceStyle(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_reference_prompt", "org_reference_prompt", "SKU-RP")
	session, err := service.CreateSession("user_reference_prompt", "org_reference_prompt", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_reference_prompt", "org_reference_prompt", session.ID, src); err != nil {
			t.Fatalf("create source reference: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_rp_sku", OrganizationID: "org_reference_prompt", SessionID: session.ID, JobID: "vdj_rp", ElementType: "product_fact", ElementKey: "visual_description", Label: "Visual description", Confidence: 0.95, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "purple earbuds with charging case"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_rp_ref_product", OrganizationID: "org_reference_prompt", SessionID: session.ID, JobID: "vdj_rp", ElementType: "product_fact", ElementKey: "product_info", Label: "图片中的产品信息", Confidence: 0.8, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "一款黑色头戴式耳机，带麦克风支架和线缆。"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
		{ID: "vde_rp_ref", OrganizationID: "org_reference_prompt", SessionID: session.ID, JobID: "vdj_rp", ElementType: "reference_strategy", ElementKey: "style", Label: "Reference style", Confidence: 0, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "minimalist product photography against white background with clean lines and soft shadows"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	_, err = service.UpdateSession("org_reference_prompt", session.ID, UpdateSessionRequest{IntentSpec: &IntentSpecDTO{
		SchemaVersion: "visual_intent_spec.v1",
		Selections: []IntentElementDTO{
			{ElementID: "fixed:sku_product", Decision: "keep", Label: "保留 SKU 原图产品", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product", "source_element_id": "vde_rp_sku"}},
			{ElementID: "fixed:sku_background", Decision: "drop", Label: "保留 SKU 原图背景", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_background", "source_element_id": "vde_rp_sku"}},
			{ElementID: "fixed:reference_product", Decision: "drop", Label: "使用参考素材产品", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_product", "source_element_id": "vde_rp_ref_product"}},
			{ElementID: "fixed:reference_background", Decision: "keep", Label: "采用参考素材背景", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background", "source_element_id": "vde_rp_ref"}},
		},
	}})
	if err == nil {
		err = service.refreshIntentInputManifest("org_reference_prompt", session.ID, nil, nil)
	}
	if err != nil {
		t.Fatalf("apply fixed choices: %v", err)
	}
	model, err := service.repo.GetSession("org_reference_prompt", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	if strings.Contains(finalPrompt, "minimalist product photography against white background with clean lines and soft shadows") {
		// OK: selected reference background/style semantics should still enter the composed prompt.
	} else {
		t.Fatalf("reference analysis should enter composed prompt, got: %s", finalPrompt)
	}
	if strings.Contains(finalPrompt, "一款黑色头戴式耳机") {
		t.Fatalf("dropped reference product should not enter reference analysis prompt: %s", finalPrompt)
	}
	if strings.Contains(finalPrompt, "按当前参考素材直接生成出图方案") {
		t.Fatalf("reference fallback should not replace non-empty reference analysis: %s", finalPrompt)
	}
}

func TestSingleImageUnderstandingProjectsProviderPlaceholderRoleToPrimarySource(t *testing.T) {
	job := &models.EcommerceVisualDeconstructionJob{
		SourceReferenceID: "vsr_ref_primary",
		Metadata:          toJSONForTest(map[string]any{"image_understanding_policy": "single_image_per_runtime_job"}),
	}
	sourceIndex := map[string]string{"vsr_ref_primary": "reference"}
	elements := projectSingleImageUnderstandingElements(job, []InternalResultElementRequest{{
		SourceRole:        "sku|reference",
		SourceReferenceID: "SOURCE_REFERENCE_ID",
		ElementType:       "background",
		ElementKey:        "background_info",
		Label:             "图片中的背景信息",
		Value:             map[string]any{"description": "白色背景，柔和光线，极简商业摄影"},
		Readiness:         "ready",
	}}, sourceIndex)
	if len(elements) != 1 {
		t.Fatalf("placeholder provider role should be projected to primary source, got %#v", elements)
	}
	if elements[0].SourceRole != "reference" || elements[0].SourceReferenceID != "vsr_ref_primary" {
		t.Fatalf("expected reference projection, got role=%q source=%q", elements[0].SourceRole, elements[0].SourceReferenceID)
	}
}

func TestVisualDeconstructionPromptUsesConcreteSourceRoleAndID(t *testing.T) {
	prompt := visualDeconstructionUnderstandingPromptForSource("reference", "vsr_ref_primary")
	if strings.Contains(prompt, "sku|reference") || strings.Contains(prompt, "SOURCE_REFERENCE_ID") {
		t.Fatalf("prompt should not ask provider to echo placeholder role/id: %s", prompt)
	}
	for _, want := range []string{"source_role 必须填写 reference", "source_reference_id 必须填写 vsr_ref_primary"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing concrete source instruction %q: %s", want, prompt)
		}
	}
}

func TestCreatePromptPlannerJobUsesFallbackWhenManualTextMissing(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_low_quality_placeholder", "org_low_quality_placeholder", "SKU-LQP")
	session, err := service.CreateSession("user_low_quality_placeholder", "org_low_quality_placeholder", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_low_quality_placeholder", "org_low_quality_placeholder", session.ID, src); err != nil {
			t.Fatalf("create source reference: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_lqp_sku", OrganizationID: "org_low_quality_placeholder", SessionID: session.ID, JobID: "vdj_lqp", ElementType: "product_fact", ElementKey: "provider_visual_description", Label: "weak sku", Confidence: 0.1, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"provider_text": "{}"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_lqp_ref", OrganizationID: "org_low_quality_placeholder", SessionID: session.ID, JobID: "vdj_lqp", ElementType: "reference_strategy", ElementKey: "style", Label: "weak ref", Confidence: 0.1, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": ""}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed low quality element: %v", err)
		}
	}
	_, err = service.ApplyAttentionTree("org_low_quality_placeholder", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{
		{ElementID: "vde_lqp_sku", Decision: "keep", Question: "保留 SKU 原图产品？", Answer: "要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product"}},
		{ElementID: "vde_lqp_sku", Decision: "drop", Question: "保留 SKU 原图背景？", Answer: "不要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_background"}},
		{ElementID: "vde_lqp_ref", Decision: "drop", Question: "使用参考素材产品？", Answer: "不要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_product"}},
		{ElementID: "vde_lqp_ref", Decision: "keep", Question: "采用参考素材背景？", Answer: "要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}},
	}})
	if err != nil {
		t.Fatalf("apply fixed choices: %v", err)
	}
	service.WithPromptSnapshotCreator(&fakePromptSnapshotCreator{response: &promptcenter.PromptRunResponse{PromptID: "prompt_lq_default", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_lq_default", SceneType: "product_composite", Status: "validated"}})
	resp, err := service.CreatePromptPlannerJob("org_low_quality_placeholder", session.ID, CreatePromptPlannerJobRequest{Marketplace: "amazon", Locale: "zh-CN", IdempotencyKey: "prompt-placeholder"})
	if err != nil {
		t.Fatalf("prompt planner with fallback image intent should not error: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("expected fallback prompt composition to complete, got %+v", resp)
	}
	model, err := service.repo.GetSession("org_low_quality_placeholder", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"按当前 SKU 图片直接生成", "按当前参考素材直接生成"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("fallback prompt missing %q: %s", want, finalPrompt)
		}
	}
}

func TestCreateGenerationFanoutUsesPromptComposerFinalPrompt(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_composer", "org_fanout_composer", "SKU-FC")
	asset := models.EcommerceAsset{ID: "asset_fanout_composer", OrganizationID: "org_fanout_composer", UserID: "user_fanout_composer", AssetType: "image", SourceType: "upload", StorageKey: "store/composer.png", MimeType: "image/png", FileName: "composer.png", Width: 1024, Height: 1024, Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	session, err := service.CreateSession("user_fanout_composer", "org_fanout_composer", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_composer", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_composer", TemplateID: "tpl_base", Variables: map[string]any{"composed_prompt_text": "基础要求：保留 SKU 产品，采用参考素材背景。"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_fanout_composer", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	_, err = service.CreateGenerationFanout("org_fanout_composer", session.ID, CreateGenerationFanoutRequest{
		TemplateSlots:   []GenerationFanoutTemplateSlotRequest{{SourceAssetID: "asset_fanout_composer", TemplateID: "tpl_lifestyle", SceneTag: "生活方式图", DetailRequirement: "模板要求：浴室台面构图", NegativeRequirement: "不要文字水印"}},
		PromptVariables: map[string]any{"prompt_composer": map[string]any{"diy_prompt_text": "手动补充：高端酒店氛围", "negative_prompt_text": "不要畸形手指"}},
	})
	if err != nil {
		t.Fatalf("create fanout: %v", err)
	}
	if len(fake.createInputs) != 1 {
		t.Fatalf("expected one runtime job, got %d", len(fake.createInputs))
	}
	manifest := decodeObject(fake.createInputs[0].InputManifest)
	promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
	userPrompt := fmt.Sprint(promptSnapshot["user_prompt"])
	for _, want := range []string{"基础要求", "模板要求：浴室台面构图", "手动补充：高端酒店氛围"} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("runtime user_prompt missing %q: %s", want, userPrompt)
		}
	}
	stylePrompt := fmt.Sprint(promptSnapshot["style_prompt"])
	if !strings.Contains(stylePrompt, "不要文字水印") || !strings.Contains(stylePrompt, "不要畸形手指") {
		t.Fatalf("runtime negative prompt missing slot/manual negatives: %s", stylePrompt)
	}
}

func TestCreateGenerationFanoutIncludesReferenceAssetsAndTemplateSpecificRuntimeParams(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_diversity", "org_fanout_diversity", "SKU-FD")
	for _, asset := range []models.EcommerceAsset{
		{ID: "asset_diversity_sku", OrganizationID: "org_fanout_diversity", UserID: "user_fanout_diversity", AssetType: "image", SourceType: "upload", StorageKey: "store/sku.png", MimeType: "image/png", FileName: "sku.png", Width: 1200, Height: 1200, Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "asset_diversity_ref", OrganizationID: "org_fanout_diversity", UserID: "user_fanout_diversity", AssetType: "image", SourceType: "upload", StorageKey: "store/ref.png", MimeType: "image/png", FileName: "ref.png", Width: 1200, Height: 1200, Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&asset).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
	}
	session, err := service.CreateSession("user_fanout_diversity", "org_fanout_diversity", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_diversity", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_diversity", TemplateID: "tpl_base", Variables: map[string]any{"composed_prompt_text": "基础要求：白色头戴式耳机，参考深色桌面氛围与侧逆光。"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_diversity_sku", Role: "sku"}, {AssetID: "asset_diversity_ref", Role: "reference"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	_, err = service.CreateGenerationFanout("org_fanout_diversity", session.ID, CreateGenerationFanoutRequest{
		IdempotencyKey: "fanout-diversity",
		TemplateSlots: []GenerationFanoutTemplateSlotRequest{
			{SourceAssetID: "asset_diversity_sku", TemplateID: "amazon-hero", SceneTag: "主图"},
			{SourceAssetID: "asset_diversity_sku", TemplateID: "industrial-poster", SceneTag: "海报"},
			{SourceAssetID: "asset_diversity_sku", TemplateID: "lifestyle-scene", SceneTag: "使用图"},
		},
		RequestedVariants: 1,
		ProviderConfig:    map[string]any{"resolution_id": "1024-square"},
	})
	if err != nil {
		t.Fatalf("create fanout diversity: %v", err)
	}
	wantDims := map[string]string{"amazon-hero": "1024x1024", "industrial-poster": "768x1024", "lifestyle-scene": "1365x768"}
	for _, input := range fake.createInputs {
		manifest := decodeObject(input.InputManifest)
		params, _ := manifest["params_snapshot"].(map[string]any)
		templateID := fmt.Sprint(params["template_id"])
		gotDims := fmt.Sprintf("%vx%v", params["width"], params["height"])
		if gotDims != wantDims[templateID] {
			t.Fatalf("template %s should use distinct runtime dimensions %s, got %s params=%#v", templateID, wantDims[templateID], gotDims, params)
		}
		sourceIDs, _ := manifest["source_asset_ids"].([]any)
		if len(sourceIDs) != 2 || fmt.Sprint(sourceIDs[0]) != "asset_diversity_sku" || fmt.Sprint(sourceIDs[1]) != "asset_diversity_ref" {
			t.Fatalf("fanout runtime should include selected SKU plus reference asset, got %#v", sourceIDs)
		}
		promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
		userPrompt := fmt.Sprint(promptSnapshot["user_prompt"])
		for _, want := range []string{"强制差异化", "不得与其他槽位构图相同", templateID} {
			if !strings.Contains(userPrompt, want) {
				t.Fatalf("fanout prompt missing diversity marker %q for template %s: %s", want, templateID, userPrompt)
			}
		}
	}
}

func TestCreateGenerationFanoutHonorsExplicitTemplateSlots(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_slots", "org_fanout_slots", "SKU-FS")
	for _, asset := range []models.EcommerceAsset{
		{ID: "asset_slot_1", OrganizationID: "org_fanout_slots", UserID: "user_fanout_slots", AssetType: "image", SourceType: "upload", StorageKey: "store/slot-a.png", MimeType: "image/png", FileName: "slot-a.png", Width: 1024, Height: 1024, Metadata: "{}"},
		{ID: "asset_slot_2", OrganizationID: "org_fanout_slots", UserID: "user_fanout_slots", AssetType: "image", SourceType: "upload", StorageKey: "store/slot-b.png", MimeType: "image/png", FileName: "slot-b.png", Width: 1024, Height: 1024, Metadata: "{}"},
	} {
		if err := db.Create(&asset).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
	}
	session, err := service.CreateSession("user_fanout_slots", "org_fanout_slots", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_slots", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_slots", TemplateID: "tpl_base", Variables: map[string]any{"prompt": "Generate ecommerce hero"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_slot_1", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateGenerationFanout("org_fanout_slots", session.ID, CreateGenerationFanoutRequest{
		IdempotencyKey: "fanout-slots",
		SourceAssetIDs: []string{"asset_slot_1", "asset_slot_2"},
		TemplateIDs:    []string{"tpl_a", "tpl_b", "tpl_c"},
		TemplateSlots: []GenerationFanoutTemplateSlotRequest{
			{SourceAssetID: "asset_slot_1", TemplateID: "tpl_a", SceneTag: "主图", DetailRequirement: "主体完整且白底", NegativeRequirement: "不要文字水印"},
			{SourceAssetID: "asset_slot_2", TemplateID: "tpl_b", SceneTag: "细节图", DetailRequirement: "突出金属纹理"},
			{SourceAssetID: "asset_slot_1", TemplateID: "tpl_c", SceneTag: "使用图", DetailRequirement: "展示真实使用场景"},
		},
		RequestedVariants: 1,
	})
	if err != nil {
		t.Fatalf("create explicit fanout: %v", err)
	}
	if len(resp.Items) != 3 || len(fake.createInputs) != 3 {
		t.Fatalf("expected exactly 3 explicit fanout jobs, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	for idx, input := range fake.createInputs {
		manifest := decodeObject(input.InputManifest)
		params, _ := manifest["params_snapshot"].(map[string]any)
		if fmt.Sprint(params["fanout_total"]) != "3" || fmt.Sprint(params["fanout_index"]) != fmt.Sprint(idx) {
			t.Fatalf("explicit fanout index/total mismatch: %#v", params)
		}
		if fmt.Sprint(params["scene_tag"]) == "" || fmt.Sprint(params["detail_requirement"]) == "" {
			t.Fatalf("explicit fanout slot requirements missing from runtime params: %#v", params)
		}
		promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
		if !strings.Contains(fmt.Sprint(promptSnapshot["user_prompt"]), fmt.Sprint(params["detail_requirement"])) {
			t.Fatalf("slot detail requirement not appended to provider prompt: prompt=%#v params=%#v", promptSnapshot, params)
		}
		if idx == 0 && !strings.Contains(fmt.Sprint(promptSnapshot["style_prompt"]), "不要文字水印") {
			t.Fatalf("slot negative requirement not appended to negative prompt: %#v", promptSnapshot)
		}
	}
}

func TestApplyAttentionTreePersistsLayeredDecisionNodes(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_tree", "org_tree", "SKU-T")
	session, err := service.CreateSession("user_tree", "org_tree", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_root", OrganizationID: "org_tree", SessionID: session.ID, JobID: "vdj_tree", ElementType: "style", ElementKey: "root", Label: "root"},
		{ID: "vde_child", OrganizationID: "org_tree", SessionID: session.ID, JobID: "vdj_tree", ElementType: "object", ElementKey: "child", Label: "child"},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	layer0 := 0
	layer1 := 1
	confidence := 0.9
	_, err = service.ApplyAttentionTree("org_tree", session.ID, ApplyAttentionTreeRequest{TreeID: "tree-1", RoundID: "round-1", Decisions: []AttentionDecisionInput{
		{ElementID: "vde_root", Decision: "keep", DecisionNodeID: "node-root", Layer: &layer0, Question: "Keep root?", Answer: "yes", Confidence: &confidence},
		{ElementID: "vde_child", Decision: "replace", DecisionNodeID: "node-child", ParentNodeID: "node-root", Layer: &layer1, Path: []string{"root", "child"}, Question: "Replace child?", Answer: "replace"},
	}})
	if err != nil {
		t.Fatalf("apply layered attention tree: %v", err)
	}
	model, err := service.repo.GetSession("org_tree", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	intent := decodeIntentSpec(model.IntentSpecJSON, model)
	manifest, _ := intent.Metadata["input_manifest"].(map[string]any)
	tree, _ := manifest["decision_tree"].(map[string]any)
	nodes, _ := tree["nodes"].([]any)
	if tree["schema_version"] != "visual-decision-tree.v1" || len(nodes) != 2 {
		t.Fatalf("decision tree projection missing: %#v", tree)
	}
	if _, err := service.ApplyAttentionTree("org_tree", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{{ElementID: "vde_root", Decision: "keep", DecisionNodeID: "bad-root", ParentNodeID: "parent", Layer: &layer0}}}); err == nil {
		t.Fatalf("expected invalid root parent error")
	}
}

func readyVisualGenerationMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "image_generation", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func readyIntentPlanningMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "intent_planning", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func readyPromptPlanningMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "prompt_planning", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func readyStrategyReportMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "strategy_report", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func toJSONForTest(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSaveGenerationVersionAsTemplateFromCompletedResult(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	if err := db.AutoMigrate(&models.SavedTemplate{}); err != nil {
		t.Fatalf("migrate saved template: %v", err)
	}
	workspaceRepo := repository.NewWorkspaceRepository(db)
	service.WithWorkspaceRepository(workspaceRepo)
	product, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_tpl_1")
	completedStatus := "completed"
	completedStage := "result_available"
	selectedAssetID := "asset_tpl_1"
	version, err := service.UpdateGenerationVersion("org_1", session.ID, version.VersionID, UpdateGenerationVersionRequest{Status: &completedStatus, Stage: &completedStage, ResultAssets: []ResultAssetDTO{{AssetID: selectedAssetID, AssetContentURL: "/api/v1/ecommerce/assets/" + selectedAssetID + "/content"}}, SelectedResultAssetID: &selectedAssetID})
	if err != nil {
		t.Fatalf("prepare completed version: %v", err)
	}
	resp, err := service.SaveGenerationVersionAsTemplate("user_1", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{Title: "Hero template", IdempotencyKey: "idem-template"})
	if err != nil {
		t.Fatalf("save template: %v", err)
	}
	if resp.ProductID != product.ID || resp.SelectedResultAssetID != "asset_tpl_1" || resp.Template.SourceType != "visual_generation_result" {
		t.Fatalf("unexpected template response: %+v", resp)
	}
	if resp.Template.ID == "" || resp.Template.ZH.Title != "Hero template" || resp.AssetContentURL == "" {
		t.Fatalf("missing template fields: %+v", resp)
	}
	items, err := workspaceRepo.ListSavedTemplates(repository.Scope{UserID: "user_1", OrgID: "org_1"})
	if err != nil || len(items) != 1 {
		t.Fatalf("saved templates not persisted: len=%d err=%v", len(items), err)
	}
	replay, err := service.SaveGenerationVersionAsTemplate("user_1", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{Title: "Hero template", IdempotencyKey: "idem-template"})
	if err != nil {
		t.Fatalf("replay save template: %v", err)
	}
	if replay.Template.ID != resp.Template.ID || len(replay.SavedTemplates) != 1 {
		t.Fatalf("expected idempotent template save, got first=%s replay=%s len=%d", resp.Template.ID, replay.Template.ID, len(replay.SavedTemplates))
	}
}

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
