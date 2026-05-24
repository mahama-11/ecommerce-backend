package visualworkflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
)

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
