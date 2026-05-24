package visualworkflow

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"ecommerce-service/internal/billinggate"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/promptcenter"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

type RuntimeCapabilityReader interface {
	ListRuntimeCapabilities(productCode, taskType string) (*platform.RuntimeCapabilityMatrix, error)
}

type RuntimeJobCreator interface {
	CreateRuntimeJob(input platform.CreateRuntimeJobInput) (*platform.RuntimeJob, error)
}

type RuntimeOrchestrator interface {
	RuntimeCapabilityReader
	RuntimeJobCreator
}

type RuntimeBillingGateway interface {
	CreateChargeSession(platform.CreateChargeSessionInput) (*platform.ChargeSession, error)
	UpdateChargeSession(string, platform.UpdateChargeSessionInput) (*platform.ChargeSession, error)
	ReserveResources(platform.ReserveInput) (*platform.ResourceReservation, error)
	ReleaseReservation(string) (*platform.ResourceReservation, error)
	FinalizeMetering(platform.FinalizeInput) (*platform.FinalizeResult, error)
}

type PromptSnapshotCreator interface {
	Preview(userID, orgID string, input promptcenter.PreviewPromptInput) (*promptcenter.PromptRunResponse, error)
}

var (
	ErrInternalCallbackInvalid = errors.New("visual workflow internal callback invalid")
)

func IsInternalCallbackInvalid(err error) bool {
	return errors.Is(err, ErrInternalCallbackInvalid)
}

func IsInternalCallbackNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

const (
	safePlatformCapabilityErrorMessage       = "Platform runtime capability check failed; retry later or contact support."
	safePlatformRuntimeJobCreateErrorMessage = "Platform runtime job creation failed; no provider call was made by Ecommerce."
)

type Service struct {
	repo             *repository.VisualWorkflowRepository
	productRepo      *repository.ProductCenterRepository
	assetRepo        *repository.ImageRuntimeRepository
	promptRepo       *repository.PromptCenterRepository
	workspaceRepo    *repository.WorkspaceRepository
	capabilityReader RuntimeCapabilityReader
	runtimeCreator   RuntimeJobCreator
	promptCreator    PromptSnapshotCreator
	generationLocks  sync.Map
}

func NewService(repo *repository.VisualWorkflowRepository, productRepo *repository.ProductCenterRepository, assetRepo *repository.ImageRuntimeRepository) *Service {
	return &Service{repo: repo, productRepo: productRepo, assetRepo: assetRepo}
}

func (s *Service) WithPromptRepository(promptRepo *repository.PromptCenterRepository) *Service {
	s.promptRepo = promptRepo
	return s
}

func (s *Service) WithPromptSnapshotCreator(creator PromptSnapshotCreator) *Service {
	s.promptCreator = creator
	return s
}

func (s *Service) WithWorkspaceRepository(workspaceRepo *repository.WorkspaceRepository) *Service {
	s.workspaceRepo = workspaceRepo
	return s
}

func (s *Service) WithRuntimeCapabilityReader(reader RuntimeCapabilityReader) *Service {
	s.capabilityReader = reader
	if creator, ok := reader.(RuntimeJobCreator); ok {
		s.runtimeCreator = creator
	}
	return s
}

func (s *Service) WithRuntimeOrchestrator(orchestrator RuntimeOrchestrator) *Service {
	s.capabilityReader = orchestrator
	s.runtimeCreator = orchestrator
	return s
}

func (s *Service) CreateSession(userID, orgID string, req CreateSessionRequest) (*models.EcommerceVisualWorkflowSession, error) {
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		if existing, err := s.repo.FindSessionByIdempotencyKey(orgID, strings.TrimSpace(req.IdempotencyKey)); err == nil {
			return existing, nil
		} else if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}
	product, err := s.requireBoundProduct(orgID, req.ProductID, req.SKUCode)
	if err != nil {
		return nil, err
	}
	item := &models.EcommerceVisualWorkflowSession{
		ID:                     buildID("vws"),
		OrganizationID:         orgID,
		UserID:                 userID,
		ProductID:              product.ID,
		SKUCode:                product.SKUCode,
		ToolSlug:               strings.TrimSpace(req.ToolSlug),
		TemplateID:             strings.TrimSpace(req.TemplateID),
		TemplateVersionID:      strings.TrimSpace(req.TemplateVersionID),
		CurrentStage:           models.VisualWorkflowStageSource,
		Status:                 models.VisualWorkflowStatusDraft,
		ReadinessJSON:          mustJSON(defaultReadiness()),
		IntentSpecJSON:         encodeIntentSpec(defaultIntentSpec(itemFromCreateRequest(product, req))),
		PromptPlanJSON:         encodePromptPlan(defaultPromptPlan(itemFromCreateRequest(product, req))),
		GenerationVersionsJSON: "[]",
		IdempotencyKey:         strings.TrimSpace(req.IdempotencyKey),
		Metadata:               "{}",
	}
	return item, s.repo.CreateSession(item)
}

func (s *Service) ListSessions(orgID string, filter repository.VisualWorkflowSessionFilter) ([]models.EcommerceVisualWorkflowSession, error) {
	return s.repo.ListSessions(orgID, filter)
}

func (s *Service) GetSession(orgID, sessionID string) (*models.EcommerceVisualWorkflowSession, error) {
	return s.repo.GetSession(orgID, sessionID)
}

func (s *Service) UpdateSession(orgID, sessionID string, req UpdateSessionRequest) (*models.EcommerceVisualWorkflowSession, error) {
	item, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	if req.CurrentStage != "" {
		stage := strings.TrimSpace(req.CurrentStage)
		if !validVisualWorkflowStage(stage) {
			return nil, fmt.Errorf("invalid current_stage: %s", stage)
		}
		item.CurrentStage = stage
	}
	if req.Status != "" {
		status := strings.TrimSpace(req.Status)
		if !validVisualWorkflowStatus(status) {
			return nil, fmt.Errorf("invalid status: %s", status)
		}
		item.Status = status
	}
	if strings.TrimSpace(req.TemplateID) != "" {
		item.TemplateID = strings.TrimSpace(req.TemplateID)
	}
	if strings.TrimSpace(req.TemplateVersionID) != "" {
		item.TemplateVersionID = strings.TrimSpace(req.TemplateVersionID)
	}
	if req.Readiness != nil {
		if err := validateReadinessMap(req.Readiness); err != nil {
			return nil, err
		}
		item.ReadinessJSON = mustJSON(req.Readiness)
	}
	if req.IntentSpec != nil {
		intentSpec := *req.IntentSpec
		applyIntentSpecDefaults(&intentSpec, item)
		if err := validateIntentSpec(&intentSpec); err != nil {
			return nil, err
		}
		item.IntentSpecJSON = encodeIntentSpec(&intentSpec)
	}
	if req.PromptPlan != nil {
		promptPlan := *req.PromptPlan
		applyPromptPlanDefaults(&promptPlan, item)
		if err := validateClientPromptPlan(&promptPlan); err != nil {
			return nil, err
		}
		item.PromptPlanJSON = encodePromptPlan(&promptPlan)
	}
	if req.GenerationVersions != nil {
		for i := range req.GenerationVersions {
			if err := rejectClientGenerationVersionJobReferences(&req.GenerationVersions[i]); err != nil {
				return nil, err
			}
			if err := validateGenerationVersion(&req.GenerationVersions[i]); err != nil {
				return nil, err
			}
		}
		encoded, err := marshalGenerationVersions(req.GenerationVersions)
		if err != nil {
			return nil, err
		}
		item.GenerationVersionsJSON = encoded
	}
	if req.Metadata != nil {
		item.Metadata = mustJSON(req.Metadata)
	}
	return item, s.repo.SaveSession(item)
}

func (s *Service) CancelSession(orgID, sessionID string) (*models.EcommerceVisualWorkflowSession, error) {
	item, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	item.Status = models.VisualWorkflowStatusCanceled
	return item, s.repo.SaveSession(item)
}

func (s *Service) CreateSourceReference(userID, orgID, sessionID string, req CreateSourceReferenceRequest) (*models.EcommerceVisualSourceReference, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(req.SourceKind)
	if kind == "" {
		return nil, fmt.Errorf("source_kind is required")
	}
	if !validVisualSourceKind(kind) {
		return nil, fmt.Errorf("invalid source_kind: %s", kind)
	}
	status := models.VisualSourceStatusReady
	resolveStatus := models.VisualSourceStatusReady
	errorCode, errorMessage := "", ""
	metadata := sanitizeDeconstructionRequestMetadata(req.Metadata)
	var storageKey, mimeType string
	if kind == models.VisualSourceKindURL {
		probe, probeErr := resolveURLMetadata(strings.TrimSpace(req.SourceURL))
		if probeErr != nil {
			status = models.VisualSourceStatusContractNeeded
			resolveStatus = models.VisualSourceStatusContractNeeded
			errorCode = "SOURCE_RESOLVE_BLOCKED"
			errorMessage = probeErr.Error()
			metadata["unavailable_reason"] = "source-resolve-blocked"
		} else {
			metadata["url_metadata"] = probe
			mimeType = strings.TrimSpace(fmt.Sprint(probe["content_type"]))
		}
	} else if kind == models.VisualSourceKindVideoFrame {
		status = models.VisualSourceStatusContractNeeded
		resolveStatus = models.VisualSourceStatusContractNeeded
		errorCode = "CONTRACT_NEEDED"
		errorMessage = "Video frame analysis is not implemented until platform source/runtime contracts are finalized"
		metadata["unavailable_reason"] = "contract-needed"
	}
	if req.AssetID != "" {
		asset, err := s.assetRepo.FindAssetByID(orgID, strings.TrimSpace(req.AssetID))
		if err != nil {
			return nil, fmt.Errorf("asset not found or not in organization: %w", err)
		}
		storageKey = asset.StorageKey
		mimeType = asset.MimeType
		if req.AssetRelationID != "" {
			rel, err := s.productRepo.GetProductAssetRelation(repository.Scope{OrgID: orgID}, session.ProductID, strings.TrimSpace(req.AssetRelationID))
			if err != nil {
				return nil, fmt.Errorf("asset relation not bound to product: %w", err)
			}
			if rel.AssetID != asset.ID {
				return nil, fmt.Errorf("asset relation does not match asset")
			}
		} else if _, err := s.productRepo.FindProductAssetRelation(repository.Scope{OrgID: orgID}, session.ProductID, asset.ID); err != nil {
			return nil, fmt.Errorf("asset is not bound to product: %w", err)
		}
	}
	if mimeType == "" {
		mimeType = strings.TrimSpace(req.MimeType)
	}
	item := &models.EcommerceVisualSourceReference{
		ID:              buildID("vsr"),
		OrganizationID:  orgID,
		UserID:          userID,
		SessionID:       session.ID,
		ProductID:       session.ProductID,
		SKUCode:         session.SKUCode,
		SourceKind:      kind,
		SourceRef:       strings.TrimSpace(req.SourceRef),
		SourceURL:       strings.TrimSpace(req.SourceURL),
		AssetID:         strings.TrimSpace(req.AssetID),
		AssetRelationID: strings.TrimSpace(req.AssetRelationID),
		StorageKey:      storageKey,
		MimeType:        mimeType,
		Status:          status,
		ResolveStatus:   resolveStatus,
		ErrorCode:       errorCode,
		ErrorMessage:    errorMessage,
		Metadata:        mustJSON(metadata),
	}
	if err := s.repo.CreateSourceReference(item); err != nil {
		return nil, err
	}
	session.CurrentStage = models.VisualWorkflowStageDeconstruction
	_ = s.repo.SaveSession(session)
	return item, nil
}

func (s *Service) ListSourceReferences(orgID, sessionID string) ([]models.EcommerceVisualSourceReference, error) {
	if _, err := s.repo.GetSession(orgID, sessionID); err != nil {
		return nil, err
	}
	return s.repo.ListSourceReferences(orgID, sessionID)
}

func (s *Service) ArchiveSourceReference(orgID, sessionID, sourceID string) (*models.EcommerceVisualSourceReference, error) {
	item, err := s.repo.GetSourceReference(orgID, sessionID, sourceID)
	if err != nil {
		return nil, err
	}
	item.Status = models.VisualSourceStatusArchived
	item.ResolveStatus = models.VisualSourceStatusArchived
	metadata := decodeObject(item.Metadata)
	metadata["archived_from"] = "production-prep-source-remove"
	item.Metadata = mustJSON(metadata)
	if err := s.repo.SaveSourceReference(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) UpdateSourceReference(orgID, sessionID, sourceID string, req UpdateSourceReferenceRequest) (*models.EcommerceVisualSourceReference, error) {
	item, err := s.repo.GetSourceReference(orgID, sessionID, sourceID)
	if err != nil {
		return nil, err
	}
	if req.Status != "" {
		status := strings.TrimSpace(req.Status)
		if !validVisualSourceStatus(status) {
			return nil, fmt.Errorf("invalid status: %s", status)
		}
		item.Status = status
	}
	if req.ResolveStatus != "" {
		resolveStatus := strings.TrimSpace(req.ResolveStatus)
		if !validVisualSourceStatus(resolveStatus) {
			return nil, fmt.Errorf("invalid resolve_status: %s", resolveStatus)
		}
		item.ResolveStatus = resolveStatus
	}
	if req.ErrorCode != "" {
		item.ErrorCode = strings.TrimSpace(req.ErrorCode)
	}
	if req.ErrorMessage != "" {
		item.ErrorMessage = strings.TrimSpace(req.ErrorMessage)
	}
	if req.Metadata != nil {
		item.Metadata = mustJSON(sanitizeDeconstructionRequestMetadata(req.Metadata))
	}
	return item, s.repo.SaveSourceReference(item)
}

func (s *Service) CreateDeconstructionJob(userID, orgID, sessionID string, req CreateDeconstructionJobRequest) (*models.EcommerceVisualDeconstructionJob, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	sources, err := s.repo.ListSourceReferences(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	dualTrack, err := resolveDualTrackSourceReferences(sources, strings.TrimSpace(req.SourceReferenceID))
	if err != nil {
		return nil, err
	}
	sourceID := dualTrack.PrimarySourceReferenceID
	idempotencyKey := deconstructionIdempotencyKey(orgID, session.ID, dualTrack.Signature, req)
	if existing, err := s.repo.FindDeconstructionJobByIdempotencyKey(orgID, sessionID, idempotencyKey); err == nil {
		if canRetryContractNeededDeconstructionJob(existing) {
			ready, blockerCode, blockerMessage, blockerMetadata := s.deconstructionRuntimeCapabilityReady()
			if !ready {
				existing.Status = models.VisualDeconstructionStatusContractNeeded
				existing.Stage = "contract_needed"
				existing.StageMessage = blockerMessage
				existing.ErrorCode = blockerCode
				existing.ErrorMessage = blockerMessage
				existing.Metadata = mustJSON(mergeObjectMaps(decodeObject(existing.Metadata), blockerMetadata, map[string]any{"unavailable_reason": "contract-needed"}))
				_ = s.repo.SaveDeconstructionJob(existing)
				return existing, nil
			}
			manifest := decodeObject(existing.InputManifestJSON)
			if err := s.createPlatformDeconstructionRuntimeJob(existing, manifest); err != nil {
				return nil, err
			}
			session.CurrentStage = models.VisualWorkflowStageDeconstruction
			session.Status = models.VisualWorkflowStatusProcessing
			_ = s.repo.SaveSession(session)
		}
		return existing, nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	manifest := map[string]any{
		"schema_version":              "ecommerce.visual_deconstruction.v2",
		"session_id":                  session.ID,
		"product_id":                  session.ProductID,
		"sku_code":                    session.SKUCode,
		"source_reference_id":         sourceID,
		"source_reference_ids":        dualTrack.SourceReferenceIDs,
		"source_references":           dualTrack.ManifestSources,
		"input_mode":                  "dual_track_sources",
		"required_tracks":             []string{"sku", "reference"},
		"requested_elements":          req.RequestedElements,
		"source_role_output_required": true,
		"output": map[string]any{
			"include_bounding_boxes": true,
			"include_masks":          false,
			"schema_version":         "visual-deconstruction.v2",
			"element_fields":         []string{"source_role", "source_reference_id", "source_asset_id", "element_type", "element_key", "value"},
		},
	}
	metadata := sanitizeDeconstructionRequestMetadata(req.Metadata)
	metadata["platform_product_code"] = "ecommerce"
	metadata["platform_task_type"] = "image_understanding"
	metadata["source_reference_ids"] = dualTrack.SourceReferenceIDs
	metadata["source_roles"] = dualTrack.SourceRoles
	item := &models.EcommerceVisualDeconstructionJob{
		ID:                 buildID("vdj"),
		OrganizationID:     orgID,
		UserID:             userID,
		SessionID:          session.ID,
		ProductID:          session.ProductID,
		SKUCode:            session.SKUCode,
		SourceReferenceID:  sourceID,
		Status:             models.VisualDeconstructionStatusQueued,
		Stage:              "queued",
		StageMessage:       "Dual-track visual deconstruction job is queued for Platform runtime orchestration",
		Progress:           0,
		CapabilityCode:     "visual_deconstruction",
		RuntimeTaskType:    "image_understanding",
		InputManifestJSON:  mustJSON(manifest),
		OutputManifestJSON: "{}",
		IdempotencyKey:     idempotencyKey,
		Metadata:           mustJSON(metadata),
	}
	ready, blockerCode, blockerMessage, blockerMetadata := s.deconstructionRuntimeCapabilityReady()
	if !ready {
		item.Status = models.VisualDeconstructionStatusContractNeeded
		item.Stage = "contract_needed"
		item.StageMessage = blockerMessage
		item.ErrorCode = blockerCode
		item.ErrorMessage = blockerMessage
		item.Metadata = mustJSON(mergeObjectMaps(metadata, blockerMetadata, map[string]any{"unavailable_reason": "contract-needed"}))
	}
	createdJobID := item.ID
	if err := s.repo.CreateDeconstructionJob(item); err != nil {
		return nil, err
	}
	if item.ID != createdJobID {
		return item, nil
	}
	if ready {
		if err := s.createPlatformDeconstructionRuntimeJob(item, manifest); err != nil {
			return nil, err
		}
	}
	session.CurrentStage = models.VisualWorkflowStageDeconstruction
	if item.Status == models.VisualDeconstructionStatusContractNeeded {
		session.Status = models.VisualWorkflowStatusBlocked
	} else {
		session.Status = models.VisualWorkflowStatusProcessing
	}
	_ = s.repo.SaveSession(session)
	return item, nil
}

func canRetryContractNeededDeconstructionJob(item *models.EcommerceVisualDeconstructionJob) bool {
	if item == nil {
		return false
	}
	return item.Status == models.VisualDeconstructionStatusContractNeeded && strings.TrimSpace(item.RuntimeJobID) == "" && strings.TrimSpace(item.InputManifestJSON) != ""
}

func (s *Service) deconstructionRuntimeCapabilityReady() (bool, string, string, map[string]any) {
	metadata := map[string]any{
		"platform_product_code": "ecommerce",
		"platform_task_type":    "image_understanding",
	}
	if s.capabilityReader == nil || s.runtimeCreator == nil {
		return false, "CONTRACT_NEEDED", "Platform runtime orchestration client is not configured; no provider call was made", metadata
	}
	matrix, err := s.capabilityReader.ListRuntimeCapabilities("ecommerce", "image_understanding")
	if err != nil {
		metadata["platform_blocker"] = map[string]any{"code": "PLATFORM_CAPABILITY_ERROR", "message": safePlatformCapabilityErrorMessage}
		return false, "PLATFORM_CAPABILITY_ERROR", safePlatformCapabilityErrorMessage, metadata
	}
	item, ok := runtimeCapabilityForTask(matrix, "image_understanding")
	if !ok {
		metadata["platform_blocker"] = map[string]any{"code": "PLATFORM_CAPABILITY_UNAVAILABLE", "message": "Platform runtime capability image_understanding is not advertised"}
		return false, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform runtime capability image_understanding is not advertised", metadata
	}
	metadata["platform_capability"] = sanitizedRuntimeCapabilityMetadata(item)
	if !runtimeCapabilityIsReady(item) {
		message := runtimeCapabilityUnavailableMessage(item)
		metadata["platform_blocker"] = map[string]any{"code": "PLATFORM_CAPABILITY_UNAVAILABLE", "message": message, "status": item.Status, "unavailable_reason": item.UnavailableReason, "contract_status": item.ContractStatus}
		return false, "PLATFORM_CAPABILITY_UNAVAILABLE", message, metadata
	}
	return true, "", "", metadata
}

func (s *Service) prepareGenerationRuntimeVersion(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO, intentSpec *IntentSpecDTO) error {
	ready, code, message, metadata := s.generationRuntimeCapabilityReady()
	if !ready {
		version.RuntimeJobID = ""
		version.ImageJobID = ""
		version.Status = "contract_needed"
		version.Stage = "contract_needed"
		version.Progress = 0
		version.Blockers = appendGenerationBlocker(version.Blockers, code, message)
		version.Metadata = mergeObjectMaps(version.Metadata, metadata, map[string]any{"unavailable_reason": "contract-needed"})
		return nil
	}
	return s.createPlatformGenerationRuntimeJob(session, version, promptPlan, intentSpec)
}

func (s *Service) generationRuntimeCapabilityReady() (bool, string, string, map[string]any) {
	metadata := map[string]any{
		"platform_product_code": "ecommerce",
		"platform_task_type":    "image_generation",
	}
	if s.capabilityReader == nil || s.runtimeCreator == nil {
		return false, "CONTRACT_NEEDED", "Platform runtime orchestration client is not configured; no provider call was made", metadata
	}
	matrix, err := s.capabilityReader.ListRuntimeCapabilities("ecommerce", "image_generation")
	if err != nil {
		metadata["platform_blocker"] = map[string]any{"code": "PLATFORM_CAPABILITY_ERROR", "message": safePlatformCapabilityErrorMessage}
		return false, "PLATFORM_CAPABILITY_ERROR", safePlatformCapabilityErrorMessage, metadata
	}
	item, ok := runtimeCapabilityForTask(matrix, "image_generation")
	if !ok {
		metadata["platform_blocker"] = map[string]any{"code": "PLATFORM_CAPABILITY_UNAVAILABLE", "message": "Platform runtime capability image_generation is not advertised"}
		return false, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform runtime capability image_generation is not advertised", metadata
	}
	metadata["platform_capability"] = sanitizedRuntimeCapabilityMetadata(item)
	if !runtimeCapabilityIsReady(item) {
		message := runtimeCapabilityUnavailableMessage(item)
		metadata["platform_blocker"] = map[string]any{"code": "PLATFORM_CAPABILITY_UNAVAILABLE", "message": message, "status": item.Status, "unavailable_reason": item.UnavailableReason, "contract_status": item.ContractStatus}
		return false, "PLATFORM_CAPABILITY_UNAVAILABLE", message, metadata
	}
	return true, "", "", metadata
}

func (s *Service) createPlatformGenerationRuntimeJob(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO, intentSpec *IntentSpecDTO) error {
	if session == nil || version == nil || s.runtimeCreator == nil {
		return nil
	}
	manifest, manifestErr := s.platformRuntimeInputManifest(session, version, promptPlan, intentSpec)
	if manifestErr != nil {
		version.RuntimeJobID = ""
		version.ImageJobID = ""
		version.Status = "contract_needed"
		version.Stage = "contract_needed"
		version.Progress = 0
		version.Blockers = appendGenerationBlocker(version.Blockers, "PROMPT_RUNTIME_CONTRACT_INVALID", manifestErr.Error())
		version.Metadata = mergeObjectMaps(version.Metadata, map[string]any{
			"unavailable_reason": "contract-needed",
			"platform_blocker":   map[string]any{"code": "PROMPT_RUNTIME_CONTRACT_INVALID", "message": manifestErr.Error()},
		})
		return nil
	}
	idempotencyKey := generationRuntimeIdempotencyKey(session, version)
	chargeCtx, chargeErr := s.beginVisualRuntimeChargeSession(session, version.VersionID, "visual_generation", "ecommerce.image.generate", idempotencyKey)
	if chargeErr != nil {
		version.RuntimeJobID = ""
		version.ImageJobID = ""
		version.Status = "contract_needed"
		version.Stage = "contract_needed"
		version.Progress = 0
		version.Blockers = appendGenerationBlocker(version.Blockers, "PLATFORM_RUNTIME_CHARGE_GATE_FAILED", "Platform runtime charge gate failed; quota or charge-session setup is unavailable.")
		version.Metadata = mergeObjectMaps(version.Metadata, map[string]any{
			"unavailable_reason": "contract-needed",
			"platform_blocker":   map[string]any{"code": "PLATFORM_RUNTIME_CHARGE_GATE_FAILED", "message": "Platform runtime charge gate failed; quota or charge-session setup is unavailable."},
		})
		return nil
	}
	providerCode := generationProviderCode(version.Metadata)
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:     "ecommerce",
		TaskType:        "image_generation",
		ProviderCode:    providerCode,
		ProviderMode:    "async",
		OrganizationID:  session.OrganizationID,
		UserID:          session.UserID,
		SourceType:      "visual_generation",
		SourceID:        version.VersionID,
		IdempotencyKey:  idempotencyKey,
		ChargeSessionID: chargeContextID(chargeCtx),
		InputManifest:   mustJSON(manifest),
		RouteSnapshot:   mustJSON(generationRuntimeRouteSnapshot(version)),
		Metadata: mustJSON(map[string]any{
			"product_id": session.ProductID,
			"sku_code":   session.SKUCode,
			"session_id": session.ID,
			"version_id": version.VersionID,
			"prompt_id":  version.PromptID,
		}),
		Priority:       100,
		MaxAttempts:    3,
		TimeoutSeconds: 900,
	})
	if err != nil {
		if chargeCtx != nil {
			_ = billinggate.New(s.runtimeBillingGateway()).Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "visual_runtime_create_failed"})
		}
		version.RuntimeJobID = ""
		version.ImageJobID = ""
		version.Status = "contract_needed"
		version.Stage = "contract_needed"
		version.Progress = 0
		version.Blockers = appendGenerationBlocker(version.Blockers, "PLATFORM_RUNTIME_JOB_CREATE_FAILED", safePlatformRuntimeJobCreateErrorMessage)
		version.Metadata = mergeObjectMaps(version.Metadata, map[string]any{
			"unavailable_reason": "contract-needed",
			"platform_blocker": map[string]any{
				"code":    "PLATFORM_RUNTIME_JOB_CREATE_FAILED",
				"message": safePlatformRuntimeJobCreateErrorMessage,
			},
		})
		return nil
	}
	version.RuntimeJobID = runtimeJob.ID
	version.ImageJobID = ""
	version.Status = mapGenerationRuntimeStatus(runtimeJob.Status)
	version.Stage = mapGenerationRuntimeStage(runtimeJob.Stage, version.Status)
	if version.Progress < 5 && (version.Status == "queued" || version.Status == "processing") {
		version.Progress = 5
	}
	version.Blockers = removeGenerationExecutionBlockers(version.Blockers)
	version.Metadata = mergeObjectMaps(version.Metadata, map[string]any{
		"platform_source_type":  "visual_generation",
		"platform_source_id":    version.VersionID,
		"runtime_task_type":     "image_generation",
		"runtime_provider_code": runtimeJob.ProviderCode,
	})
	return nil
}

func (s *Service) runtimeBillingGateway() RuntimeBillingGateway {
	if s == nil || s.runtimeCreator == nil {
		return nil
	}
	gateway, _ := s.runtimeCreator.(RuntimeBillingGateway)
	return gateway
}

func (s *Service) beginVisualRuntimeChargeSession(session *models.EcommerceVisualWorkflowSession, sourceID, sourceType, billableItemCode, idempotencyKey string) (*billinggate.Context, error) {
	gateway := s.runtimeBillingGateway()
	if gateway == nil || session == nil {
		return nil, nil
	}
	return billinggate.New(gateway).Begin(billinggate.BeginInput{
		Action:           billinggate.ActionGeneration,
		SourceType:       sourceType,
		SourceID:         sourceID,
		ProductCode:      "ecommerce",
		OrganizationID:   session.OrganizationID,
		UserID:           session.UserID,
		BillableItemCode: billableItemCode,
		ResourceType:     billinggate.DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   idempotencyKey,
		RouteSnapshot:    map[string]any{"session_id": session.ID, "source_type": sourceType},
		Metadata:         map[string]any{"product_id": session.ProductID, "sku_code": session.SKUCode, "session_id": session.ID},
	})
}
