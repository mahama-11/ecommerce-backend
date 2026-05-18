package visualworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:     "ecommerce",
		TaskType:        "image_generation",
		ProviderMode:    "async",
		OrganizationID:  session.OrganizationID,
		UserID:          session.UserID,
		SourceType:      "visual_generation",
		SourceID:        version.VersionID,
		IdempotencyKey:  idempotencyKey,
		ChargeSessionID: chargeContextID(chargeCtx),
		InputManifest:   mustJSON(manifest),
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
		"platform_source_type": "visual_generation",
		"platform_source_id":   version.VersionID,
		"runtime_task_type":    "image_generation",
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

func chargeContextID(ctx *billinggate.Context) string {
	if ctx == nil {
		return ""
	}
	return ctx.ChargeSessionID
}

func generationRuntimeIdempotencyKey(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO) string {
	key := ""
	if version != nil {
		key = metadataString(version.Metadata, "idempotency_key")
	}
	if key == "" && version != nil {
		key = version.VersionID
	}
	if session == nil {
		return "ecommerce:visual_generation:" + key
	}
	return fmt.Sprintf("ecommerce:visual_generation:%s:%s:%s", session.OrganizationID, session.ID, key)
}

func (s *Service) platformRuntimeInputManifest(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO, intentSpec *IntentSpecDTO) (map[string]any, error) {
	manifest := map[string]any{
		"input_mode":         "prompt_snapshot",
		"requested_variants": requestedVariantsFromMetadata(versionMetadata(version)),
		"params_snapshot":    map[string]any{},
		"source_asset_ids":   []string{},
		"source_assets":      []map[string]any{},
		"ecommerce_snapshot": sanitizedGenerationManifest(session, version, promptPlan, intentSpec),
	}
	if version == nil || strings.TrimSpace(version.PromptID) == "" {
		return nil, fmt.Errorf("Prompt Center snapshot is required for visual generation runtime")
	}
	if s.promptRepo == nil {
		return s.platformRuntimeInputManifestFromPromptPlan(manifest, session, version, promptPlan)
	}
	promptRun, err := s.promptRepo.FindPromptRunByID(session.OrganizationID, version.PromptID)
	if err != nil {
		return s.platformRuntimeInputManifestFromPromptPlan(manifest, session, version, promptPlan)
	}
	if promptRun.ProductID != session.ProductID || promptRun.SKUCode != session.SKUCode {
		return nil, fmt.Errorf("Prompt Center snapshot does not match visual workflow product/SKU")
	}
	if promptRun.Status != "validated" && promptRun.Status != "bound" && promptRun.Status != "executed" {
		return nil, fmt.Errorf("Prompt Center snapshot is not executable")
	}
	var compiled promptcenter.CompiledPrompt
	if err := json.Unmarshal([]byte(promptRun.CompiledPromptJSON), &compiled); err != nil || strings.TrimSpace(compiled.FinalPrompt) == "" {
		return nil, fmt.Errorf("Prompt Center compiled prompt snapshot is invalid")
	}
	var bindings []promptcenter.SourceAssetBinding
	if strings.TrimSpace(promptRun.SourceAssetBindingsJSON) != "" {
		if err := json.Unmarshal([]byte(promptRun.SourceAssetBindingsJSON), &bindings); err != nil {
			return nil, fmt.Errorf("Prompt Center source asset bindings are invalid")
		}
	}
	sourceAssetIDs := make([]string, 0, len(bindings))
	sourceAssets := make([]map[string]any, 0, len(bindings))
	for _, binding := range bindings {
		assetID := strings.TrimSpace(binding.AssetID)
		if assetID == "" {
			continue
		}
		asset, err := s.assetRepo.FindAssetByID(session.OrganizationID, assetID)
		if err != nil || strings.TrimSpace(asset.StorageKey) == "" || !assetUsableForGeneration(asset.Width, asset.Height) {
			continue
		}
		sourceAssetIDs = append(sourceAssetIDs, asset.ID)
		sourceAssets = append(sourceAssets, map[string]any{
			"id":          asset.ID,
			"storage_key": asset.StorageKey,
			"mime_type":   asset.MimeType,
			"width":       asset.Width,
			"height":      asset.Height,
		})
	}
	if fanoutSourceID := metadataString(version.Metadata, "source_asset_id"); fanoutSourceID != "" {
		if ids, assets := s.runtimeSourceAssetsForIDs(session.OrganizationID, []string{fanoutSourceID}); len(ids) > 0 {
			sourceAssetIDs = ids
			sourceAssets = assets
		}
	}
	manifest["prompt_snapshot"] = map[string]any{
		"provider":        "",
		"model":           "",
		"system_prompt":   "",
		"style_prompt":    fanoutNegativePrompt(compiled.FinalNegativePrompt, versionMetadata(version)),
		"user_prompt":     fanoutSlotPrompt(compiled.FinalPrompt, versionMetadata(version)),
		"prompt_template": promptRun.TemplateCode,
	}
	paramsSnapshot := map[string]any{
		"prompt_id":           promptRun.ID,
		"template_id":         promptRun.TemplateID,
		"template_version_id": promptRun.TemplateVersionID,
		"schema_version":      promptRun.SchemaVersion,
		"content_hash":        promptRun.ContentHash,
		"source_map_hash":     promptRun.SourceMapHash,
	}
	mergeAllowedGenerationRuntimeParams(paramsSnapshot, version.Metadata)
	mergeFanoutGenerationRuntimeParams(paramsSnapshot, version.Metadata)
	manifest["params_snapshot"] = paramsSnapshot
	manifest["source_asset_ids"] = sourceAssetIDs
	manifest["source_assets"] = sourceAssets
	return sanitizeGenerationManifestValue(manifest).(map[string]any), nil
}

func (s *Service) platformRuntimeInputManifestFromPromptPlan(manifest map[string]any, session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO) (map[string]any, error) {
	if promptPlan == nil || strings.TrimSpace(promptPlan.Status) != "ready" || strings.TrimSpace(promptPlan.PromptID) == "" || strings.TrimSpace(version.PromptID) != strings.TrimSpace(promptPlan.PromptID) {
		return nil, fmt.Errorf("Prompt Center snapshot missing or not in organization")
	}
	userPrompt := promptPlanRuntimeText(session, version, promptPlan)
	if strings.TrimSpace(userPrompt) == "" {
		return nil, fmt.Errorf("Prompt plan snapshot is not executable")
	}
	sourceAssetIDs := make([]string, 0, len(promptPlan.SourceAssets))
	sourceAssets := make([]map[string]any, 0, len(promptPlan.SourceAssets))
	for _, binding := range promptPlan.SourceAssets {
		assetID := strings.TrimSpace(binding.AssetID)
		if assetID == "" {
			continue
		}
		if s.assetRepo == nil {
			continue
		}
		asset, err := s.assetRepo.FindAssetByID(session.OrganizationID, assetID)
		if err != nil || strings.TrimSpace(asset.StorageKey) == "" || !assetUsableForGeneration(asset.Width, asset.Height) {
			continue
		}
		sourceAssetIDs = append(sourceAssetIDs, asset.ID)
		sourceAssets = append(sourceAssets, map[string]any{
			"id":          asset.ID,
			"storage_key": asset.StorageKey,
			"mime_type":   asset.MimeType,
			"width":       asset.Width,
			"height":      asset.Height,
			"role":        binding.Role,
		})
	}
	if fanoutSourceID := metadataString(version.Metadata, "source_asset_id"); fanoutSourceID != "" {
		if ids, assets := s.runtimeSourceAssetsForIDs(session.OrganizationID, []string{fanoutSourceID}); len(ids) > 0 {
			sourceAssetIDs = ids
			sourceAssets = assets
		}
	}
	manifest["prompt_snapshot"] = map[string]any{
		"provider":        "",
		"model":           "",
		"system_prompt":   "",
		"style_prompt":    fanoutNegativePrompt(metadataString(promptPlan.Metadata, "negative_prompt"), versionMetadata(version)),
		"user_prompt":     fanoutSlotPrompt(userPrompt, versionMetadata(version)),
		"prompt_template": strings.TrimSpace(promptPlan.TemplateID),
	}
	paramsSnapshot := map[string]any{
		"prompt_id":           promptPlan.PromptID,
		"template_id":         promptPlan.TemplateID,
		"template_version_id": promptPlan.TemplateVersionID,
		"schema_version":      promptPlan.SchemaVersion,
		"snapshot_source":     "visual_workflow_prompt_plan",
	}
	mergeAllowedGenerationRuntimeParams(paramsSnapshot, version.Metadata)
	mergeFanoutGenerationRuntimeParams(paramsSnapshot, version.Metadata)
	manifest["params_snapshot"] = paramsSnapshot
	manifest["source_asset_ids"] = sourceAssetIDs
	manifest["source_assets"] = sourceAssets
	return sanitizeGenerationManifestValue(manifest).(map[string]any), nil
}

func (s *Service) runtimeSourceAssetsForIDs(orgID string, assetIDs []string) ([]string, []map[string]any) {
	if s == nil || s.assetRepo == nil {
		return nil, nil
	}
	ids := make([]string, 0, len(assetIDs))
	assets := make([]map[string]any, 0, len(assetIDs))
	for _, rawID := range compactUniqueStrings(assetIDs) {
		asset, err := s.assetRepo.FindAssetByID(orgID, rawID)
		if err != nil || strings.TrimSpace(asset.StorageKey) == "" || !assetUsableForGeneration(asset.Width, asset.Height) {
			continue
		}
		ids = append(ids, asset.ID)
		assets = append(assets, map[string]any{
			"id":          asset.ID,
			"storage_key": asset.StorageKey,
			"mime_type":   asset.MimeType,
			"width":       asset.Width,
			"height":      asset.Height,
		})
	}
	return ids, assets
}

func assetUsableForGeneration(width, height int) bool {
	return width >= 14 && height >= 14
}

func fanoutSlotPrompt(base string, metadata map[string]any) string {
	parts := []string{strings.TrimSpace(base)}
	if scene := metadataString(metadata, "scene_tag"); scene != "" {
		parts = append(parts, "Image type / scene: "+scene)
	}
	if detail := metadataString(metadata, "detail_requirement"); detail != "" {
		parts = append(parts, "Slot-specific detail requirements: "+detail)
	}
	return strings.TrimSpace(strings.Join(compactUniqueStrings(parts), "\n"))
}

func fanoutNegativePrompt(base string, metadata map[string]any) string {
	negative := metadataString(metadata, "negative_requirement")
	if negative == "" {
		return strings.TrimSpace(base)
	}
	if strings.TrimSpace(base) == "" {
		return negative
	}
	return strings.TrimSpace(base) + "\n" + negative
}

func mergeAllowedGenerationRuntimeParams(params map[string]any, metadata map[string]any) {
	if params == nil || metadata == nil {
		return
	}
	uiConfig, _ := metadata["ui_execution_config"].(map[string]any)
	if execConfig, ok := metadata["execution_config"].(map[string]any); ok {
		uiConfig = execConfig
	}
	providerConfig, _ := uiConfig["provider_config"].(map[string]any)
	resolutionID, _ := providerConfig["resolution_id"].(string)
	switch strings.TrimSpace(resolutionID) {
	case "1024-square":
		params["width"] = 1024
		params["height"] = 1024
		params["resolution_id"] = "1024-square"
	case "720-wide":
		params["width"] = 1280
		params["height"] = 720
		params["resolution_id"] = "720-wide"
	}
	for _, key := range []string{"scene_tag", "detail_requirement", "negative_requirement"} {
		if text := metadataString(metadata, key); text != "" {
			params[key] = text
		}
	}
}

func promptPlanRuntimeText(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO) string {
	if promptPlan == nil {
		return ""
	}
	for _, key := range []string{"final_prompt", "user_prompt", "positive_prompt", "generation_prompt", "prompt", "creative_brief"} {
		if text := metadataString(promptPlan.Metadata, key); strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if promptPlan.Variables != nil {
			if text, ok := promptPlan.Variables[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	payload := map[string]any{
		"task":        "Generate ecommerce SKU visual assets from the current backend-approved prompt plan.",
		"prompt_id":   promptPlan.PromptID,
		"scene_type":  promptPlan.SceneType,
		"template_id": promptPlan.TemplateID,
		"variables":   promptPlan.Variables,
	}
	if session != nil {
		payload["product_id"] = session.ProductID
		payload["sku_code"] = session.SKUCode
	}
	if version != nil && strings.TrimSpace(version.RefinementInstruction) != "" {
		payload["refinement_instruction"] = version.RefinementInstruction
	}
	return mustJSON(payload)
}

func sanitizedGenerationManifest(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO, intentSpec *IntentSpecDTO) map[string]any {
	manifest := map[string]any{
		"contract":     "ecommerce.visual_generation.v1",
		"product_code": "ecommerce",
		"task_type":    "image_generation",
	}
	if session != nil {
		manifest["session_id"] = session.ID
		manifest["product_id"] = session.ProductID
		manifest["sku_code"] = session.SKUCode
	}
	if version != nil {
		manifest["version_id"] = version.VersionID
		manifest["prompt_id"] = version.PromptID
		manifest["parent_version_id"] = version.ParentVersionID
		manifest["source_version_id"] = version.SourceVersionID
		manifest["refinement_instruction"] = version.RefinementInstruction
		manifest["mask_asset_id"] = version.MaskAssetID
	}
	if promptPlan != nil {
		manifest["prompt_plan"] = map[string]any{"prompt_id": promptPlan.PromptID, "status": promptPlan.Status, "scene_type": promptPlan.SceneType, "variables": promptPlan.Variables, "source_assets": promptPlan.SourceAssets}
	}
	if intentSpec != nil {
		manifest["intent_spec"] = intentSnapshotMetadata(*intentSpec)
	}
	return sanitizeGenerationManifestValue(manifest).(map[string]any)
}

func sanitizeGenerationManifestValue(raw any) any {
	switch value := raw.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if isForbiddenExecutionArtifactKey(key) || isForbiddenWritebackMetadataKey(key) || isForbiddenDeconstructionMetadataKey(key) {
				continue
			}
			out[key] = sanitizeGenerationManifestValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, child := range value {
			out = append(out, sanitizeGenerationManifestValue(child))
		}
		return out
	default:
		return raw
	}
}

func appendGenerationBlocker(blockers []ReadinessBlocker, code, message string) []ReadinessBlocker {
	if strings.TrimSpace(code) == "" {
		code = "CONTRACT_NEEDED"
	}
	for _, blocker := range blockers {
		if blocker.Code == code && blocker.Target == "generation" {
			return blockers
		}
	}
	return append(blockers, ReadinessBlocker{Code: code, Target: "generation", Message: message})
}

func removeGenerationExecutionBlockers(blockers []ReadinessBlocker) []ReadinessBlocker {
	out := make([]ReadinessBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		if blocker.Target == "generation" && (blocker.Code == "CONTRACT_NEEDED" || strings.HasPrefix(blocker.Code, "PLATFORM_")) {
			continue
		}
		out = append(out, blocker)
	}
	return out
}

func mapGenerationRuntimeStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "queued":
		return "queued"
	case "processing", "running":
		return "processing"
	case "completed", "succeeded":
		return "completed"
	case "failed":
		return "failed"
	case "canceled":
		return "canceled"
	default:
		return "processing"
	}
}

func normalizeGenerationRuntimeCallbackStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "queued":
		return "queued"
	case "processing", "running":
		return "processing"
	case "completed", "succeeded":
		return "completed"
	case "failed":
		return "failed"
	case "canceled", "cancelled":
		return "canceled"
	default:
		return ""
	}
}

func mapGenerationRuntimeStage(stage, status string) string {
	trimmed := strings.TrimSpace(stage)
	if validGenerationVersionStage(trimmed) {
		return trimmed
	}
	switch status {
	case "queued":
		return "queued"
	case "processing":
		return "running"
	case "completed":
		return "result_available"
	case "failed":
		return "failed"
	case "canceled":
		return "canceled"
	default:
		return "queued"
	}
}

func (s *Service) CreateIntentPlannerJob(orgID, sessionID string, req CreateIntentPlannerJobRequest) (*IntentPlannerJobResponse, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	if s.capabilityReader == nil || s.runtimeCreator == nil {
		return s.persistIntentPlannerBlocked(session, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform text runtime is not configured")
	}
	matrix, err := s.capabilityReader.ListRuntimeCapabilities("ecommerce", "intent_planning")
	if err != nil {
		return s.persistIntentPlannerBlocked(session, "PLATFORM_CAPABILITY_CHECK_FAILED", safePlatformCapabilityErrorMessage)
	}
	capability, ok := runtimeCapabilityForTask(matrix, "intent_planning")
	if !ok || !runtimeCapabilityIsReady(capability) {
		return s.persistIntentPlannerBlocked(session, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform intent_planning runtime is not ready")
	}
	elements, err := s.repo.ListDeconstructionElements(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	selected := selectPlannerElements(elements, req.ElementIDs)
	if len(selected) == 0 {
		return s.persistIntentPlannerBlocked(session, "DECONSTRUCTION_SELECTION_REQUIRED", "Select or confirm deconstruction elements before intent planning")
	}
	manifest := map[string]any{
		"input_mode": "ecommerce_visual_intent_planning",
		"prompt_snapshot": map[string]any{
			"provider":    "minimax_text",
			"user_prompt": buildIntentPlannerPrompt(session, selected, req),
		},
		"params_snapshot": map[string]any{
			"response_format": "json",
			"marketplace":     strings.TrimSpace(req.Marketplace),
			"locale":          strings.TrimSpace(req.Locale),
			"drift_controls":  sanitizeGenerationManifestValue(req.DriftControls),
		},
		"ecommerce_snapshot": map[string]any{
			"contract":            "ecommerce.visual_intent_planner.v1",
			"session_id":          session.ID,
			"product_id":          session.ProductID,
			"sku_code":            session.SKUCode,
			"source_reference_id": strings.TrimSpace(req.SourceReferenceID),
			"elements":            intentPlannerElementSnapshots(selected),
		},
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("intent-planner:%s:%x", session.ID, sha256.Sum256([]byte(mustJSON(manifest))))
	}
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:    "ecommerce",
		TaskType:       "intent_planning",
		ProviderMode:   "async",
		OrganizationID: session.OrganizationID,
		UserID:         session.UserID,
		SourceType:     "visual_intent_planning",
		SourceID:       session.ID,
		IdempotencyKey: "ecommerce:visual_intent_planning:" + idempotencyKey,
		InputManifest:  mustJSON(sanitizeGenerationManifestValue(manifest)),
		Metadata:       mustJSON(map[string]any{"product_id": session.ProductID, "sku_code": session.SKUCode, "session_id": session.ID}),
		Priority:       80,
		MaxAttempts:    2,
		TimeoutSeconds: 300,
	})
	if err != nil {
		return s.persistIntentPlannerBlocked(session, "PLATFORM_RUNTIME_CREATE_FAILED", safePlatformRuntimeJobCreateErrorMessage)
	}
	metadata := decodeObject(session.Metadata)
	metadata["intent_planner"] = map[string]any{"runtime_job_id": runtimeJob.ID, "status": runtimeJob.Status, "stage": runtimeJob.Stage, "progress": 5, "idempotency_key": idempotencyKey}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	session.Status = models.VisualWorkflowStatusProcessing
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &IntentPlannerJobResponse{SessionID: session.ID, RuntimeJobID: runtimeJob.ID, Status: runtimeJob.Status, Stage: runtimeJob.Stage, Progress: 5, IdempotencyKey: idempotencyKey}, nil
}

func (s *Service) persistIntentPlannerBlocked(session *models.EcommerceVisualWorkflowSession, code, message string) (*IntentPlannerJobResponse, error) {
	metadata := decodeObject(session.Metadata)
	metadata["intent_planner"] = map[string]any{"status": "contract_needed", "blocker_code": code, "message": message}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	session.Status = models.VisualWorkflowStatusBlocked
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &IntentPlannerJobResponse{SessionID: session.ID, Status: "contract_needed", Stage: "contract_needed", Blockers: []ReadinessBlocker{{Code: code, Target: "intent_planner", Message: message}}}, nil
}

func (s *Service) InternalUpdateIntentPlannerRuntime(sessionID string, req InternalRuntimeUpdateRequest) (*IntentPlannerJobResponse, error) {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	status := normalizeGenerationRuntimeCallbackStatus(req.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: unsupported intent planner runtime status %q", ErrInternalCallbackInvalid, req.Status)
	}
	progress := 0
	if req.Progress != nil {
		progress = *req.Progress
	}
	metadata := decodeObject(session.Metadata)
	metadata["intent_planner"] = map[string]any{"runtime_job_id": req.RuntimeJobID, "status": status, "stage": req.Stage, "progress": progress, "error_code": req.ErrorCode, "error_message": req.ErrorMessage}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	if status == "failed" || status == "canceled" {
		session.Status = status
	} else {
		session.Status = models.VisualWorkflowStatusProcessing
	}
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &IntentPlannerJobResponse{SessionID: session.ID, RuntimeJobID: req.RuntimeJobID, Status: status, Stage: req.Stage, Progress: progress}, nil
}

func (s *Service) InternalRecordIntentPlannerResults(sessionID string, req InternalRecordResultsRequest) (*SessionDTO, error) {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	status := normalizeGenerationRuntimeCallbackStatus(req.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: unsupported intent planner result status %q", ErrInternalCallbackInvalid, req.Status)
	}
	if status != "completed" {
		_, err := s.InternalUpdateIntentPlannerRuntime(sessionID, InternalRuntimeUpdateRequest{Status: status, Stage: req.Stage, StageMessage: req.StageMessage, Progress: &req.Progress, ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage})
		if err != nil {
			return nil, err
		}
		dto := sessionDTO(session)
		return dto, nil
	}
	content := firstPlannerVariantText(req.Variants)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("%w: intent planner result text is required", ErrInternalCallbackInvalid)
	}
	var spec IntentSpecDTO
	if err := json.Unmarshal([]byte(content), &spec); err != nil {
		return nil, fmt.Errorf("%w: intent planner result must be valid intent JSON", ErrInternalCallbackInvalid)
	}
	applyIntentSpecDefaults(&spec, session)
	if err := validateIntentSpec(&spec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInternalCallbackInvalid, err)
	}
	session.IntentSpecJSON = encodeIntentSpec(&spec)
	session.CurrentStage = models.VisualWorkflowStagePrompt
	session.Status = models.VisualWorkflowStatusReady
	metadata := decodeObject(session.Metadata)
	metadata["intent_planner"] = map[string]any{"status": "completed", "stage": req.Stage, "progress": req.Progress}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return sessionDTO(session), nil
}

func (s *Service) HasIntentPlannerSession(sessionID string) bool {
	_, err := s.repo.FindSessionByID(sessionID)
	return err == nil
}

func (s *Service) CreatePromptPlannerJob(orgID, sessionID string, req CreatePromptPlannerJobRequest) (*PromptPlannerJobResponse, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	if s.capabilityReader == nil || s.runtimeCreator == nil {
		return s.persistPromptPlannerBlocked(session, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform prompt planning runtime is not configured")
	}
	intentSpec := decodeIntentSpec(session.IntentSpecJSON, session)
	if strings.TrimSpace(intentSpec.SchemaVersion) == "" {
		return s.persistPromptPlannerBlocked(session, "INTENT_SPEC_REQUIRED", "Intent spec is required before prompt planning")
	}
	matrix, err := s.capabilityReader.ListRuntimeCapabilities("ecommerce", "prompt_planning")
	if err != nil {
		return s.persistPromptPlannerBlocked(session, "PLATFORM_CAPABILITY_CHECK_FAILED", safePlatformCapabilityErrorMessage)
	}
	capability, ok := runtimeCapabilityForTask(matrix, "prompt_planning")
	if !ok || !runtimeCapabilityIsReady(capability) {
		return s.persistPromptPlannerBlocked(session, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform prompt_planning runtime is not ready")
	}
	existingPlan := decodePromptPlan(session.PromptPlanJSON, session)
	manifest := map[string]any{
		"input_mode": "ecommerce_visual_prompt_planning",
		"prompt_snapshot": map[string]any{
			"provider":    "minimax_text",
			"user_prompt": buildPromptPlannerPrompt(session, &intentSpec, &existingPlan, req),
		},
		"params_snapshot": map[string]any{"response_format": "json", "temperature": 0.2},
		"output_contract": map[string]any{"schema": "visual_prompt_plan.v1", "required_fields": []string{"schema_version", "status", "prompt_id", "scene_type", "variables", "metadata.prompt_diff"}},
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("prompt-planner:%s:%x", session.ID, sha256.Sum256([]byte(mustJSON(manifest))))
	}
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:    "ecommerce",
		TaskType:       "prompt_planning",
		ProviderMode:   "async",
		OrganizationID: session.OrganizationID,
		UserID:         session.UserID,
		SourceType:     "visual_prompt_planning",
		SourceID:       session.ID,
		IdempotencyKey: "ecommerce:visual_prompt_planning:" + idempotencyKey,
		InputManifest:  mustJSON(sanitizeGenerationManifestValue(manifest)),
		Metadata:       mustJSON(map[string]any{"product_id": session.ProductID, "sku_code": session.SKUCode, "session_id": session.ID}),
		Priority:       90,
		MaxAttempts:    2,
		TimeoutSeconds: 300,
	})
	if err != nil {
		return s.persistPromptPlannerBlocked(session, "PLATFORM_RUNTIME_CREATE_FAILED", safePlatformRuntimeJobCreateErrorMessage)
	}
	metadata := decodeObject(session.Metadata)
	metadata["prompt_planner"] = map[string]any{"runtime_job_id": runtimeJob.ID, "status": runtimeJob.Status, "stage": runtimeJob.Stage, "progress": 5, "idempotency_key": idempotencyKey}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	session.Status = models.VisualWorkflowStatusProcessing
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &PromptPlannerJobResponse{SessionID: session.ID, RuntimeJobID: runtimeJob.ID, Status: runtimeJob.Status, Stage: runtimeJob.Stage, Progress: 5, IdempotencyKey: idempotencyKey}, nil
}

func (s *Service) persistPromptPlannerBlocked(session *models.EcommerceVisualWorkflowSession, code, message string) (*PromptPlannerJobResponse, error) {
	metadata := decodeObject(session.Metadata)
	metadata["prompt_planner"] = map[string]any{"status": "contract_needed", "blocker_code": code, "message": message}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	session.Status = models.VisualWorkflowStatusBlocked
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &PromptPlannerJobResponse{SessionID: session.ID, Status: "contract_needed", Stage: "contract_needed", Blockers: []ReadinessBlocker{{Code: code, Target: "prompt_planner", Message: message}}}, nil
}

func (s *Service) resolvePromptPlannerSession(callbackID string) (*models.EcommerceVisualWorkflowSession, error) {
	callbackID = strings.TrimSpace(callbackID)
	if callbackID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	if session, err := s.repo.FindSessionByID(callbackID); err == nil {
		return session, nil
	}
	session, err := s.repo.FindSessionByMetadataLike(callbackID)
	if err != nil {
		return nil, err
	}
	promptPlanner, _ := decodeObject(session.Metadata)["prompt_planner"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(promptPlanner["runtime_job_id"])) != callbackID {
		return nil, gorm.ErrRecordNotFound
	}
	return session, nil
}

func (s *Service) InternalUpdatePromptPlannerRuntime(sessionID string, req InternalRuntimeUpdateRequest) (*PromptPlannerJobResponse, error) {
	session, err := s.resolvePromptPlannerSession(sessionID)
	if err != nil {
		return nil, err
	}
	status := normalizeGenerationRuntimeCallbackStatus(req.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: unsupported prompt planner runtime status %q", ErrInternalCallbackInvalid, req.Status)
	}
	progress := 0
	if req.Progress != nil {
		progress = *req.Progress
	}
	metadata := decodeObject(session.Metadata)
	metadata["prompt_planner"] = map[string]any{"runtime_job_id": req.RuntimeJobID, "status": status, "stage": req.Stage, "progress": progress, "error_code": req.ErrorCode, "error_message": req.ErrorMessage}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	if status == "failed" || status == "canceled" {
		session.Status = status
	} else {
		session.Status = models.VisualWorkflowStatusProcessing
	}
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &PromptPlannerJobResponse{SessionID: session.ID, RuntimeJobID: req.RuntimeJobID, Status: status, Stage: req.Stage, Progress: progress}, nil
}

func (s *Service) InternalRecordPromptPlannerResults(sessionID string, req InternalRecordResultsRequest) (*SessionDTO, error) {
	session, err := s.resolvePromptPlannerSession(sessionID)
	if err != nil {
		return nil, err
	}
	status := normalizeGenerationRuntimeCallbackStatus(req.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: unsupported prompt planner result status %q", ErrInternalCallbackInvalid, req.Status)
	}
	if status != "completed" {
		_, err := s.InternalUpdatePromptPlannerRuntime(sessionID, InternalRuntimeUpdateRequest{Status: status, Stage: req.Stage, StageMessage: req.StageMessage, Progress: &req.Progress, ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage})
		if err != nil {
			return nil, err
		}
		dto := sessionDTO(session)
		return dto, nil
	}
	content := firstPlannerVariantText(req.Variants)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("%w: prompt planner result text is required", ErrInternalCallbackInvalid)
	}
	existingPlan := decodePromptPlan(session.PromptPlanJSON, session)
	content = normalizePromptPlannerJSON(content)
	var planAlias promptPlanAlias
	if err := json.Unmarshal([]byte(content), &planAlias); err != nil {
		return nil, fmt.Errorf("%w: prompt planner result must be valid prompt plan JSON", ErrInternalCallbackInvalid)
	}
	plan := PromptPlanDTO(planAlias)
	applyPromptPlanDefaults(&plan, session)
	if plan.Metadata == nil {
		plan.Metadata = map[string]any{}
	}
	plan.Metadata["source"] = "llm_prompt_planner"
	plan.Metadata["requires_prompt_diff"] = true
	if promptDiffNeedsFallback(plan.Metadata["prompt_diff"]) {
		plan.Metadata["prompt_diff"] = buildPromptPlanDiff(&existingPlan, &plan)
	}
	if err := validatePromptPlan(&plan); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInternalCallbackInvalid, err)
	}
	if err := s.attachPromptCenterSnapshot(session, &plan); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInternalCallbackInvalid, err)
	}
	session.PromptPlanJSON = encodePromptPlan(&plan)
	session.CurrentStage = models.VisualWorkflowStageGeneration
	session.Status = models.VisualWorkflowStatusReady
	metadata := decodeObject(session.Metadata)
	metadata["prompt_planner"] = map[string]any{"status": "completed", "stage": req.Stage, "progress": req.Progress, "prompt_id": plan.PromptID}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return sessionDTO(session), nil
}

func (s *Service) HasPromptPlannerSession(sessionID string) bool {
	_, err := s.repo.FindSessionByID(sessionID)
	return err == nil
}

func (s *Service) attachPromptCenterSnapshot(session *models.EcommerceVisualWorkflowSession, plan *PromptPlanDTO) error {
	if session == nil || plan == nil {
		return fmt.Errorf("visual workflow session and prompt plan are required")
	}
	if s.promptCreator == nil {
		return fmt.Errorf("Prompt Center preview service is not configured; no executable prompt snapshot was created")
	}
	templateID := strings.TrimSpace(plan.TemplateID)
	if templateID == "" {
		templateID = strings.TrimSpace(session.TemplateID)
	}
	if templateID == "" {
		return fmt.Errorf("Prompt Center template_id is required before visual generation runtime")
	}
	sceneType := strings.TrimSpace(plan.SceneType)
	if sceneType == "" {
		sceneType = metadataString(plan.Metadata, "scene_type")
	}
	if sceneType == "" {
		sceneType = strings.TrimSpace(session.ToolSlug)
	}
	if sceneType == "" {
		sceneType = "product_visual_generation"
	}
	variables := mergeObjectMaps(plan.Variables, map[string]any{
		"visual_workflow_session_id": session.ID,
		"product_id":                 session.ProductID,
		"sku_code":                   session.SKUCode,
	})
	if diff, ok := plan.Metadata["prompt_diff"]; ok {
		variables["prompt_diff"] = diff
	}
	sourceAssets := s.promptCenterSourceAssetBindings(session, plan)
	idempotencyKey := fmt.Sprintf("visual-prompt-snapshot:%s:%x", session.ID, sha256.Sum256([]byte(mustJSON(map[string]any{
		"template_id":          templateID,
		"template_version_id":  strings.TrimSpace(plan.TemplateVersionID),
		"scene_type":           sceneType,
		"variables":            variables,
		"source_assets":        sourceAssets,
		"prompt_plan_metadata": sanitizeGenerationManifestValue(plan.Metadata),
	}))))
	resp, err := s.promptCreator.Preview(session.UserID, session.OrganizationID, promptcenter.PreviewPromptInput{
		ProductID:         session.ProductID,
		SKUCode:           session.SKUCode,
		TemplateID:        templateID,
		TemplateVersionID: strings.TrimSpace(plan.TemplateVersionID),
		SceneType:         sceneType,
		Variables:         variables,
		SourceAssets:      sourceAssets,
		IdempotencyKey:    idempotencyKey,
		Metadata: map[string]interface{}{
			"source":                     "visual_workflow_prompt_planner",
			"visual_workflow_session_id": session.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("Prompt Center preview failed: %w", err)
	}
	if resp == nil || strings.TrimSpace(resp.PromptID) == "" {
		return fmt.Errorf("Prompt Center preview did not return prompt_id")
	}
	if resp.Status != "validated" && resp.Status != "bound" && resp.Status != "executed" {
		return fmt.Errorf("Prompt Center snapshot is not executable: %s", resp.Status)
	}
	plan.PromptID = resp.PromptID
	plan.Status = "ready"
	plan.TemplateID = resp.TemplateID
	plan.TemplateVersionID = resp.TemplateVersionID
	plan.SceneType = resp.SceneType
	plan.Blockers = removePromptPlanExecutionBlockers(plan.Blockers)
	plan.Metadata = mergeObjectMaps(plan.Metadata, map[string]any{
		"prompt_center_snapshot_id": resp.PromptID,
		"prompt_center_status":      resp.Status,
		"prompt_center_source":      "preview",
	})
	return nil
}

func (s *Service) promptCenterSourceAssetBindings(session *models.EcommerceVisualWorkflowSession, plan *PromptPlanDTO) []promptcenter.SourceAssetBinding {
	seen := map[string]bool{}
	out := []promptcenter.SourceAssetBinding{}
	add := func(slot, assetID string) {
		assetID = strings.TrimSpace(assetID)
		if assetID == "" || seen[assetID] {
			return
		}
		seen[assetID] = true
		if strings.TrimSpace(slot) == "" {
			slot = fmt.Sprintf("source_%d", len(out)+1)
		}
		out = append(out, promptcenter.SourceAssetBinding{Slot: strings.TrimSpace(slot), AssetID: assetID})
	}
	if plan != nil {
		for _, asset := range plan.SourceAssets {
			add(asset.Role, asset.AssetID)
		}
	}
	if len(out) > 0 || s.repo == nil || session == nil {
		return out
	}
	sources, err := s.repo.ListSourceReferences(session.OrganizationID, session.ID)
	if err != nil {
		return out
	}
	for _, src := range sources {
		if src.Status != models.VisualSourceStatusReady || src.ResolveStatus != models.VisualSourceStatusReady {
			continue
		}
		metadata := decodeObject(src.Metadata)
		add(sourceRoleFromMetadata(metadata, src.SourceKind), src.AssetID)
	}
	return out
}

func removePromptPlanExecutionBlockers(blockers []ReadinessBlocker) []ReadinessBlocker {
	out := make([]ReadinessBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		code := strings.ToUpper(strings.TrimSpace(blocker.Code))
		if code == "CONTRACT_NEEDED" || code == "PROMPT_RUNTIME_CONTRACT_INVALID" {
			continue
		}
		out = append(out, blocker)
	}
	return out
}

func (s *Service) CreateStrategyReportJob(orgID, sessionID string, req CreateStrategyReportJobRequest) (*StrategyReportJobResponse, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	if s.capabilityReader == nil || s.runtimeCreator == nil {
		return s.persistStrategyReportBlocked(session, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform strategy report runtime is not configured")
	}
	matrix, err := s.capabilityReader.ListRuntimeCapabilities("ecommerce", "strategy_report")
	if err != nil {
		return s.persistStrategyReportBlocked(session, "PLATFORM_CAPABILITY_CHECK_FAILED", safePlatformCapabilityErrorMessage)
	}
	capability, ok := runtimeCapabilityForTask(matrix, "strategy_report")
	if !ok || !runtimeCapabilityIsReady(capability) {
		return s.persistStrategyReportBlocked(session, "PLATFORM_CAPABILITY_UNAVAILABLE", "Platform strategy_report runtime is not ready")
	}
	manifest := map[string]any{"input_mode": "ecommerce_strategy_report", "prompt_snapshot": map[string]any{"provider": "minimax_text", "user_prompt": buildStrategyReportPrompt(session, req)}, "params_snapshot": map[string]any{"response_format": "json", "temperature": 0.2}, "output_contract": map[string]any{"schema": "ecommerce_strategy_report.v1", "required_fields": []string{"schema_version", "status", "summary", "recommendations"}}}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("strategy-report:%s:%x", session.ID, sha256.Sum256([]byte(mustJSON(manifest))))
	}
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{ProductCode: "ecommerce", TaskType: "strategy_report", ProviderMode: "async", OrganizationID: session.OrganizationID, UserID: session.UserID, SourceType: "visual_strategy_report", SourceID: session.ID, IdempotencyKey: "ecommerce:visual_strategy_report:" + idempotencyKey, InputManifest: mustJSON(sanitizeGenerationManifestValue(manifest)), Metadata: mustJSON(map[string]any{"product_id": session.ProductID, "sku_code": session.SKUCode, "session_id": session.ID}), Priority: 70, MaxAttempts: 2, TimeoutSeconds: 300})
	if err != nil {
		return s.persistStrategyReportBlocked(session, "PLATFORM_RUNTIME_CREATE_FAILED", safePlatformRuntimeJobCreateErrorMessage)
	}
	metadata := decodeObject(session.Metadata)
	metadata["strategy_report"] = map[string]any{"runtime_job_id": runtimeJob.ID, "status": runtimeJob.Status, "stage": runtimeJob.Stage, "progress": 5, "idempotency_key": idempotencyKey}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &StrategyReportJobResponse{SessionID: session.ID, RuntimeJobID: runtimeJob.ID, Status: runtimeJob.Status, Stage: runtimeJob.Stage, Progress: 5, IdempotencyKey: idempotencyKey}, nil
}

func (s *Service) persistStrategyReportBlocked(session *models.EcommerceVisualWorkflowSession, code, message string) (*StrategyReportJobResponse, error) {
	metadata := decodeObject(session.Metadata)
	metadata["strategy_report"] = map[string]any{"status": "contract_needed", "blocker_code": code, "message": message}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &StrategyReportJobResponse{SessionID: session.ID, Status: "contract_needed", Stage: "contract_needed", Blockers: []ReadinessBlocker{{Code: code, Target: "strategy_report", Message: message}}}, nil
}

func (s *Service) InternalUpdateStrategyReportRuntime(sessionID string, req InternalRuntimeUpdateRequest) (*StrategyReportJobResponse, error) {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	status := normalizeGenerationRuntimeCallbackStatus(req.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: unsupported strategy report runtime status %q", ErrInternalCallbackInvalid, req.Status)
	}
	progress := 0
	if req.Progress != nil {
		progress = *req.Progress
	}
	metadata := decodeObject(session.Metadata)
	metadata["strategy_report"] = map[string]any{"runtime_job_id": req.RuntimeJobID, "status": status, "stage": req.Stage, "progress": progress, "error_code": req.ErrorCode, "error_message": req.ErrorMessage}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &StrategyReportJobResponse{SessionID: session.ID, RuntimeJobID: req.RuntimeJobID, Status: status, Stage: req.Stage, Progress: progress}, nil
}

func (s *Service) InternalRecordStrategyReportResults(sessionID string, req InternalRecordResultsRequest) (*SessionDTO, error) {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	status := normalizeGenerationRuntimeCallbackStatus(req.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: unsupported strategy report result status %q", ErrInternalCallbackInvalid, req.Status)
	}
	metadata := decodeObject(session.Metadata)
	if status != "completed" {
		metadata["strategy_report"] = map[string]any{"status": status, "stage": req.Stage, "progress": req.Progress, "error_code": req.ErrorCode, "error_message": req.ErrorMessage}
		session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
		if err := s.repo.SaveSession(session); err != nil {
			return nil, err
		}
		dto := sessionDTO(session)
		return dto, nil
	}
	content := firstPlannerVariantText(req.Variants)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("%w: strategy report result text is required", ErrInternalCallbackInvalid)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return nil, fmt.Errorf("%w: strategy report result must be valid JSON", ErrInternalCallbackInvalid)
	}
	if strings.TrimSpace(fmt.Sprint(report["schema_version"])) == "" || strings.TrimSpace(fmt.Sprint(report["summary"])) == "" {
		return nil, fmt.Errorf("%w: strategy report schema_version and summary are required", ErrInternalCallbackInvalid)
	}
	metadata["strategy_report"] = map[string]any{"status": "completed", "stage": req.Stage, "progress": req.Progress, "report": sanitizeGenerationManifestValue(report)}
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return sessionDTO(session), nil
}

func (s *Service) HasStrategyReportSession(sessionID string) bool {
	_, err := s.repo.FindSessionByID(sessionID)
	return err == nil
}

func buildStrategyReportPrompt(session *models.EcommerceVisualWorkflowSession, req CreateStrategyReportJobRequest) string {
	payload := map[string]any{"instruction": "Return strict JSON ecommerce_strategy_report.v1. Use only supplied SKU/workflow/source facts. Provide summary, opportunities, risks, and recommendations. Do not include credentials or execution artifacts.", "product_id": session.ProductID, "sku_code": session.SKUCode, "marketplace": req.Marketplace, "locale": req.Locale, "report_goal": req.ReportGoal, "source_facts": req.SourceFacts, "intent_spec": decodeIntentSpec(session.IntentSpecJSON, session), "prompt_plan": decodePromptPlan(session.PromptPlanJSON, session)}
	return mustJSON(payload)
}

func buildPromptPlannerPrompt(session *models.EcommerceVisualWorkflowSession, intentSpec *IntentSpecDTO, existingPlan *PromptPlanDTO, req CreatePromptPlannerJobRequest) string {
	payload := map[string]any{"instruction": "Return strict JSON visual_prompt_plan.v1 for provider-executable ecommerce visual generation. Include prompt_id/status/scene_type/variables/source_assets when known. Also include metadata.prompt_diff with added, removed, and changed arrays comparing the existing_prompt_plan against the new plan. Do not include execution artifact or credential fields.", "product_id": session.ProductID, "sku_code": session.SKUCode, "marketplace": req.Marketplace, "locale": req.Locale, "drift_controls": req.DriftControls, "prompt_variables": req.PromptVariables, "prompt_id": req.PromptID, "template_id": req.TemplateID, "intent_spec": intentSpec, "existing_prompt_plan": existingPlan}
	return mustJSON(payload)
}

func selectPlannerElements(elements []models.EcommerceVisualDeconstructionElement, ids []string) []models.EcommerceVisualDeconstructionElement {
	want := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			want[strings.TrimSpace(id)] = true
		}
	}
	out := make([]models.EcommerceVisualDeconstructionElement, 0, len(elements))
	for _, item := range elements {
		if len(want) > 0 && !want[item.ID] {
			continue
		}
		if len(want) == 0 && !item.Selected && !item.Confirmed {
			continue
		}
		out = append(out, item)
	}
	return out
}

func intentPlannerElementSnapshots(elements []models.EcommerceVisualDeconstructionElement) []map[string]any {
	out := make([]map[string]any, 0, len(elements))
	for _, item := range elements {
		value := decodeObject(item.ValueJSON)
		metadata := decodeObject(item.Metadata)
		out = append(out, map[string]any{"element_id": item.ID, "element_type": item.ElementType, "element_key": item.ElementKey, "label": item.Label, "value": sanitizeGenerationManifestValue(value), "decision": metadataString(metadata, "decision"), "group_path": metadata["group_path"], "target_asset_id": metadataString(metadata, "target_asset_id")})
	}
	return out
}

func buildIntentPlannerPrompt(session *models.EcommerceVisualWorkflowSession, elements []models.EcommerceVisualDeconstructionElement, req CreateIntentPlannerJobRequest) string {
	payload := map[string]any{"instruction": "Return a strict JSON visual_intent_spec.v1 for ecommerce SKU visual generation. Use keep/replace/drop decisions and drift controls. Do not include provider, storage, billing, runtime, or credential fields.", "product_id": session.ProductID, "sku_code": session.SKUCode, "marketplace": req.Marketplace, "locale": req.Locale, "drift_controls": req.DriftControls, "elements": intentPlannerElementSnapshots(elements)}
	return mustJSON(payload)
}

func firstPlannerVariantText(variants []map[string]any) string {
	for _, variant := range variants {
		for _, key := range []string{"inline_data", "text", "content", "body"} {
			if value, ok := variant[key].(string); ok && strings.TrimSpace(value) != "" {
				return stripJSONMarkdownFence(value)
			}
		}
		if asset, ok := variant["asset"].(map[string]any); ok {
			for _, key := range []string{"inline_data", "text", "content", "body"} {
				if value, ok := asset[key].(string); ok && strings.TrimSpace(value) != "" {
					return stripJSONMarkdownFence(value)
				}
			}
		}
	}
	return ""
}

func stripJSONMarkdownFence(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "json") {
		trimmed = strings.TrimSpace(trimmed[len("json"):])
	}
	if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	return trimmed
}

func normalizePromptPlannerJSON(value string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return value
	}
	if sourceAssets, ok := raw["source_assets"].(map[string]any); ok {
		assets := make([]map[string]any, 0, len(sourceAssets))
		for role, source := range sourceAssets {
			asset := map[string]any{"role": role}
			switch typed := source.(type) {
			case string:
				asset["metadata"] = map[string]any{"value": typed}
			case map[string]any:
				if assetID, _ := typed["asset_id"].(string); strings.TrimSpace(assetID) != "" {
					asset["asset_id"] = assetID
				}
				if relationID, _ := typed["asset_relation_id"].(string); strings.TrimSpace(relationID) != "" {
					asset["asset_relation_id"] = relationID
				}
				if refID, _ := typed["source_reference_id"].(string); strings.TrimSpace(refID) != "" {
					asset["source_reference_id"] = refID
				}
				asset["metadata"] = typed
			default:
				asset["metadata"] = map[string]any{"value": typed}
			}
			assets = append(assets, asset)
		}
		raw["source_assets"] = assets
	}
	return mustJSON(raw)
}

func (s *Service) createPlatformDeconstructionRuntimeJob(item *models.EcommerceVisualDeconstructionJob, manifest map[string]any) error {
	if item == nil || s.runtimeCreator == nil {
		return nil
	}
	manifest = sanitizedDeconstructionManifest(manifest)
	runtimeManifest, manifestErr := s.platformDeconstructionRuntimeInputManifest(item, manifest)
	if manifestErr != nil {
		item.RuntimeJobID = ""
		item.Status = models.VisualDeconstructionStatusContractNeeded
		item.Stage = "contract_needed"
		item.StageMessage = manifestErr.Error()
		item.ErrorCode = "PLATFORM_DECONSTRUCTION_INPUT_INVALID"
		item.ErrorMessage = manifestErr.Error()
		item.Metadata = mustJSON(mergeObjectMaps(decodeObject(item.Metadata), map[string]any{
			"unavailable_reason": "contract-needed",
			"platform_blocker": map[string]any{
				"code":    "PLATFORM_DECONSTRUCTION_INPUT_INVALID",
				"message": manifestErr.Error(),
			},
		}))
		return s.repo.SaveDeconstructionJob(item)
	}
	idempotencyKey := fmt.Sprintf("ecommerce:visual_deconstruction:%s:%s:%s", item.OrganizationID, item.SessionID, strings.TrimSpace(item.IdempotencyKey))
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:    "ecommerce",
		TaskType:       "image_understanding",
		ProviderMode:   "async",
		OrganizationID: item.OrganizationID,
		UserID:         item.UserID,
		SourceType:     "visual_deconstruction",
		SourceID:       item.ID,
		IdempotencyKey: idempotencyKey,
		InputManifest:  mustJSON(runtimeManifest),
		Metadata: mustJSON(map[string]any{
			"product_id":           item.ProductID,
			"sku_code":             item.SKUCode,
			"session_id":           item.SessionID,
			"source_reference_id":  item.SourceReferenceID,
			"source_reference_ids": decodeObject(item.Metadata)["source_reference_ids"],
			"source_roles":         decodeObject(item.Metadata)["source_roles"],
			"capability_code":      item.CapabilityCode,
		}),
		Priority:       100,
		MaxAttempts:    3,
		TimeoutSeconds: 600,
	})
	if err != nil {
		item.RuntimeJobID = ""
		item.Status = models.VisualDeconstructionStatusContractNeeded
		item.Stage = "contract_needed"
		item.StageMessage = safePlatformRuntimeJobCreateErrorMessage
		item.ErrorCode = "PLATFORM_RUNTIME_JOB_CREATE_FAILED"
		item.ErrorMessage = safePlatformRuntimeJobCreateErrorMessage
		item.Metadata = mustJSON(mergeObjectMaps(decodeObject(item.Metadata), map[string]any{
			"unavailable_reason": "contract-needed",
			"platform_blocker": map[string]any{
				"code":    "PLATFORM_RUNTIME_JOB_CREATE_FAILED",
				"message": safePlatformRuntimeJobCreateErrorMessage,
			},
		}))
		return s.repo.SaveDeconstructionJob(item)
	}
	item.RuntimeJobID = runtimeJob.ID
	item.ErrorCode = ""
	item.ErrorMessage = ""
	item.Stage = defaultString(runtimeJob.Stage, "queued")
	item.StageMessage = defaultString(runtimeJob.StageMessage, "Platform runtime job created")
	item.Status = mapVisualDeconstructionRuntimeStatus(runtimeJob.Status)
	if item.Status == models.VisualDeconstructionStatusProcessing && item.Progress < 5 {
		item.Progress = 5
	}
	item.Metadata = mustJSON(mergeObjectMaps(decodeObject(item.Metadata), map[string]any{
		"platform_source_type": "visual_deconstruction",
		"platform_source_id":   item.ID,
	}))
	return s.repo.SaveDeconstructionJob(item)
}

func (s *Service) platformDeconstructionRuntimeInputManifest(item *models.EcommerceVisualDeconstructionJob, businessManifest map[string]any) (map[string]any, error) {
	if item == nil {
		return nil, fmt.Errorf("deconstruction job is required")
	}
	sources, err := s.repo.ListSourceReferences(item.OrganizationID, item.SessionID)
	if err != nil {
		return nil, err
	}
	sourceAssets := make([]map[string]any, 0, len(sources))
	sourceAssetIDs := make([]string, 0, len(sources))
	for i := range sources {
		src := sources[i]
		if src.Status != models.VisualSourceStatusReady || src.ResolveStatus != models.VisualSourceStatusReady {
			continue
		}
		if strings.TrimSpace(src.ID) != strings.TrimSpace(item.SourceReferenceID) {
			continue
		}
		metadata := sanitizeDeconstructionRequestMetadata(decodeObject(src.Metadata))
		role := sourceRoleFromMetadata(metadata, src.SourceKind)
		asset := map[string]any{
			"id":                  strings.TrimSpace(src.AssetID),
			"source_reference_id": src.ID,
			"asset_relation_id":   src.AssetRelationID,
			"role":                role,
			"source_kind":         src.SourceKind,
			"source_ref":          src.SourceRef,
			"source_url":          src.SourceURL,
			"mime_type":           src.MimeType,
		}
		if strings.TrimSpace(src.AssetID) != "" && s.assetRepo != nil {
			if detail, err := s.assetRepo.FindAssetByID(item.OrganizationID, src.AssetID); err == nil && detail != nil {
				asset["id"] = detail.ID
				asset["storage_key"] = detail.StorageKey
				asset["mime_type"] = defaultString(detail.MimeType, src.MimeType)
				asset["width"] = detail.Width
				asset["height"] = detail.Height
				sourceAssetIDs = append(sourceAssetIDs, detail.ID)
			}
		}
		if strings.TrimSpace(fmt.Sprint(asset["source_url"])) == "" && strings.TrimSpace(src.SourceRef) != "" {
			asset["source_url"] = src.SourceRef
		}
		if strings.TrimSpace(fmt.Sprint(asset["source_url"])) == "" {
			continue
		}
		sourceAssets = append(sourceAssets, asset)
	}
	if len(sourceAssets) == 0 {
		return nil, fmt.Errorf("deconstruction runtime requires at least one resolved source asset")
	}
	return map[string]any{
		"input_mode":                  "dual_track_sources",
		"source_role_output_required": true,
		"source_reference_id":         item.SourceReferenceID,
		"source_reference_ids":        businessManifest["source_reference_ids"],
		"source_references":           businessManifest["source_references"],
		"source_asset_ids":            sourceAssetIDs,
		"source_assets":               sourceAssets,
		"prompt_snapshot": map[string]any{
			"user_prompt": "Look at the provided ecommerce image and return only strict JSON: {\"deconstruction_elements\":[{\"source_role\":\"sku\",\"source_reference_id\":\"SOURCE_REFERENCE_ID\",\"element_type\":\"product_fact\",\"element_key\":\"visual_description\",\"label\":\"Visual description\",\"value\":{\"description\":\"what is visible in the image\"},\"confidence\":0.8,\"readiness\":\"ready\"},{\"source_role\":\"reference\",\"source_reference_id\":\"SOURCE_REFERENCE_ID\",\"element_type\":\"reference_strategy\",\"element_key\":\"style\",\"label\":\"Reference style\",\"value\":{\"style\":\"visible style cues\"},\"confidence\":0.8,\"readiness\":\"ready\"}]}. Use the source_reference_id values supplied in the request metadata when possible. Do not include markdown.",
		},
		"params_snapshot": map[string]any{
			"schema_version":     "ecommerce.visual_deconstruction.runtime.v1",
			"required_tracks":    []string{"sku", "reference"},
			"requested_elements": businessManifest["requested_elements"],
			"output_schema":      "visual-deconstruction.v2",
		},
		"ecommerce_snapshot": businessManifest,
	}, nil
}

type dualTrackSourceReferences struct {
	PrimarySourceReferenceID string
	Signature                string
	SourceReferenceIDs       []string
	SourceRoles              map[string]string
	ManifestSources          []map[string]any
}

func resolveDualTrackSourceReferences(sources []models.EcommerceVisualSourceReference, requestedSourceID string) (*dualTrackSourceReferences, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("dual-track source references are required before deconstruction")
	}
	var sku, reference *models.EcommerceVisualSourceReference
	for i := range sources {
		if sources[i].Status != models.VisualSourceStatusReady || sources[i].ResolveStatus != models.VisualSourceStatusReady {
			continue
		}
		role := sourceRoleFromMetadata(decodeObject(sources[i].Metadata), sources[i].SourceKind)
		switch role {
		case "sku":
			if sku == nil || sources[i].CreatedAt.After(sku.CreatedAt) {
				sku = &sources[i]
			}
		case "reference":
			if reference == nil || sources[i].CreatedAt.After(reference.CreatedAt) {
				reference = &sources[i]
			}
		}
	}
	if sku == nil || reference == nil {
		return nil, fmt.Errorf("dual-track source references require ready sku and reference tracks")
	}
	primary := sku
	if requestedSourceID != "" {
		matched := false
		for i := range sources {
			if sources[i].ID == requestedSourceID {
				matched = true
				if sources[i].ID == reference.ID {
					primary = reference
				}
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("source_reference_id is not in this session")
		}
	}
	ordered := []models.EcommerceVisualSourceReference{*sku, *reference}
	ids := make([]string, 0, len(ordered))
	roles := make(map[string]string, len(ordered))
	manifestSources := make([]map[string]any, 0, len(ordered))
	for _, src := range ordered {
		metadata := sanitizeDeconstructionRequestMetadata(decodeObject(src.Metadata))
		role := sourceRoleFromMetadata(metadata, src.SourceKind)
		ids = append(ids, src.ID)
		roles[src.ID] = role
		manifestSources = append(manifestSources, map[string]any{
			"source_reference_id": src.ID,
			"role":                role,
			"source_kind":         src.SourceKind,
			"source_ref":          src.SourceRef,
			"asset_id":            src.AssetID,
			"asset_relation_id":   src.AssetRelationID,
			"mime_type":           src.MimeType,
			"metadata":            metadata,
		})
	}
	return &dualTrackSourceReferences{
		PrimarySourceReferenceID: primary.ID,
		Signature:                strings.Join(ids, ","),
		SourceReferenceIDs:       ids,
		SourceRoles:              roles,
		ManifestSources:          manifestSources,
	}, nil
}

func sourceRoleFromMetadata(metadata map[string]any, sourceKind string) string {
	role, _ := metadata["source_role"].(string)
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "sku" || role == "reference" {
		return role
	}
	if sourceKind == models.VisualSourceKindURL {
		return "reference"
	}
	return "sku"
}

func deconstructionIdempotencyKey(orgID, sessionID, sourceID string, req CreateDeconstructionJobRequest) string {
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		return key
	}
	payload := map[string]any{
		"organization_id":     orgID,
		"session_id":          sessionID,
		"source_reference_id": sourceID,
		"requested_elements":  req.RequestedElements,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "server-" + hex.EncodeToString(sum[:])[:24]
}

func runtimeCapabilityForTask(matrix *platform.RuntimeCapabilityMatrix, taskType string) (platform.RuntimeCapabilityItem, bool) {
	if matrix == nil {
		return platform.RuntimeCapabilityItem{}, false
	}
	for _, item := range matrix.Items {
		if item.TaskType == taskType {
			return item, true
		}
	}
	return platform.RuntimeCapabilityItem{}, false
}

func runtimeCapabilityIsReady(item platform.RuntimeCapabilityItem) bool {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	contract := strings.ToLower(strings.TrimSpace(item.ContractStatus))
	return item.Available && (status == "available" || status == "ready") && (contract == "" || contract == "ready" || contract == "available")
}

func runtimeCapabilityUnavailableMessage(item platform.RuntimeCapabilityItem) string {
	for _, reason := range item.Reasons {
		if strings.TrimSpace(reason.Message) != "" {
			return reason.Message
		}
	}
	if strings.TrimSpace(item.UnavailableReason) != "" {
		return "Platform runtime capability unavailable: " + item.UnavailableReason
	}
	return "Platform runtime capability image_understanding is not ready"
}

func sanitizedRuntimeCapabilityMetadata(item platform.RuntimeCapabilityItem) map[string]any {
	return map[string]any{
		"task_type":          item.TaskType,
		"status":             item.Status,
		"available":          item.Available,
		"unavailable_reason": item.UnavailableReason,
		"contract_status":    item.ContractStatus,
	}
}

func sanitizedDeconstructionManifest(manifest map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range manifest {
		if isForbiddenDeconstructionMetadataKey(k) {
			continue
		}
		out[k] = sanitizeDeconstructionMetadataValue(v)
	}
	return out
}

func sanitizeDeconstructionRequestMetadata(raw map[string]any) map[string]any {
	cleaned, _ := sanitizeDeconstructionMetadataValue(raw).(map[string]any)
	if cleaned == nil {
		return map[string]any{}
	}
	return cleaned
}

func sanitizeDeconstructionMetadataValue(raw any) any {
	switch value := raw.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if isForbiddenDeconstructionMetadataKey(key) {
				continue
			}
			out[key] = sanitizeDeconstructionMetadataValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, child := range value {
			out = append(out, sanitizeDeconstructionMetadataValue(child))
		}
		return out
	default:
		return raw
	}
}

func isForbiddenDeconstructionMetadataKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "idempotency_key", "platform_runtime_idempotency_key",
		"runtime_job_id", "provider_job_id", "provider", "provider_code", "provider_payload", "provider_response",
		"storage_key", "storage_keys", "storagekey", "internal_storage_key",
		"billing", "billing_truth", "billing_status", "billing_context", "billing_details", "billing_detail", "billable_item_code", "charge", "charge_id", "chargeid", "charges", "charged", "charge_status":
		return true
	default:
		return isForbiddenExecutionArtifactKey(key)
	}
}

func mapVisualDeconstructionRuntimeStatus(status string) string {
	mapped, _ := normalizeVisualDeconstructionRuntimeStatus(status, true)
	return mapped
}

func normalizeVisualDeconstructionRuntimeStatus(status string, allowEmpty bool) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(status))
	if trimmed == "" {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("%w: runtime result status is required", ErrInternalCallbackInvalid)
	}
	switch trimmed {
	case "queued", "pending":
		return models.VisualDeconstructionStatusQueued, nil
	case "processing", "running", "started":
		return models.VisualDeconstructionStatusProcessing, nil
	case "completed", "succeeded", "success":
		return models.VisualDeconstructionStatusCompleted, nil
	case "failed", "error":
		return models.VisualDeconstructionStatusFailed, nil
	case "canceled", "cancelled":
		return models.VisualDeconstructionStatusCanceled, nil
	default:
		return "", fmt.Errorf("%w: unsupported runtime status %q", ErrInternalCallbackInvalid, status)
	}
}

func (s *Service) HasDeconstructionJob(jobID string) bool {
	if strings.TrimSpace(jobID) == "" {
		return false
	}
	_, err := s.repo.FindDeconstructionJobByID(strings.TrimSpace(jobID))
	return err == nil
}

func (s *Service) InternalUpdateDeconstructionRuntime(jobID string, input InternalRuntimeUpdateRequest) (*models.EcommerceVisualDeconstructionJob, error) {
	item, err := s.repo.FindDeconstructionJobByID(strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if status := strings.TrimSpace(input.Status); status != "" {
		mapped, err := normalizeVisualDeconstructionRuntimeStatus(status, true)
		if err != nil {
			return nil, err
		}
		if mapped != "" {
			item.Status = mapped
		}
	}
	if stage := strings.TrimSpace(input.Stage); stage != "" {
		item.Stage = stage
	}
	if message := strings.TrimSpace(input.StageMessage); message != "" {
		item.StageMessage = message
	}
	if input.Progress != nil {
		item.Progress = clampVisualProgress(*input.Progress, item.Status)
	}
	if runtimeJobID := strings.TrimSpace(input.RuntimeJobID); runtimeJobID != "" {
		item.RuntimeJobID = runtimeJobID
	}
	if code := strings.TrimSpace(input.ErrorCode); code != "" {
		item.ErrorCode = code
	}
	if message := strings.TrimSpace(input.ErrorMessage); message != "" {
		item.ErrorMessage = message
	}
	applyVisualDeconstructionCompletionTimestamps(item)
	if err := s.repo.SaveDeconstructionJob(item); err != nil {
		return nil, err
	}
	_ = s.updateSessionForDeconstructionJob(item)
	return item, nil
}

func (s *Service) InternalRecordDeconstructionResults(jobID string, input InternalRecordResultsRequest) (*models.EcommerceVisualDeconstructionJob, error) {
	item, err := s.repo.FindDeconstructionJobByID(strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	status, err := normalizeVisualDeconstructionRuntimeStatus(input.Status, false)
	if err != nil {
		return nil, err
	}
	elementInputs := visualResultElements(input)
	item.Status = status
	item.Progress = clampVisualProgress(input.Progress, item.Status)
	if stage := strings.TrimSpace(input.Stage); stage != "" {
		item.Stage = stage
	} else {
		item.Stage = visualResultStage(item.Status)
	}
	item.StageMessage = defaultString(strings.TrimSpace(input.StageMessage), visualResultStageMessage(item.Status))
	item.ErrorCode = strings.TrimSpace(input.ErrorCode)
	item.ErrorMessage = strings.TrimSpace(input.ErrorMessage)
	applyVisualDeconstructionCompletionTimestamps(item)

	sourceRefs, err := s.repo.ListSourceReferences(item.OrganizationID, item.SessionID)
	if err != nil {
		return nil, err
	}
	sourceIndex := deconstructionResultSourceIndex(sourceRefs)
	if len(elementInputs) == 0 && item.Status == models.VisualDeconstructionStatusCompleted {
		elementInputs = fallbackVisualResultElementsFromProviderText(input, sourceRefs)
	}
	if item.Status == models.VisualDeconstructionStatusCompleted && jobRequiresDualTrackResultCoverage(item) && visualProviderText(input) != "" {
		elementInputs = ensureDualTrackVisualResultElements(elementInputs, input, sourceRefs)
	}
	if len(elementInputs) == 0 && item.Status == models.VisualDeconstructionStatusCompleted {
		if len(input.Variants) > 0 {
			return nil, fmt.Errorf("%w: visual deconstruction result must contain deconstruction elements; image generation variants are not accepted", ErrInternalCallbackInvalid)
		}
		return nil, fmt.Errorf("%w: visual deconstruction completed result contains no elements", ErrInternalCallbackInvalid)
	}
	elements := make([]models.EcommerceVisualDeconstructionElement, 0, len(elementInputs))
	for idx, in := range elementInputs {
		element, err := internalResultElementToModel(item, in, idx, sourceIndex)
		if err != nil {
			return nil, err
		}
		elements = append(elements, element)
	}
	if err := validateDualTrackResultCoverage(item, elements); err != nil {
		return nil, err
	}
	item.OutputManifestJSON = mustJSON(map[string]any{
		"schema_version": "visual-deconstruction-result.v1",
		"status":         item.Status,
		"stage":          item.Stage,
		"elements_count": len(elements),
		"error_code":     item.ErrorCode,
	})
	if err := s.repo.SaveDeconstructionResult(item, elements, visualWorkflowStatusForDeconstruction(item.Status)); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) SaveGenerationVersionAsTemplate(userID, orgID, sessionID, versionID string, req SaveGenerationTemplateRequest) (*SaveGenerationTemplateResponse, error) {
	if s.workspaceRepo == nil {
		return nil, fmt.Errorf("workspace template repository is not configured")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user is required to save a template")
	}
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	idx := -1
	for i := range versions {
		if versions[i].VersionID == strings.TrimSpace(versionID) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	version := versions[idx]
	if version.Status != "completed" || len(version.ResultAssets) == 0 {
		return nil, fmt.Errorf("generation version must be completed with result assets before saving as template")
	}
	assetID := strings.TrimSpace(req.AssetID)
	if assetID == "" {
		assetID = strings.TrimSpace(version.SelectedResultAssetID)
	}
	if assetID == "" {
		return nil, fmt.Errorf("selected result asset is required before saving as template")
	}
	var selected ResultAssetDTO
	found := false
	for _, asset := range version.ResultAssets {
		if strings.TrimSpace(asset.AssetID) == assetID {
			selected = asset
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("asset_id is not a result asset of this generation version")
	}
	if _, err := s.assetRepo.FindAssetByID(orgID, assetID); err != nil {
		return nil, fmt.Errorf("selected result asset not found: %w", err)
	}
	now := time.Now().UTC()
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = fmt.Sprintf("%s %s V%d visual template", defaultString(session.SKUCode, "SKU"), defaultString(version.Stage, "generation"), idx+1)
	}
	scenario := strings.TrimSpace(req.Scenario)
	if scenario == "" {
		scenario = "Ecommerce SKU visual generation template saved from a completed Workshop result."
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = fmt.Sprintf("Saved from Visual Workflow session %s, generation version %s, asset %s.", session.ID, version.VersionID, assetID)
	}
	tags := []string{"visual-workflow", "generated-result", "ecommerce-v2", session.SKUCode}
	for _, tag := range req.Tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	record := repository.SavedTemplateRecord{
		ID:          buildID("tpl"),
		Platform:    "ecommerce",
		Tags:        dedupeStrings(tags),
		UsageCount:  "0",
		Favorite:    0,
		SavedAt:     now.Format(time.RFC3339),
		SourceType:  "visual_generation_result",
		SourceLabel: fmt.Sprintf("product=%s sku=%s session=%s version=%s asset=%s", session.ProductID, session.SKUCode, session.ID, version.VersionID, assetID),
		ZH:          repository.TemplateCopy{Title: title, Summary: summary, Scenario: scenario},
		EN:          repository.TemplateCopy{Title: title, Summary: summary, Scenario: scenario},
	}
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		record.ID = "tpl_" + shortStableHash(fmt.Sprintf("%s:%s:%s:%s", orgID, session.ID, version.VersionID, strings.TrimSpace(req.IdempotencyKey)))
	}
	items, err := s.workspaceRepo.SaveTemplate(repository.Scope{UserID: userID, OrgID: orgID}, record)
	if err != nil {
		return nil, err
	}
	metadata := mergeGenerationMetadata(version.Metadata, map[string]any{"saved_template": map[string]any{"template_id": record.ID, "asset_id": assetID, "saved_at": record.SavedAt}})
	version.Metadata = metadata
	version.UpdatedAt = now.Format(time.RFC3339Nano)
	versions[idx] = version
	if err := s.saveGenerationVersions(session, versions, visualWorkflowStatusForGeneration(version.Status)); err != nil {
		return nil, err
	}
	return &SaveGenerationTemplateResponse{SessionID: session.ID, VersionID: version.VersionID, ProductID: session.ProductID, SKUCode: session.SKUCode, SelectedResultAssetID: assetID, Template: record, SavedTemplates: items, GenerationVersion: version, AssetContentURL: selected.AssetContentURL}, nil
}

func (s *Service) HasGenerationVersion(versionID string) bool {
	if strings.TrimSpace(versionID) == "" {
		return false
	}
	_, _, _, err := s.findGenerationVersionByID(strings.TrimSpace(versionID))
	return err == nil
}

func (s *Service) findGenerationVersionByCallbackID(callbackID string) (*models.EcommerceVisualWorkflowSession, []GenerationVersionDTO, int, error) {
	callbackID = strings.TrimSpace(callbackID)
	if callbackID == "" {
		return nil, nil, -1, gorm.ErrRecordNotFound
	}
	if session, versions, idx, err := s.findGenerationVersionByID(callbackID); err == nil {
		return session, versions, idx, nil
	}
	session, err := s.repo.FindSessionByGenerationVersionID(callbackID)
	if err != nil {
		return nil, nil, -1, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	for i := range versions {
		if strings.TrimSpace(versions[i].RuntimeJobID) == callbackID {
			return session, versions, i, nil
		}
	}
	return nil, nil, -1, gorm.ErrRecordNotFound
}

func (s *Service) InternalUpdateGenerationRuntime(versionID string, input InternalRuntimeUpdateRequest) (*GenerationVersionDTO, error) {
	session, versions, idx, err := s.findGenerationVersionByCallbackID(strings.TrimSpace(versionID))
	if err != nil {
		return nil, err
	}
	version := versions[idx]
	if status := strings.TrimSpace(input.Status); status != "" {
		normalizedStatus := normalizeGenerationRuntimeCallbackStatus(status)
		if normalizedStatus == "" {
			return nil, fmt.Errorf("%w: unsupported generation runtime status %q", ErrInternalCallbackInvalid, status)
		}
		version.Status = normalizedStatus
	}
	if stage := strings.TrimSpace(input.Stage); stage != "" {
		version.Stage = mapGenerationRuntimeStage(stage, version.Status)
	} else if version.Stage == "" {
		version.Stage = mapGenerationRuntimeStage("", version.Status)
	}
	if input.Progress != nil {
		version.Progress = clampGenerationProgress(*input.Progress, version.Status)
	}
	if runtimeJobID := strings.TrimSpace(input.RuntimeJobID); runtimeJobID != "" {
		version.RuntimeJobID = runtimeJobID
	}
	version.Blockers = generationBlockersForStatus(version.Blockers, version.Status, input.ErrorCode, input.ErrorMessage)
	version.Metadata = mergeGenerationMetadata(version.Metadata, map[string]any{
		"runtime_update": sanitizeGenerationManifestValue(map[string]any{
			"status":        version.Status,
			"stage":         version.Stage,
			"error_code":    strings.TrimSpace(input.ErrorCode),
			"error_message": strings.TrimSpace(input.ErrorMessage),
		}),
	})
	version.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	versions[idx] = version
	if err := s.saveGenerationVersions(session, versions, visualWorkflowStatusForGeneration(version.Status)); err != nil {
		return nil, err
	}
	return &version, nil
}

func (s *Service) InternalRecordGenerationResults(versionID string, input InternalRecordResultsRequest) (*GenerationVersionDTO, error) {
	session, versions, idx, err := s.findGenerationVersionByCallbackID(strings.TrimSpace(versionID))
	if err != nil {
		return nil, err
	}
	version := versions[idx]
	status := normalizeGenerationResultStatus(input.Status)
	if status == "" {
		return nil, fmt.Errorf("%w: generation result status is required", ErrInternalCallbackInvalid)
	}
	if len(input.Variants) == 0 && status == "completed" {
		return nil, fmt.Errorf("%w: completed generation result contains no variants", ErrInternalCallbackInvalid)
	}
	version.Status = status
	version.Progress = clampGenerationProgress(input.Progress, status)
	version.Stage = mapGenerationRuntimeStage(input.Stage, status)
	version.Blockers = generationBlockersForStatus(version.Blockers, status, input.ErrorCode, input.ErrorMessage)
	resultAssets := dedupeResultAssets(version.ResultAssets)
	selectedAssetID := strings.TrimSpace(version.SelectedResultAssetID)
	for idxVariant, raw := range input.Variants {
		asset, selected, err := s.findOrCreateGenerationResultAsset(session, &version, input, raw, idxVariant)
		if err != nil {
			return nil, fmt.Errorf("record generation result variant %d: %w", idxVariant, err)
		}
		if asset == nil {
			continue
		}
		role := stringFromMap(raw, "role")
		if role == "" {
			role = "primary"
		}
		resultAssets = upsertResultAsset(resultAssets, ResultAssetDTO{
			AssetID:         asset.ID,
			AssetContentURL: "/api/v1/ecommerce/assets/" + asset.ID + "/content",
			Role:            role,
			Selected:        selected,
			Metadata: sanitizeGenerationResultProjectionMetadata(map[string]any{
				"variant_index": idxVariant,
				"status":        stringFromMap(raw, "status"),
			}),
		})
		if selected || selectedAssetID == "" {
			selectedAssetID = asset.ID
		}
	}
	for i := range resultAssets {
		resultAssets[i].Selected = resultAssets[i].AssetID == selectedAssetID && selectedAssetID != ""
	}
	version.ResultAssets = resultAssets
	version.SelectedResultAssetID = selectedAssetID
	version.Metadata = mergeGenerationMetadata(version.Metadata, map[string]any{"runtime_result": sanitizeGenerationResultProjectionMetadata(map[string]any{"status": status, "stage": version.Stage, "variants_count": len(input.Variants), "error_code": strings.TrimSpace(input.ErrorCode), "error_message": strings.TrimSpace(input.ErrorMessage)})})
	version.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := validateGenerationVersion(&version); err != nil {
		return nil, err
	}
	versions[idx] = version
	if err := s.saveGenerationVersions(session, versions, visualWorkflowStatusForGeneration(version.Status)); err != nil {
		return nil, err
	}
	return &version, nil
}

func visualResultElements(input InternalRecordResultsRequest) []InternalResultElementRequest {
	if len(input.DeconstructionElements) > 0 {
		return input.DeconstructionElements
	}
	if len(input.Elements) > 0 {
		return input.Elements
	}
	if len(input.Metadata.DeconstructionElements) > 0 {
		return input.Metadata.DeconstructionElements
	}
	for _, variant := range input.Variants {
		for _, key := range []string{"inline_data", "text", "result_text"} {
			if text, _ := variant[key].(string); strings.TrimSpace(text) != "" {
				if elements := parseVisualResultElementsFromJSON(text); len(elements) > 0 {
					return elements
				}
			}
		}
	}
	return nil
}

func parseVisualResultElementsFromJSON(raw string) []InternalResultElementRequest {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	var envelope struct {
		Elements               []InternalResultElementRequest `json:"elements"`
		DeconstructionElements []InternalResultElementRequest `json:"deconstruction_elements"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return nil
	}
	if len(envelope.DeconstructionElements) > 0 {
		return envelope.DeconstructionElements
	}
	if len(envelope.Elements) > 0 {
		return envelope.Elements
	}
	var single InternalResultElementRequest
	if err := json.Unmarshal([]byte(text), &single); err == nil && (strings.TrimSpace(single.ElementType) != "" || strings.TrimSpace(single.ElementKey) != "" || len(single.Value) > 0) {
		return []InternalResultElementRequest{single}
	}
	return nil
}

func fallbackVisualResultElementsFromProviderText(input InternalRecordResultsRequest, sourceRefs []models.EcommerceVisualSourceReference) []InternalResultElementRequest {
	providerText := visualProviderText(input)
	if providerText == "" {
		return nil
	}
	return visualResultElementsForSources(providerText, sourceRefs)
}

func ensureDualTrackVisualResultElements(elements []InternalResultElementRequest, input InternalRecordResultsRequest, sourceRefs []models.EcommerceVisualSourceReference) []InternalResultElementRequest {
	roles := map[string]bool{}
	for i := range elements {
		role := strings.ToLower(strings.TrimSpace(elements[i].SourceRole))
		if role == "" {
			if rawRole, _ := elements[i].Metadata["source_role"].(string); rawRole != "" {
				role = strings.ToLower(strings.TrimSpace(rawRole))
			}
		}
		if role == "sku" || role == "reference" {
			roles[role] = true
		}
	}
	if roles["sku"] && roles["reference"] {
		return elements
	}
	providerText := visualProviderText(input)
	if providerText == "" {
		providerText = "Provider returned a structured visual deconstruction result without complete source-role coverage."
	}
	fallbacks := visualResultElementsForSources(providerText, sourceRefs)
	for _, fallback := range fallbacks {
		if !roles[strings.ToLower(strings.TrimSpace(fallback.SourceRole))] {
			fallback.SortOrder = len(elements)
			elements = append(elements, fallback)
		}
	}
	return elements
}

func visualProviderText(input InternalRecordResultsRequest) string {
	providerText := ""
	if providerText == "" {
		for _, variant := range input.Variants {
			for _, key := range []string{"inline_data", "text", "result_text"} {
				if text, _ := variant[key].(string); strings.TrimSpace(text) != "" {
					providerText = strings.TrimSpace(text)
					break
				}
			}
			if providerText != "" {
				break
			}
		}
	}
	return providerText
}

func visualResultElementsForSources(providerText string, sourceRefs []models.EcommerceVisualSourceReference) []InternalResultElementRequest {
	out := make([]InternalResultElementRequest, 0, len(sourceRefs))
	for i := range sourceRefs {
		role := sourceRoleFromMetadata(decodeObject(sourceRefs[i].Metadata), sourceRefs[i].SourceKind)
		if role != "sku" && role != "reference" {
			continue
		}
		elementType := "product_fact"
		elementKey := "provider_visual_description"
		label := "Provider visual description"
		if role == "reference" {
			elementType = "reference_strategy"
			elementKey = "provider_reference_description"
			label = "Provider reference description"
		}
		out = append(out, InternalResultElementRequest{
			SourceRole:        role,
			SourceReferenceID: sourceRefs[i].ID,
			SourceAssetID:     sourceRefs[i].AssetID,
			ElementType:       elementType,
			ElementKey:        elementKey,
			Label:             label,
			Value:             map[string]any{"provider_text": providerText},
			Confidence:        0.5,
			Readiness:         models.VisualReadinessNeedsReview,
			SortOrder:         len(out),
			Metadata:          map[string]any{"fallback_from_provider_text": true},
		})
	}
	return out
}

func internalResultElementToModel(job *models.EcommerceVisualDeconstructionJob, input InternalResultElementRequest, idx int, sourceIndex map[string]string) (models.EcommerceVisualDeconstructionElement, error) {
	readiness := strings.TrimSpace(input.Readiness)
	if !validVisualReadiness(readiness) {
		readiness = models.VisualReadinessNeedsReview
	}
	elementType := strings.TrimSpace(input.ElementType)
	if elementType == "" {
		elementType = "deconstructed_element"
	}
	elementKey := strings.TrimSpace(defaultString(input.ElementKey, input.Key))
	value := sanitizeDeconstructionRequestMetadata(input.Value)
	if value == nil {
		value = map[string]any{}
	}
	sortOrder := input.SortOrder
	if sortOrder == 0 {
		sortOrder = idx
	}
	confidence := input.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	metadata := sanitizeDeconstructionRequestMetadata(input.Metadata)
	sourceRole, sourceReferenceID, err := normalizeResultElementSourceProjection(input, sourceIndex)
	if err != nil {
		return models.EcommerceVisualDeconstructionElement{}, err
	}
	if sourceRole != "" {
		metadata["source_role"] = sourceRole
	}
	if sourceReferenceID != "" {
		metadata["source_reference_id"] = sourceReferenceID
	}
	return models.EcommerceVisualDeconstructionElement{
		ID:              buildID("vde"),
		OrganizationID:  job.OrganizationID,
		SessionID:       job.SessionID,
		JobID:           job.ID,
		ProductID:       job.ProductID,
		SKUCode:         job.SKUCode,
		ElementType:     elementType,
		ElementKey:      elementKey,
		Label:           strings.TrimSpace(input.Label),
		Confidence:      confidence,
		BoundingBoxJSON: mustJSON(sanitizeDeconstructionRequestMetadata(input.BoundingBox)),
		MaskAssetID:     strings.TrimSpace(input.MaskAssetID),
		SourceAssetID:   strings.TrimSpace(input.SourceAssetID),
		ValueJSON:       mustJSON(value),
		Readiness:       readiness,
		Selected:        input.Selected,
		Confirmed:       input.Confirmed,
		SortOrder:       sortOrder,
		Metadata:        mustJSON(metadata),
	}, nil
}

func deconstructionResultSourceIndex(sources []models.EcommerceVisualSourceReference) map[string]string {
	index := map[string]string{}
	for i := range sources {
		role := sourceRoleFromMetadata(decodeObject(sources[i].Metadata), sources[i].SourceKind)
		index[sources[i].ID] = role
	}
	return index
}

func normalizeResultElementSourceProjection(input InternalResultElementRequest, sourceIndex map[string]string) (string, string, error) {
	role := strings.ToLower(strings.TrimSpace(input.SourceRole))
	if role == "" {
		if rawRole, _ := input.Metadata["source_role"].(string); rawRole != "" {
			role = strings.ToLower(strings.TrimSpace(rawRole))
		}
	}
	if role != "" && role != "sku" && role != "reference" {
		return "", "", fmt.Errorf("%w: unsupported source_role %q", ErrInternalCallbackInvalid, input.SourceRole)
	}
	sourceReferenceID := strings.TrimSpace(input.SourceReferenceID)
	if sourceReferenceID == "" {
		if rawID, _ := input.Metadata["source_reference_id"].(string); rawID != "" {
			sourceReferenceID = strings.TrimSpace(rawID)
		}
	}
	if sourceReferenceID == "" {
		return role, "", nil
	}
	trustedRole, ok := sourceIndex[sourceReferenceID]
	if !ok {
		return "", "", fmt.Errorf("%w: source_reference_id %q does not belong to this visual workflow session", ErrInternalCallbackInvalid, sourceReferenceID)
	}
	if role == "" {
		role = trustedRole
	}
	if role != trustedRole {
		return "", "", fmt.Errorf("%w: source_role %q does not match source_reference_id %q role %q", ErrInternalCallbackInvalid, role, sourceReferenceID, trustedRole)
	}
	return role, sourceReferenceID, nil
}

func validateDualTrackResultCoverage(job *models.EcommerceVisualDeconstructionJob, elements []models.EcommerceVisualDeconstructionElement) error {
	if !jobRequiresDualTrackResultCoverage(job) || job.Status != models.VisualDeconstructionStatusCompleted {
		return nil
	}
	roles := map[string]bool{}
	for i := range elements {
		role := sourceRoleFromResultElement(elements[i])
		if role == "sku" || role == "reference" {
			roles[role] = true
		}
	}
	if !roles["sku"] || !roles["reference"] {
		return fmt.Errorf("%w: completed dual-track deconstruction result must contain both sku and reference source_role elements", ErrInternalCallbackInvalid)
	}
	return nil
}

func jobRequiresDualTrackResultCoverage(job *models.EcommerceVisualDeconstructionJob) bool {
	manifest := decodeObject(job.InputManifestJSON)
	if strings.EqualFold(fmt.Sprint(manifest["input_mode"]), "dual_track_sources") {
		return true
	}
	return false
}

func sourceRoleFromResultElement(element models.EcommerceVisualDeconstructionElement) string {
	metadata := decodeObject(element.Metadata)
	role, _ := metadata["source_role"].(string)
	return strings.ToLower(strings.TrimSpace(role))
}

func visualWorkflowStatusForDeconstruction(status string) string {
	switch status {
	case models.VisualDeconstructionStatusCompleted:
		return models.VisualWorkflowStatusReady
	case models.VisualDeconstructionStatusFailed:
		return models.VisualWorkflowStatusFailed
	case models.VisualDeconstructionStatusCanceled:
		return models.VisualWorkflowStatusCanceled
	case models.VisualDeconstructionStatusProcessing, models.VisualDeconstructionStatusQueued:
		return models.VisualWorkflowStatusProcessing
	default:
		return ""
	}
}

func (s *Service) updateSessionForDeconstructionJob(job *models.EcommerceVisualDeconstructionJob) error {
	session, err := s.repo.GetSession(job.OrganizationID, job.SessionID)
	if err != nil {
		return err
	}
	session.CurrentStage = models.VisualWorkflowStageDeconstruction
	if status := visualWorkflowStatusForDeconstruction(job.Status); status != "" {
		session.Status = status
	}
	return s.repo.SaveSession(session)
}

func applyVisualDeconstructionCompletionTimestamps(item *models.EcommerceVisualDeconstructionJob) {
	now := time.Now()
	if item.Status == models.VisualDeconstructionStatusCompleted {
		item.CompletedAt = &now
	}
}

func clampVisualProgress(progress int, status string) int {
	if status == models.VisualDeconstructionStatusCompleted {
		return 100
	}
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func visualResultStage(status string) string {
	switch status {
	case models.VisualDeconstructionStatusCompleted:
		return "completed"
	case models.VisualDeconstructionStatusFailed:
		return "failed"
	case models.VisualDeconstructionStatusCanceled:
		return "canceled"
	default:
		return "processing"
	}
}

func visualResultStageMessage(status string) string {
	switch status {
	case models.VisualDeconstructionStatusCompleted:
		return "Visual deconstruction completed"
	case models.VisualDeconstructionStatusFailed:
		return "Visual deconstruction failed"
	case models.VisualDeconstructionStatusCanceled:
		return "Visual deconstruction canceled"
	default:
		return "Visual deconstruction processing"
	}
}

func mergeObjectMaps(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (s *Service) findGenerationVersionByID(versionID string) (*models.EcommerceVisualWorkflowSession, []GenerationVersionDTO, int, error) {
	if strings.TrimSpace(versionID) == "" {
		return nil, nil, -1, gorm.ErrRecordNotFound
	}
	session, err := s.repo.FindSessionByGenerationVersionID(strings.TrimSpace(versionID))
	if err != nil {
		return nil, nil, -1, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	for i := range versions {
		if versions[i].VersionID == versionID {
			return session, versions, i, nil
		}
	}
	return nil, nil, -1, gorm.ErrRecordNotFound
}

func (s *Service) saveGenerationVersions(session *models.EcommerceVisualWorkflowSession, versions []GenerationVersionDTO, status string) error {
	encoded, err := marshalGenerationVersions(versions)
	if err != nil {
		return err
	}
	session.GenerationVersionsJSON = encoded
	session.CurrentStage = models.VisualWorkflowStageGeneration
	if status != "" {
		session.Status = status
	}
	return s.repo.SaveSession(session)
}

func (s *Service) findOrCreateGenerationResultAsset(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, input InternalRecordResultsRequest, raw map[string]any, idx int) (*models.EcommerceAsset, bool, error) {
	assetPayload := mapFromMap(raw, "asset")
	if len(assetPayload) == 0 {
		assetPayload = raw
	}
	assetID := stringFromMap(assetPayload, "asset_id")
	if assetID == "" {
		assetID = stringFromMap(raw, "asset_id")
	}
	if assetID != "" {
		asset, err := s.assetRepo.FindAssetByID(session.OrganizationID, assetID)
		if err != nil {
			return nil, false, fmt.Errorf("%w: generation result asset_id not found", ErrInternalCallbackInvalid)
		}
		return asset, boolFromMap(raw, "is_selected"), nil
	}
	storageKey := stringFromMap(assetPayload, "storage_key")
	if storageKey == "" {
		if input.Status == "completed" {
			return nil, false, fmt.Errorf("%w: completed generation result variant requires asset_id or storage_key", ErrInternalCallbackInvalid)
		}
		return nil, false, nil
	}
	if existing, err := s.assetRepo.FindAssetByStorageKey(session.OrganizationID, storageKey); err == nil && existing != nil {
		return existing, boolFromMap(raw, "is_selected"), nil
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	resultKey := fmt.Sprintf("%s:%d", version.VersionID, idx)
	metadata := sanitizeGenerationResultProjectionMetadata(map[string]any{
		"visual_workflow_session_id": session.ID,
		"generation_version_id":      version.VersionID,
		"generation_result_key":      resultKey,
		"variant_index":              idx,
		"variant_status":             stringFromMap(raw, "status"),
		"is_selected":                boolFromMap(raw, "is_selected"),
	})
	asset := &models.EcommerceAsset{
		ID:             buildID("asset"),
		OrganizationID: session.OrganizationID,
		UserID:         session.UserID,
		AssetType:      defaultString(stringFromMap(assetPayload, "asset_type"), "generated"),
		SourceType:     defaultString(stringFromMap(assetPayload, "source_type"), "generated"),
		StorageKey:     storageKey,
		MimeType:       stringFromMap(assetPayload, "mime_type"),
		Width:          intFromMap(assetPayload, "width"),
		Height:         intFromMap(assetPayload, "height"),
		FileName:       stringFromMap(assetPayload, "file_name"),
		Metadata:       mustJSON(metadata),
	}
	if err := s.assetRepo.CreateAsset(asset); err != nil {
		return nil, false, err
	}
	return asset, boolFromMap(raw, "is_selected"), nil
}

func normalizeGenerationResultStatus(status string) string {
	trimmed := strings.ToLower(strings.TrimSpace(status))
	switch trimmed {
	case "completed", "succeeded", "success":
		return "completed"
	case "processing", "running", "started":
		return "processing"
	case "queued", "pending":
		return "queued"
	case "failed", "error":
		return "failed"
	case "canceled", "cancelled":
		return "canceled"
	default:
		return ""
	}
}

func visualWorkflowStatusForGeneration(status string) string {
	switch status {
	case "completed":
		return models.VisualWorkflowStatusReady
	case "failed":
		return models.VisualWorkflowStatusFailed
	case "canceled":
		return models.VisualWorkflowStatusCanceled
	case "queued", "processing":
		return models.VisualWorkflowStatusProcessing
	default:
		return ""
	}
}

func clampGenerationProgress(progress int, status string) int {
	if status == "completed" {
		return 100
	}
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func generationBlockersForStatus(_ []ReadinessBlocker, status, code, message string) []ReadinessBlocker {
	if status != "failed" && status != "contract_needed" && status != "blocked" {
		return nil
	}
	code = defaultString(strings.TrimSpace(code), "RUNTIME_FAILED")
	message = defaultString(strings.TrimSpace(message), "Generation runtime failed")
	return []ReadinessBlocker{{Code: code, Target: "generation", Message: message}}
}

func mergeGenerationMetadata(base map[string]any, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return sanitizeGenerationResultProjectionMetadata(out)
}

func sanitizeGenerationResultProjectionMetadata(raw map[string]any) map[string]any {
	cleaned, _ := sanitizeGenerationManifestValue(raw).(map[string]any)
	if cleaned == nil {
		return map[string]any{}
	}
	return cleaned
}

func dedupeResultAssets(in []ResultAssetDTO) []ResultAssetDTO {
	out := []ResultAssetDTO{}
	seen := map[string]bool{}
	for _, item := range in {
		if strings.TrimSpace(item.AssetID) == "" || seen[item.AssetID] {
			continue
		}
		item.Metadata = sanitizeGenerationResultProjectionMetadata(item.Metadata)
		out = append(out, item)
		seen[item.AssetID] = true
	}
	return out
}

func upsertResultAsset(in []ResultAssetDTO, item ResultAssetDTO) []ResultAssetDTO {
	for i := range in {
		if in[i].AssetID == item.AssetID {
			in[i] = item
			return in
		}
	}
	return append(in, item)
}

func mapFromMap(raw map[string]any, key string) map[string]any {
	if raw == nil {
		return nil
	}
	if value, ok := raw[key].(map[string]any); ok {
		return value
	}
	return nil
}

func stringFromMap(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	switch value := raw[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func boolFromMap(raw map[string]any, key string) bool {
	if raw == nil {
		return false
	}
	value, _ := raw[key].(bool)
	return value
}

func intFromMap(raw map[string]any, key string) int {
	if raw == nil {
		return 0
	}
	switch value := raw[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return 0
	}
}

func (s *Service) GetDeconstructionJob(orgID, sessionID, jobID string) (*models.EcommerceVisualDeconstructionJob, error) {
	return s.repo.GetDeconstructionJob(orgID, sessionID, jobID)
}

func (s *Service) ListElements(orgID, sessionID string) ([]models.EcommerceVisualDeconstructionElement, error) {
	return s.repo.ListDeconstructionElements(orgID, sessionID)
}

func (s *Service) UpdateElement(orgID, sessionID, elementID string, req UpdateElementRequest) (*models.EcommerceVisualDeconstructionElement, error) {
	item, err := s.repo.GetDeconstructionElement(orgID, sessionID, elementID)
	if err != nil {
		return nil, err
	}
	if req.Selected != nil {
		item.Selected = *req.Selected
	}
	if req.Confirmed != nil {
		item.Confirmed = *req.Confirmed
	}
	if req.Readiness != "" {
		readiness := strings.TrimSpace(req.Readiness)
		if !validVisualReadiness(readiness) {
			return nil, fmt.Errorf("invalid readiness: %s", readiness)
		}
		item.Readiness = readiness
	}
	if req.Label != "" {
		item.Label = strings.TrimSpace(req.Label)
	}
	if req.Value != nil {
		item.ValueJSON = mustJSON(req.Value)
	}
	if req.BoundingBox != nil {
		item.BoundingBoxJSON = mustJSON(req.BoundingBox)
	}
	if err := applyAttentionDecisionToElement(item, req.Decision, req.GroupPath, req.TargetAssetID, req.Rationale, req.Confidence, req.Metadata); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDeconstructionElement(item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Decision) != "" || req.Metadata != nil || len(req.GroupPath) > 0 || strings.TrimSpace(req.TargetAssetID) != "" {
		if err := s.refreshIntentInputManifest(orgID, sessionID, nil); err != nil {
			return nil, err
		}
	}
	return item, nil
}

func (s *Service) ApplyAttentionTree(orgID, sessionID string, req ApplyAttentionTreeRequest) ([]models.EcommerceVisualDeconstructionElement, error) {
	if len(req.Decisions) == 0 {
		return nil, fmt.Errorf("attention decisions are required")
	}
	if _, err := s.repo.GetSession(orgID, sessionID); err != nil {
		return nil, err
	}
	if err := validateAttentionTreeRequest(req); err != nil {
		return nil, err
	}
	out := make([]models.EcommerceVisualDeconstructionElement, 0, len(req.Decisions))
	for _, decision := range req.Decisions {
		item, err := s.repo.GetDeconstructionElement(orgID, sessionID, strings.TrimSpace(decision.ElementID))
		if err != nil {
			return nil, err
		}
		selected := decision.Decision == "keep" || decision.Decision == "replace"
		item.Selected = selected
		item.Confirmed = decision.Decision != "needs_review"
		if decision.Decision == "needs_review" {
			item.Readiness = models.VisualReadinessNeedsReview
		} else {
			item.Readiness = models.VisualReadinessReady
		}
		if err := applyAttentionDecisionToElement(item, decision.Decision, decision.GroupPath, decision.TargetAssetID, decision.Rationale, decision.Confidence, attentionDecisionMetadata(req, decision)); err != nil {
			return nil, err
		}
		if err := s.repo.UpdateDeconstructionElement(item); err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	if err := s.refreshIntentInputManifest(orgID, sessionID, req.DriftControls); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) refreshIntentInputManifest(orgID, sessionID string, driftControls map[string]any) error {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return err
	}
	sources, err := s.repo.ListSourceReferences(orgID, sessionID)
	if err != nil {
		return err
	}
	elements, err := s.repo.ListDeconstructionElements(orgID, sessionID)
	if err != nil {
		return err
	}
	intent := decodeIntentSpec(session.IntentSpecJSON, session)
	intent.Source.SourceReferences = intentSourceReferencesFromSources(sources)
	if len(intent.Source.SourceReferences) > 0 {
		intent.Source.SourceKind = "dual_track_sources"
		intent.Source.SourceReferenceID = ""
		intent.Source.AssetID = ""
		intent.Source.AssetRelationID = ""
		intent.Source.SourceRef = ""
	}
	intent.Selections = intentSelectionsFromElements(elements)
	if intent.Requirements == nil {
		intent.Requirements = map[string]any{}
	}
	if driftControls != nil {
		intent.Requirements["attribute_drift"] = sanitizeGenerationManifestValue(driftControls)
	}
	if intent.Metadata == nil {
		intent.Metadata = map[string]any{}
	}
	inputManifest := buildIntentFusionInputManifest(intent.Source.SourceReferences, intent.Selections, elements)
	intent.Metadata["input_manifest"] = sanitizeGenerationManifestValue(inputManifest)
	applyIntentSpecDefaults(&intent, session)
	if err := validateIntentSpec(&intent); err != nil {
		return err
	}
	session.IntentSpecJSON = encodeIntentSpec(&intent)
	promptPlan := promptPlanFromIntentFusion(session, &intent, inputManifest)
	if err := validatePromptPlan(promptPlan); err != nil {
		return err
	}
	session.PromptPlanJSON = encodePromptPlan(promptPlan)
	return s.repo.SaveSession(session)
}

func intentSourceReferencesFromSources(sources []models.EcommerceVisualSourceReference) []IntentSourceReferenceDTO {
	out := make([]IntentSourceReferenceDTO, 0, len(sources))
	for i := range sources {
		if sources[i].Status != models.VisualSourceStatusReady || sources[i].ResolveStatus != models.VisualSourceStatusReady {
			continue
		}
		metadata := sanitizeDeconstructionRequestMetadata(decodeObject(sources[i].Metadata))
		role := sourceRoleFromMetadata(metadata, sources[i].SourceKind)
		if role != "sku" && role != "reference" {
			continue
		}
		out = append(out, IntentSourceReferenceDTO{
			SourceReferenceID: sources[i].ID,
			Role:              role,
			SourceKind:        sources[i].SourceKind,
			AssetID:           sources[i].AssetID,
			AssetRelationID:   sources[i].AssetRelationID,
			SourceRef:         sources[i].SourceRef,
			Metadata:          metadata,
		})
	}
	return out
}

func intentSelectionsFromElements(elements []models.EcommerceVisualDeconstructionElement) []IntentElementDTO {
	out := make([]IntentElementDTO, 0, len(elements))
	for i := range elements {
		metadata := sanitizeDeconstructionRequestMetadata(decodeObject(elements[i].Metadata))
		decision := metadataString(metadata, "decision")
		if decision == "" || decision == "needs_review" {
			continue
		}
		out = append(out, IntentElementDTO{
			ElementID:     elements[i].ID,
			ElementType:   elements[i].ElementType,
			Decision:      decision,
			GroupPath:     stringSliceFromAny(metadata["group_path"]),
			TargetAssetID: metadataString(metadata, "target_asset_id"),
			ElementKey:    elements[i].ElementKey,
			Label:         elements[i].Label,
			Value:         sanitizeDeconstructionRequestMetadata(decodeObject(elements[i].ValueJSON)),
			Metadata:      metadata,
		})
	}
	return out
}

func buildIntentFusionInputManifest(sources []IntentSourceReferenceDTO, selections []IntentElementDTO, elements []models.EcommerceVisualDeconstructionElement) map[string]any {
	skuFacts := make([]map[string]any, 0)
	referenceStrategies := make([]map[string]any, 0)
	for i := range elements {
		metadata := sanitizeDeconstructionRequestMetadata(decodeObject(elements[i].Metadata))
		role := metadataString(metadata, "source_role")
		entry := map[string]any{
			"element_id":          elements[i].ID,
			"element_type":        elements[i].ElementType,
			"element_key":         elements[i].ElementKey,
			"label":               elements[i].Label,
			"value":               sanitizeDeconstructionRequestMetadata(decodeObject(elements[i].ValueJSON)),
			"source_role":         role,
			"source_reference_id": metadataString(metadata, "source_reference_id"),
			"decision":            metadataString(metadata, "decision"),
		}
		switch role {
		case "sku":
			skuFacts = append(skuFacts, entry)
		case "reference":
			referenceStrategies = append(referenceStrategies, entry)
		}
	}
	ready := len(sources) >= 2 && len(skuFacts) > 0 && len(referenceStrategies) > 0 && len(selections) > 0
	readiness := "blocked"
	blockers := []ReadinessBlocker{}
	if ready {
		readiness = "ready"
	} else {
		if len(skuFacts) == 0 {
			blockers = append(blockers, ReadinessBlocker{Code: "SKU_FACTS_REQUIRED", Target: "intent_input", Message: "SKU fact elements are required before prompt planning."})
		}
		if len(referenceStrategies) == 0 {
			blockers = append(blockers, ReadinessBlocker{Code: "REFERENCE_STRATEGIES_REQUIRED", Target: "intent_input", Message: "Reference strategy elements are required before prompt planning."})
		}
		if len(selections) == 0 {
			blockers = append(blockers, ReadinessBlocker{Code: "ATTENTION_DECISION_REQUIRED", Target: "intent_input", Message: "Keep/Replace/Drop decisions are required before prompt planning."})
		}
	}
	return map[string]any{
		"schema_version":           "visual-intent-input.v1",
		"source_references":        sources,
		"selections":               selections,
		"selection_count":          len(selections),
		"sku_facts":                skuFacts,
		"sku_fact_count":           len(skuFacts),
		"reference_strategies":     referenceStrategies,
		"reference_strategy_count": len(referenceStrategies),
		"decision_tree":            decisionTreeProjection(selections),
		"readiness":                readiness,
		"blockers":                 blockers,
		"requires_prompt_diff":     true,
	}
}

func promptPlanFromIntentFusion(session *models.EcommerceVisualWorkflowSession, intent *IntentSpecDTO, manifest map[string]any) *PromptPlanDTO {
	status := "blocked"
	blockers := []ReadinessBlocker{}
	if strings.TrimSpace(fmt.Sprint(manifest["readiness"])) == "ready" {
		status = "ready"
	} else if rawBlockers, ok := manifest["blockers"].([]ReadinessBlocker); ok {
		blockers = rawBlockers
	} else {
		blockers = []ReadinessBlocker{{Code: "INTENT_INPUT_REQUIRED", Target: "prompt_plan", Message: "Backend intent fusion input is not ready."}}
	}
	plan := &PromptPlanDTO{
		SchemaVersion:     promptPlanSchemaVersion,
		Status:            status,
		SceneType:         intent.SceneType,
		TemplateID:        strings.TrimSpace(session.TemplateID),
		TemplateVersionID: strings.TrimSpace(session.TemplateVersionID),
		Variables: map[string]any{
			"intent_input_manifest": manifest,
			"attribute_drift":       intent.Requirements["attribute_drift"],
		},
		SourceAssets: promptPlanSourceAssetsFromIntentSources(intent.Source.SourceReferences),
		Blockers:     blockers,
		Metadata: map[string]any{
			"source":               "backend_intent_fusion",
			"requires_prompt_diff": true,
			"execution_contract":   "not_started",
		},
	}
	applyPromptPlanDefaults(plan, session)
	return plan
}

func promptPlanSourceAssetsFromIntentSources(sources []IntentSourceReferenceDTO) []PromptPlanSourceAssetDTO {
	out := make([]PromptPlanSourceAssetDTO, 0, len(sources))
	for _, source := range sources {
		out = append(out, PromptPlanSourceAssetDTO{
			AssetID:           source.AssetID,
			AssetRelationID:   source.AssetRelationID,
			SourceReferenceID: source.SourceReferenceID,
			Role:              source.Role,
			Metadata:          sanitizeDeconstructionRequestMetadata(source.Metadata),
		})
	}
	return out
}

func stringSliceFromAny(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func validateAttentionTreeRequest(req ApplyAttentionTreeRequest) error {
	requestLayer := -1
	if req.Layer != nil {
		if *req.Layer < 0 {
			return fmt.Errorf("attention tree layer must be >= 0")
		}
		requestLayer = *req.Layer
	}
	seenNodeIDs := map[string]bool{}
	for _, decision := range req.Decisions {
		layer := requestLayer
		if decision.Layer != nil {
			if *decision.Layer < 0 {
				return fmt.Errorf("attention tree layer must be >= 0")
			}
			layer = *decision.Layer
		}
		if layer == 0 && strings.TrimSpace(decision.ParentNodeID) != "" {
			return fmt.Errorf("root attention decision cannot have parent_node_id")
		}
		if strings.TrimSpace(decision.DecisionNodeID) != "" {
			if seenNodeIDs[strings.TrimSpace(decision.DecisionNodeID)] {
				return fmt.Errorf("duplicate decision_node_id: %s", decision.DecisionNodeID)
			}
			seenNodeIDs[strings.TrimSpace(decision.DecisionNodeID)] = true
		}
	}
	return nil
}

func attentionDecisionMetadata(req ApplyAttentionTreeRequest, decision AttentionDecisionInput) map[string]any {
	metadata := mergeObjectMaps(decision.Metadata)
	if strings.TrimSpace(req.TreeID) != "" {
		metadata["tree_id"] = strings.TrimSpace(req.TreeID)
	}
	roundID := strings.TrimSpace(decision.RoundID)
	if roundID == "" {
		roundID = strings.TrimSpace(req.RoundID)
	}
	if roundID != "" {
		metadata["round_id"] = roundID
	}
	if strings.TrimSpace(decision.DecisionNodeID) != "" {
		metadata["decision_node_id"] = strings.TrimSpace(decision.DecisionNodeID)
	}
	if strings.TrimSpace(decision.ParentNodeID) != "" {
		metadata["parent_node_id"] = strings.TrimSpace(decision.ParentNodeID)
	}
	if decision.Layer != nil {
		metadata["layer"] = *decision.Layer
	} else if req.Layer != nil {
		metadata["layer"] = *req.Layer
	}
	if len(decision.Path) > 0 {
		metadata["path"] = decision.Path
	}
	if strings.TrimSpace(decision.Question) != "" {
		metadata["question"] = strings.TrimSpace(decision.Question)
	}
	if strings.TrimSpace(decision.Answer) != "" {
		metadata["answer"] = strings.TrimSpace(decision.Answer)
	}
	return metadata
}

func decisionTreeProjection(selections []IntentElementDTO) map[string]any {
	nodes := make([]map[string]any, 0, len(selections))
	layers := map[int]bool{}
	rounds := map[string]bool{}
	for _, selection := range selections {
		metadata := selection.Metadata
		if metadata == nil {
			continue
		}
		nodeID := metadataString(metadata, "decision_node_id")
		if nodeID == "" {
			nodeID = selection.ElementID
		}
		layer := intFromAny(metadata["layer"], 0)
		roundID := metadataString(metadata, "round_id")
		if roundID != "" {
			rounds[roundID] = true
		}
		layers[layer] = true
		nodes = append(nodes, map[string]any{
			"node_id":         nodeID,
			"parent_node_id":  metadataString(metadata, "parent_node_id"),
			"round_id":        roundID,
			"layer":           layer,
			"path":            metadata["path"],
			"question":        metadataString(metadata, "question"),
			"answer":          metadataString(metadata, "answer"),
			"element_id":      selection.ElementID,
			"decision":        selection.Decision,
			"target_asset_id": selection.TargetAssetID,
			"group_path":      selection.GroupPath,
		})
	}
	layerList := make([]int, 0, len(layers))
	for layer := range layers {
		layerList = append(layerList, layer)
	}
	roundList := make([]string, 0, len(rounds))
	for roundID := range rounds {
		roundList = append(roundList, roundID)
	}
	return map[string]any{"schema_version": "visual-decision-tree.v1", "rounds": roundList, "layers": layerList, "nodes": nodes}
}

func applyAttentionDecisionToElement(item *models.EcommerceVisualDeconstructionElement, decision string, groupPath []string, targetAssetID, rationale string, confidence *float64, extra map[string]any) error {
	metadata := decodeObject(item.Metadata)
	if extra != nil {
		metadata = mergeObjectMaps(metadata, sanitizeGenerationManifestValue(extra).(map[string]any))
	}
	decision = strings.TrimSpace(decision)
	if decision != "" {
		if !validAttentionDecision(decision) {
			return fmt.Errorf("invalid attention decision: %s", decision)
		}
		metadata["decision"] = decision
	}
	if len(groupPath) > 0 {
		metadata["group_path"] = groupPath
	}
	if strings.TrimSpace(targetAssetID) != "" {
		metadata["target_asset_id"] = strings.TrimSpace(targetAssetID)
	}
	if strings.TrimSpace(rationale) != "" {
		metadata["rationale"] = strings.TrimSpace(rationale)
	}
	if confidence != nil {
		if *confidence < 0 || *confidence > 1 {
			return fmt.Errorf("attention confidence must be 0..1")
		}
		metadata["confidence"] = *confidence
	}
	item.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	return nil
}

func validAttentionDecision(v string) bool {
	switch strings.TrimSpace(v) {
	case "keep", "replace", "drop", "needs_review":
		return true
	default:
		return false
	}
}

func (s *Service) ConfirmSelection(orgID, sessionID string, elementIDs []string) ([]models.EcommerceVisualDeconstructionElement, error) {
	wanted := map[string]bool{}
	for _, id := range elementIDs {
		wanted[strings.TrimSpace(id)] = true
	}
	items, err := s.repo.ListDeconstructionElements(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]models.EcommerceVisualDeconstructionElement, 0, len(items))
	for i := range items {
		if wanted[items[i].ID] {
			items[i].Selected = true
			items[i].Confirmed = true
			items[i].Readiness = models.VisualReadinessReady
			if err := s.repo.UpdateDeconstructionElement(&items[i]); err != nil {
				return nil, err
			}
			out = append(out, items[i])
		}
	}
	return out, nil
}

func (s *Service) StageView(orgID, sessionID string) (*StageViewDTO, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.buildStageView(session)
}

func (s *Service) CreateGenerationVersion(orgID, sessionID string, req CreateGenerationVersionRequest) (*GenerationVersionDTO, error) {
	if err := rejectCreateGenerationVersionJobReferences(&req); err != nil {
		return nil, err
	}
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey != "" {
		for i := range versions {
			if metadataString(versions[i].Metadata, "idempotency_key") == idempotencyKey {
				return &versions[i], nil
			}
		}
	}
	promptPlan := decodePromptPlan(session.PromptPlanJSON, session)
	intentSpec := decodeIntentSpec(session.IntentSpecJSON, session)
	promptID := strings.TrimSpace(req.PromptID)
	if promptID == "" {
		promptID = strings.TrimSpace(promptPlan.PromptID)
	} else if strings.TrimSpace(promptPlan.PromptID) != "" && promptID != strings.TrimSpace(promptPlan.PromptID) {
		return nil, fmt.Errorf("prompt_id must match current prompt_plan.prompt_id")
	}
	progress := 0
	if req.Progress != nil {
		progress = *req.Progress
	}
	version := GenerationVersionDTO{
		VersionID:             buildID("gv"),
		ParentVersionID:       strings.TrimSpace(req.ParentVersionID),
		SourceVersionID:       strings.TrimSpace(req.SourceVersionID),
		PromptID:              promptID,
		PromptPlanStatus:      strings.TrimSpace(promptPlan.Status),
		IntentSpecSnapshot:    intentSnapshotMetadata(intentSpec),
		RefinementInstruction: strings.TrimSpace(req.RefinementInstruction),
		MaskAssetID:           strings.TrimSpace(req.MaskAssetID),
		RuntimeJobID:          strings.TrimSpace(req.RuntimeJobID),
		ImageJobID:            strings.TrimSpace(req.ImageJobID),
		SelectedResultAssetID: strings.TrimSpace(req.SelectedResultAssetID),
		Status:                strings.TrimSpace(req.Status),
		Stage:                 strings.TrimSpace(req.Stage),
		Progress:              progress,
		ResultAssets:          req.ResultAssets,
		Blockers:              req.Blockers,
		CreatedAt:             time.Now().UTC().Format(time.RFC3339Nano),
		UpdatedAt:             time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:              req.Metadata,
	}
	if version.Status == "" {
		version.Status = "contract_needed"
	}
	if version.Stage == "" {
		version.Stage = "contract_needed"
	}
	if version.Metadata == nil {
		version.Metadata = map[string]any{}
	}
	if idempotencyKey != "" {
		version.Metadata["idempotency_key"] = idempotencyKey
	}
	if err := validateGenerationVersion(&version); err != nil {
		return nil, err
	}
	if err := s.prepareGenerationRuntimeVersion(session, &version, &promptPlan, &intentSpec); err != nil {
		return nil, err
	}
	if err := validateGenerationVersion(&version); err != nil {
		return nil, err
	}
	versions = append(versions, version)
	encoded, err := marshalGenerationVersions(versions)
	if err != nil {
		return nil, err
	}
	session.GenerationVersionsJSON = encoded
	session.CurrentStage = models.VisualWorkflowStageGeneration
	if session.Status == models.VisualWorkflowStatusDraft || session.Status == models.VisualWorkflowStatusReady {
		session.Status = models.VisualWorkflowStatusBlocked
	}
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &version, nil
}

func (s *Service) CreateGenerationFanout(orgID, sessionID string, req CreateGenerationFanoutRequest) (*CreateGenerationFanoutResponse, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	type fanoutPair struct {
		sourceID            string
		templateID          string
		templateVersionID   string
		sceneTag            string
		detailRequirement   string
		negativeRequirement string
	}
	pairs := make([]fanoutPair, 0)
	if len(req.TemplateSlots) > 0 {
		for _, slot := range req.TemplateSlots {
			sourceID := strings.TrimSpace(slot.SourceAssetID)
			templateID := strings.TrimSpace(slot.TemplateID)
			if sourceID == "" || templateID == "" {
				continue
			}
			pairs = append(pairs, fanoutPair{
				sourceID:            sourceID,
				templateID:          templateID,
				templateVersionID:   strings.TrimSpace(slot.TemplateVersionID),
				sceneTag:            strings.TrimSpace(slot.SceneTag),
				detailRequirement:   strings.TrimSpace(slot.DetailRequirement),
				negativeRequirement: strings.TrimSpace(slot.NegativeRequirement),
			})
		}
	} else {
		sourceIDs := compactUniqueStrings(req.SourceAssetIDs)
		templateIDs := compactUniqueStrings(req.TemplateIDs)
		for _, sourceID := range sourceIDs {
			for templateIndex, templateID := range templateIDs {
				templateVersionID := ""
				if templateIndex < len(req.TemplateVersionIDs) {
					templateVersionID = strings.TrimSpace(req.TemplateVersionIDs[templateIndex])
				}
				pairs = append(pairs, fanoutPair{sourceID: sourceID, templateID: templateID, templateVersionID: templateVersionID})
			}
		}
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("at least one source/template fan-out task is required")
	}
	total := len(pairs)
	if total > MaxGenerationFanoutTasks {
		return nil, fmt.Errorf("generation fan-out task count %d exceeds limit %d", total, MaxGenerationFanoutTasks)
	}
	for _, sourceID := range compactUniqueStrings(func() []string {
		ids := make([]string, 0, len(pairs))
		for _, pair := range pairs {
			ids = append(ids, pair.sourceID)
		}
		return ids
	}()) {
		if err := s.validateFanoutSourceAsset(session, sourceID); err != nil {
			return nil, err
		}
	}
	fanoutID := strings.TrimSpace(req.IdempotencyKey)
	if fanoutID == "" {
		fanoutID = buildID("gfb")
	}
	executionConfig := GenerationFanoutExecutionConfigDTO{
		MaxConcurrency: clampInt(req.MaxConcurrency, 1, MaxGenerationFanoutTasks),
		RetryOnFailure: req.RetryOnFailure,
		MaxRetries:     clampInt(req.MaxRetries, 0, 5),
		TimeoutSeconds: clampInt(req.TimeoutSeconds, 30, 1800),
	}
	promptPlan := decodePromptPlan(session.PromptPlanJSON, session)
	if strings.TrimSpace(promptPlan.PromptID) == "" || strings.TrimSpace(promptPlan.Status) != "ready" {
		return nil, fmt.Errorf("ready prompt_plan with prompt_id is required before generation fan-out")
	}
	items := make([]GenerationFanoutItemDTO, 0, total)
	for slotIndex, pair := range pairs {
		sourceID := pair.sourceID
		templateID := pair.templateID
		templateVersionID := pair.templateVersionID
		fanoutTaskID := fmt.Sprintf("%s:%02d", fanoutID, slotIndex+1)
		providerConfig := sanitizeGenerationManifestValue(req.ProviderConfig)
		metadata := mergeObjectMaps(req.Metadata, map[string]any{
			"source":               "sandbox_generation_fanout",
			"idempotency_key":      fmt.Sprintf("generation-fanout:%s:%s:%02d:%s:%s", session.ID, fanoutID, slotIndex+1, sourceID, templateID),
			"fanout_id":            fanoutID,
			"fanout_task_id":       fanoutTaskID,
			"fanout_index":         slotIndex,
			"fanout_total":         total,
			"source_asset_id":      sourceID,
			"template_id":          strings.TrimSpace(templateID),
			"template_version_id":  templateVersionID,
			"scene_tag":            pair.sceneTag,
			"detail_requirement":   pair.detailRequirement,
			"negative_requirement": pair.negativeRequirement,
			"requested_variants":   clampInt(req.RequestedVariants, 1, 4),
			"execution_config": map[string]any{
				"max_concurrency":  executionConfig.MaxConcurrency,
				"retry_on_failure": executionConfig.RetryOnFailure,
				"max_retries":      executionConfig.MaxRetries,
				"timeout_seconds":  executionConfig.TimeoutSeconds,
				"provider_config":  providerConfig,
			},
		})
		if len(req.PromptVariables) > 0 {
			metadata["prompt_variables"] = sanitizeGenerationManifestValue(req.PromptVariables)
		}
		version, err := s.CreateGenerationVersion(orgID, sessionID, CreateGenerationVersionRequest{
			PromptID:       promptPlan.PromptID,
			Status:         "queued",
			Stage:          "queued",
			Progress:       intPtr(0),
			IdempotencyKey: metadataString(metadata, "idempotency_key"),
			Metadata:       metadata,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, GenerationFanoutItemDTO{FanoutTaskID: fanoutTaskID, SourceAssetID: sourceID, TemplateID: strings.TrimSpace(templateID), TemplateVersionID: templateVersionID, SlotIndex: slotIndex, SceneTag: pair.sceneTag, DetailRequirement: pair.detailRequirement, GenerationVersion: *version})
	}
	return &CreateGenerationFanoutResponse{
		SessionID:       session.ID,
		ProductID:       session.ProductID,
		SKUCode:         session.SKUCode,
		FanoutID:        fanoutID,
		Status:          "queued",
		ExecutionConfig: executionConfig,
		Summary:         GenerationFanoutSummaryDTO{TotalTasks: len(items), QueuedTasks: len(items)},
		Items:           items,
	}, nil
}

func (s *Service) ListGenerationVersions(orgID, sessionID string) ([]GenerationVersionDTO, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	return decodeArray(session.GenerationVersionsJSON), nil
}

func (s *Service) GetGenerationVersion(orgID, sessionID, versionID string) (*GenerationVersionDTO, error) {
	versions, err := s.ListGenerationVersions(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	for i := range versions {
		if versions[i].VersionID == versionID {
			return &versions[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *Service) UpdateGenerationVersion(orgID, sessionID, versionID string, req UpdateGenerationVersionRequest) (*GenerationVersionDTO, error) {
	if err := rejectUpdateGenerationVersionJobReferences(&req); err != nil {
		return nil, err
	}
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	idx := -1
	for i := range versions {
		if versions[i].VersionID == versionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	version := versions[idx]
	if req.Status != nil {
		version.Status = strings.TrimSpace(*req.Status)
	}
	if req.Stage != nil {
		version.Stage = strings.TrimSpace(*req.Stage)
	}
	if req.Progress != nil {
		version.Progress = *req.Progress
	}
	if req.RuntimeJobID != nil {
		version.RuntimeJobID = strings.TrimSpace(*req.RuntimeJobID)
	}
	if req.ImageJobID != nil {
		version.ImageJobID = strings.TrimSpace(*req.ImageJobID)
	}
	if req.ResultAssets != nil {
		version.ResultAssets = req.ResultAssets
	}
	if req.SelectedResultAssetID != nil {
		version.SelectedResultAssetID = strings.TrimSpace(*req.SelectedResultAssetID)
	}
	if req.RefinementInstruction != nil {
		version.RefinementInstruction = strings.TrimSpace(*req.RefinementInstruction)
	}
	if req.MaskAssetID != nil {
		version.MaskAssetID = strings.TrimSpace(*req.MaskAssetID)
	}
	if req.SourceVersionID != nil {
		version.SourceVersionID = strings.TrimSpace(*req.SourceVersionID)
	}
	if req.ParentVersionID != nil {
		version.ParentVersionID = strings.TrimSpace(*req.ParentVersionID)
	}
	if req.Blockers != nil {
		version.Blockers = req.Blockers
	}
	if req.Metadata != nil {
		version.Metadata = req.Metadata
	}
	version.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := validateGenerationVersion(&version); err != nil {
		return nil, err
	}
	versions[idx] = version
	encoded, err := marshalGenerationVersions(versions)
	if err != nil {
		return nil, err
	}
	session.GenerationVersionsJSON = encoded
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &version, nil
}

func (s *Service) SelectGenerationVersion(orgID, sessionID, versionID string, req SelectGenerationVersionRequest) (*GenerationVersionDTO, error) {
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	idx := -1
	for i := range versions {
		if versions[i].VersionID == versionID {
			idx = i
		}
		if versions[i].Metadata == nil {
			versions[i].Metadata = map[string]any{}
		}
		delete(versions[i].Metadata, "selected")
		for j := range versions[i].ResultAssets {
			versions[i].ResultAssets[j].Selected = false
		}
	}
	if idx < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	selectedAssetID := strings.TrimSpace(req.SelectedResultAssetID)
	if selectedAssetID != "" {
		found := false
		for j := range versions[idx].ResultAssets {
			if versions[idx].ResultAssets[j].AssetID == selectedAssetID {
				versions[idx].ResultAssets[j].Selected = true
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("selected_result_asset_id is not in this generation version")
		}
	}
	versions[idx].SelectedResultAssetID = selectedAssetID
	versions[idx].Stage = "selected"
	versions[idx].Metadata["selected"] = true
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			versions[idx].Metadata[k] = v
		}
	}
	versions[idx].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := validateGenerationVersion(&versions[idx]); err != nil {
		return nil, err
	}
	encoded, err := marshalGenerationVersions(versions)
	if err != nil {
		return nil, err
	}
	session.GenerationVersionsJSON = encoded
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &versions[idx], nil
}

func (s *Service) WritebackSelectedGenerationAsset(userID, orgID, sessionID, versionID string, req WritebackSelectedGenerationAssetRequest) (*WritebackSelectedGenerationAssetResponse, error) {
	if err := validateNoWritebackMetadataArtifacts(req.Metadata); err != nil {
		return nil, err
	}
	role := strings.TrimSpace(req.AssetRole)
	if role == "" {
		role = models.AssetRoleHero
	}
	if !validWritebackAssetRole(role) {
		return nil, fmt.Errorf("invalid asset_role: %s", role)
	}
	isPrimary := false
	if req.IsPrimary != nil {
		isPrimary = *req.IsPrimary
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)

	var out *WritebackSelectedGenerationAssetResponse
	err := s.repo.WithTransaction(func(tx *gorm.DB) error {
		vwRepo := repository.NewVisualWorkflowRepository(tx)
		productRepo := repository.NewProductCenterRepository(tx)
		assetRepo := repository.NewImageRuntimeRepository(tx)
		scope := repository.Scope{OrgID: orgID, UserID: userID}

		session, err := vwRepo.GetSession(orgID, sessionID)
		if err != nil {
			return fmt.Errorf("session not found in organization: %w", err)
		}
		product, err := productRepo.GetProduct(scope, session.ProductID)
		if err != nil {
			return fmt.Errorf("product not found in organization: %w", err)
		}
		if strings.TrimSpace(product.SKUCode) != strings.TrimSpace(session.SKUCode) {
			return fmt.Errorf("sku_code does not match product")
		}

		versions := decodeArray(session.GenerationVersionsJSON)
		idx := -1
		for i := range versions {
			if versions[i].VersionID == versionID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("generation version not found in session")
		}
		version := versions[idx]
		selectedAssetID := strings.TrimSpace(req.AssetID)
		if selectedAssetID == "" {
			selectedAssetID = strings.TrimSpace(version.SelectedResultAssetID)
		}
		if selectedAssetID == "" {
			return fmt.Errorf("selected result asset is required")
		}
		found := false
		for i := range version.ResultAssets {
			if strings.TrimSpace(version.ResultAssets[i].AssetID) == selectedAssetID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("asset_id is not in this generation version result_assets")
		}
		if _, err := assetRepo.FindAssetByID(orgID, selectedAssetID); err != nil {
			return fmt.Errorf("selected asset is not an Ecommerce asset in this organization: %w", err)
		}

		now := time.Now().UTC().Format(time.RFC3339)
		writebackMetadata := map[string]any{
			"origin":                     "visual_workflow_selected_generation_asset_writeback",
			"visual_workflow_session_id": session.ID,
			"generation_version_id":      version.VersionID,
			"selected_result_asset_id":   selectedAssetID,
			"writeback_at":               now,
			"writeback_by":               userID,
		}
		if strings.TrimSpace(version.SourceVersionID) != "" {
			writebackMetadata["source_version_id"] = strings.TrimSpace(version.SourceVersionID)
		}
		if strings.TrimSpace(version.ParentVersionID) != "" {
			writebackMetadata["parent_version_id"] = strings.TrimSpace(version.ParentVersionID)
		}
		if idempotencyKey != "" {
			writebackMetadata["idempotency_key"] = idempotencyKey
		}
		for k, v := range req.Metadata {
			writebackMetadata[k] = v
		}
		if err := validateNoWritebackMetadataArtifacts(writebackMetadata); err != nil {
			return err
		}

		relation, findErr := productRepo.FindProductAssetRelation(scope, session.ProductID, selectedAssetID)
		idempotent := false
		if findErr == nil {
			currentMetadata := sanitizeWritebackRelationMetadata(decodeObject(relation.Metadata))
			if (idempotencyKey != "" && metadataString(currentMetadata, "idempotency_key") == idempotencyKey) || idempotencyKey == "" {
				idempotent = true
			}
			for k, v := range writebackMetadata {
				currentMetadata[k] = v
			}
			relation.RelationType = models.AssetRelationTypeResult
			relation.AssetRole = role
			relation.IsPrimary = isPrimary
			if strings.TrimSpace(relation.Visibility) == "" {
				relation.Visibility = "library"
			}
			relation.Metadata = mustJSON(currentMetadata)
			relation, err = productRepo.UpdateProductAssetRelation(scope, *relation)
			if err != nil {
				return err
			}
		} else if findErr == gorm.ErrRecordNotFound {
			relation, err = productRepo.AddProductAsset(scope, models.EcomAssetRelation{
				ID:           buildID("rel"),
				AssetID:      selectedAssetID,
				OwnerType:    models.AssetRelationOwnerTypeProduct,
				OwnerID:      session.ProductID,
				RelationType: models.AssetRelationTypeResult,
				AssetRole:    role,
				IsPrimary:    isPrimary,
				Visibility:   "library",
				Metadata:     mustJSON(writebackMetadata),
			})
			if err != nil {
				return err
			}
		} else {
			return findErr
		}
		if isPrimary {
			if err := productRepo.ClearPrimaryProductAssets(scope, session.ProductID, relation.ID); err != nil {
				return err
			}
			relation.IsPrimary = true
			relation, err = productRepo.UpdateProductAssetRelation(scope, *relation)
			if err != nil {
				return err
			}
		}

		if version.Metadata == nil {
			version.Metadata = map[string]any{}
		}
		version.Metadata["writeback"] = map[string]any{
			"status":            "succeeded",
			"asset_relation_id": relation.ID,
			"asset_id":          selectedAssetID,
			"asset_role":        role,
			"is_primary":        isPrimary,
			"writeback_at":      now,
		}
		if idempotencyKey != "" {
			version.Metadata["writeback"].(map[string]any)["idempotency_key"] = idempotencyKey
		}
		version.SelectedResultAssetID = selectedAssetID
		version.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := validateGenerationVersion(&version); err != nil {
			return err
		}
		versions[idx] = version
		encoded, err := marshalGenerationVersions(versions)
		if err != nil {
			return err
		}
		session.GenerationVersionsJSON = encoded
		if err := vwRepo.SaveSession(session); err != nil {
			return err
		}

		out = &WritebackSelectedGenerationAssetResponse{
			SessionID:             session.ID,
			VersionID:             version.VersionID,
			ProductID:             session.ProductID,
			SKUCode:               session.SKUCode,
			SelectedResultAssetID: selectedAssetID,
			AssetRelation:         assetRelationDTO(*relation),
			Idempotent:            idempotent,
			GenerationVersion:     version,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) requireBoundProduct(orgID, productID, skuCode string) (*models.EcomProductSKU, error) {
	product, err := s.productRepo.GetProduct(repository.Scope{OrgID: orgID}, strings.TrimSpace(productID))
	if err != nil {
		return nil, fmt.Errorf("product not found: %w", err)
	}
	if strings.TrimSpace(product.SKUCode) != strings.TrimSpace(skuCode) {
		return nil, fmt.Errorf("sku_code does not match product")
	}
	return product, nil
}

func shortStableHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func buildID(prefix string) string { return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()) }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func decodeObject(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func decodeArray(raw string) []GenerationVersionDTO {
	out := []GenerationVersionDTO{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	type storageGenerationVersion GenerationVersionDTO
	var stored []storageGenerationVersion
	_ = json.Unmarshal([]byte(raw), &stored)
	for _, item := range stored {
		out = append(out, GenerationVersionDTO(item))
	}
	if out == nil {
		return []GenerationVersionDTO{}
	}
	return out
}

func marshalGenerationVersions(versions []GenerationVersionDTO) (string, error) {
	for i := range versions {
		if err := validateGenerationVersion(&versions[i]); err != nil {
			return "", err
		}
	}
	b, err := json.Marshal(versions)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

const (
	intentSpecSchemaVersion = "visual_intent_spec.v1"
	promptPlanSchemaVersion = "visual_prompt_plan.v1"
)

func itemFromCreateRequest(product *models.EcomProductSKU, req CreateSessionRequest) *models.EcommerceVisualWorkflowSession {
	return &models.EcommerceVisualWorkflowSession{
		ProductID:         product.ID,
		SKUCode:           product.SKUCode,
		ToolSlug:          strings.TrimSpace(req.ToolSlug),
		TemplateID:        strings.TrimSpace(req.TemplateID),
		TemplateVersionID: strings.TrimSpace(req.TemplateVersionID),
	}
}

func defaultIntentSpec(session *models.EcommerceVisualWorkflowSession) *IntentSpecDTO {
	return &IntentSpecDTO{
		SchemaVersion: intentSpecSchemaVersion,
		ToolSlug:      strings.TrimSpace(session.ToolSlug),
		ProductID:     strings.TrimSpace(session.ProductID),
		SKUCode:       strings.TrimSpace(session.SKUCode),
		Requirements:  map[string]any{},
		Metadata:      map[string]any{},
	}
}

func defaultPromptPlan(session *models.EcommerceVisualWorkflowSession) *PromptPlanDTO {
	return &PromptPlanDTO{
		SchemaVersion:     promptPlanSchemaVersion,
		Status:            "contract_needed",
		TemplateID:        strings.TrimSpace(session.TemplateID),
		TemplateVersionID: strings.TrimSpace(session.TemplateVersionID),
		Variables:         map[string]any{},
		SourceAssets:      []PromptPlanSourceAssetDTO{},
		Blockers: []ReadinessBlocker{{
			Code:    "CONTRACT_NEEDED",
			Target:  "prompt_plan",
			Message: "Prompt execution/preview contract is not finalized; no provider or fake Prompt Center execution was called.",
		}},
		Metadata: map[string]any{},
	}
}

func applyIntentSpecDefaults(spec *IntentSpecDTO, session *models.EcommerceVisualWorkflowSession) {
	if strings.TrimSpace(spec.SchemaVersion) == "" {
		spec.SchemaVersion = intentSpecSchemaVersion
	}
	if strings.TrimSpace(spec.ProductID) == "" {
		spec.ProductID = strings.TrimSpace(session.ProductID)
	}
	if strings.TrimSpace(spec.SKUCode) == "" {
		spec.SKUCode = strings.TrimSpace(session.SKUCode)
	}
	if strings.TrimSpace(spec.ToolSlug) == "" {
		spec.ToolSlug = strings.TrimSpace(session.ToolSlug)
	}
	if spec.Requirements == nil {
		spec.Requirements = map[string]any{}
	}
	if spec.Metadata == nil {
		spec.Metadata = map[string]any{}
	}
}

func applyPromptPlanDefaults(plan *PromptPlanDTO, session *models.EcommerceVisualWorkflowSession) {
	if strings.TrimSpace(plan.SchemaVersion) == "" {
		plan.SchemaVersion = promptPlanSchemaVersion
	}
	if strings.TrimSpace(plan.Status) == "" {
		plan.Status = "contract_needed"
	}
	if strings.TrimSpace(plan.TemplateID) == "" {
		plan.TemplateID = strings.TrimSpace(session.TemplateID)
	}
	if strings.TrimSpace(plan.TemplateVersionID) == "" {
		plan.TemplateVersionID = strings.TrimSpace(session.TemplateVersionID)
	}
	if plan.Variables == nil {
		plan.Variables = map[string]any{}
	}
	if plan.SourceAssets == nil {
		plan.SourceAssets = []PromptPlanSourceAssetDTO{}
	}
	if plan.Metadata == nil {
		plan.Metadata = map[string]any{}
	}
	if plan.Status == "contract_needed" && !containsReadinessBlocker(plan.Blockers, "CONTRACT_NEEDED") {
		plan.Blockers = append(plan.Blockers, ReadinessBlocker{Code: "CONTRACT_NEEDED", Target: "prompt_plan", Message: "Prompt execution/preview contract is not finalized; no provider or fake Prompt Center execution was called."})
	}
}

func validateIntentSpec(spec *IntentSpecDTO) error {
	if strings.TrimSpace(spec.SchemaVersion) == "" {
		return fmt.Errorf("intent_spec.schema_version is required")
	}
	return nil
}

func validatePromptPlan(plan *PromptPlanDTO) error {
	if strings.TrimSpace(plan.SchemaVersion) == "" {
		return fmt.Errorf("prompt_plan.schema_version is required")
	}
	if !validPromptPlanStatus(strings.TrimSpace(plan.Status)) {
		return fmt.Errorf("invalid prompt_plan.status: %s", plan.Status)
	}
	if err := validatePromptPlanForbiddenKeys(plan.Variables); err != nil {
		return err
	}
	if err := validatePromptPlanForbiddenKeys(plan.Metadata); err != nil {
		return err
	}
	for _, asset := range plan.SourceAssets {
		if err := validatePromptPlanForbiddenKeys(asset.Metadata); err != nil {
			return err
		}
	}
	return nil
}

func validateClientPromptPlan(plan *PromptPlanDTO) error {
	if err := validatePromptPlan(plan); err != nil {
		return err
	}
	source := metadataString(plan.Metadata, "source")
	if source == "backend_intent_fusion" || source == "llm_prompt_planner" {
		return fmt.Errorf("prompt_plan.metadata.source %q is server-owned and cannot be supplied by clients", source)
	}
	if key, ok := findForbiddenClientPromptEvidenceKey(plan.Metadata); ok {
		return fmt.Errorf("prompt_plan.metadata.%s is server-owned and cannot be supplied by clients", key)
	}
	return nil
}

func encodeIntentSpec(spec *IntentSpecDTO) string {
	if spec == nil {
		return mustJSON(defaultIntentSpec(&models.EcommerceVisualWorkflowSession{}))
	}
	return mustJSON(spec)
}

func decodeIntentSpec(raw string, session *models.EcommerceVisualWorkflowSession) IntentSpecDTO {
	out := *defaultIntentSpec(session)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	applyIntentSpecDefaults(&out, session)
	return out
}

func encodePromptPlan(plan *PromptPlanDTO) string {
	if plan == nil {
		return mustJSON(defaultPromptPlan(&models.EcommerceVisualWorkflowSession{}))
	}
	return mustJSON(plan)
}

func decodePromptPlan(raw string, session *models.EcommerceVisualWorkflowSession) PromptPlanDTO {
	out := *defaultPromptPlan(session)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	applyPromptPlanDefaults(&out, session)
	return out
}

func promptDiffNeedsFallback(raw any) bool {
	m, ok := raw.(map[string]any)
	if !ok {
		return true
	}
	for _, key := range []string{"added", "removed", "changed"} {
		if values, ok := m[key].([]any); ok && len(values) > 0 {
			return false
		}
		if values, ok := m[key].([]string); ok && len(values) > 0 {
			return false
		}
	}
	return true
}

func buildPromptPlanDiff(before, after *PromptPlanDTO) map[string]any {
	added := []any{}
	removed := []any{}
	changed := []any{}
	if before == nil || after == nil {
		return map[string]any{"added": added, "removed": removed, "changed": changed, "status": "fallback_unavailable"}
	}
	if strings.TrimSpace(before.PromptID) == "" && strings.TrimSpace(after.PromptID) != "" {
		added = append(added, fmt.Sprintf("prompt_id: %s", after.PromptID))
	} else if before.PromptID != after.PromptID {
		changed = append(changed, map[string]any{"field": "prompt_id", "from": before.PromptID, "to": after.PromptID})
	}
	if before.Status != after.Status {
		changed = append(changed, map[string]any{"field": "status", "from": before.Status, "to": after.Status})
	}
	if before.SceneType != after.SceneType {
		changed = append(changed, map[string]any{"field": "scene_type", "from": before.SceneType, "to": after.SceneType})
	}
	for key, afterValue := range after.Variables {
		beforeValue, existed := before.Variables[key]
		if !existed {
			added = append(added, fmt.Sprintf("variables.%s", key))
			continue
		}
		if mustJSON(beforeValue) != mustJSON(afterValue) {
			changed = append(changed, map[string]any{"field": "variables." + key, "from": beforeValue, "to": afterValue})
		}
	}
	for key := range before.Variables {
		if _, exists := after.Variables[key]; !exists {
			removed = append(removed, fmt.Sprintf("variables.%s", key))
		}
	}
	if len(before.SourceAssets) != len(after.SourceAssets) {
		changed = append(changed, map[string]any{"field": "source_assets.count", "from": len(before.SourceAssets), "to": len(after.SourceAssets)})
	}
	status := "generated"
	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		status = "no_material_change"
	}
	return map[string]any{"added": added, "removed": removed, "changed": changed, "status": status, "source": "backend_fallback"}
}

func validPromptPlanStatus(v string) bool {
	switch v {
	case "draft", "needs_review", "contract_needed", "blocked", "ready":
		return true
	default:
		return false
	}
}

func containsReadinessBlocker(blockers []ReadinessBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func validateGenerationVersion(version *GenerationVersionDTO) error {
	if strings.TrimSpace(version.VersionID) == "" {
		return fmt.Errorf("version_id is required")
	}
	version.Status = strings.TrimSpace(version.Status)
	version.Stage = strings.TrimSpace(version.Stage)
	if version.Status == "" {
		return fmt.Errorf("generation version status is required")
	}
	if !validGenerationVersionStatus(version.Status) {
		return fmt.Errorf("invalid generation version status: %s", version.Status)
	}
	if version.Stage != "" && !validGenerationVersionStage(version.Stage) {
		return fmt.Errorf("invalid generation version stage: %s", version.Stage)
	}
	if version.Progress < 0 || version.Progress > 100 {
		return fmt.Errorf("generation version progress must be 0..100")
	}
	if version.Status == "contract_needed" && !containsReadinessBlocker(version.Blockers, "CONTRACT_NEEDED") {
		version.Blockers = append(version.Blockers, ReadinessBlocker{Code: "CONTRACT_NEEDED", Target: "generation", Message: "Generation execution contract is not finalized; no provider call was made."})
	}
	if version.Status == "completed" && len(version.ResultAssets) == 0 && strings.TrimSpace(version.RuntimeJobID) == "" && strings.TrimSpace(version.ImageJobID) == "" {
		return fmt.Errorf("completed generation version requires result_assets or a real runtime/image job reference")
	}
	if version.SelectedResultAssetID != "" {
		found := false
		for _, asset := range version.ResultAssets {
			if asset.AssetID == version.SelectedResultAssetID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("selected_result_asset_id is not in this generation version")
		}
	}
	if err := validateNoExecutionArtifacts(version.Metadata); err != nil {
		return err
	}
	if err := validateNoExecutionArtifacts(version.IntentSpecSnapshot); err != nil {
		return err
	}
	for _, asset := range version.ResultAssets {
		if strings.TrimSpace(asset.AssetID) == "" {
			return fmt.Errorf("result_assets.asset_id is required")
		}
		if err := validateNoExecutionArtifacts(asset.Metadata); err != nil {
			return err
		}
	}
	return nil
}

func rejectCreateGenerationVersionJobReferences(req *CreateGenerationVersionRequest) error {
	if strings.TrimSpace(req.RuntimeJobID) != "" {
		return fmt.Errorf("runtime_job_id is not client-writeable in this tranche")
	}
	if strings.TrimSpace(req.ImageJobID) != "" {
		return fmt.Errorf("image_job_id is not client-writeable in this tranche")
	}
	return nil
}

func rejectUpdateGenerationVersionJobReferences(req *UpdateGenerationVersionRequest) error {
	if req.RuntimeJobID != nil && strings.TrimSpace(*req.RuntimeJobID) != "" {
		return fmt.Errorf("runtime_job_id is not client-writeable in this tranche")
	}
	if req.ImageJobID != nil && strings.TrimSpace(*req.ImageJobID) != "" {
		return fmt.Errorf("image_job_id is not client-writeable in this tranche")
	}
	return nil
}

func rejectClientGenerationVersionJobReferences(version *GenerationVersionDTO) error {
	if strings.TrimSpace(version.RuntimeJobID) != "" {
		return fmt.Errorf("runtime_job_id is not client-writeable in this tranche")
	}
	if strings.TrimSpace(version.ImageJobID) != "" {
		return fmt.Errorf("image_job_id is not client-writeable in this tranche")
	}
	return nil
}

func validGenerationVersionStatus(v string) bool {
	switch v {
	case "draft", "queued", "processing", "completed", "failed", "canceled", "blocked", "contract_needed":
		return true
	default:
		return false
	}
}

func validGenerationVersionStage(v string) bool {
	switch v {
	case "created", "prompt_snapshot", "contract_needed", "queued", "running", "result_available", "selected", "failed", "canceled":
		return true
	default:
		return false
	}
}

func validWritebackAssetRole(v string) bool {
	switch strings.TrimSpace(v) {
	case models.AssetRoleHero, models.AssetRoleModelShot, models.AssetRoleSceneShot, models.AssetRoleDetailShot, models.AssetRoleListingAttach:
		return true
	default:
		return false
	}
}

func intentSnapshotMetadata(spec IntentSpecDTO) map[string]any {
	out := map[string]any{
		"schema_version": strings.TrimSpace(spec.SchemaVersion),
		"product_id":     strings.TrimSpace(spec.ProductID),
		"sku_code":       strings.TrimSpace(spec.SKUCode),
	}
	if strings.TrimSpace(spec.SceneType) != "" {
		out["scene_type"] = strings.TrimSpace(spec.SceneType)
	}
	if strings.TrimSpace(spec.ToolSlug) != "" {
		out["tool_slug"] = strings.TrimSpace(spec.ToolSlug)
	}
	if len(spec.Selections) > 0 {
		out["selection_count"] = len(spec.Selections)
	}
	return out
}

func compactUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func intPtr(v int) *int { return &v }

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func intFromAny(raw any, fallback int) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func versionMetadata(version *GenerationVersionDTO) map[string]any {
	if version == nil {
		return nil
	}
	return version.Metadata
}

func requestedVariantsFromMetadata(metadata map[string]any) int {
	return clampInt(intFromAny(metadata["requested_variants"], 1), 1, 4)
}

func mergeFanoutGenerationRuntimeParams(params map[string]any, metadata map[string]any) {
	if params == nil || metadata == nil {
		return
	}
	for _, key := range []string{"fanout_id", "fanout_task_id", "fanout_index", "fanout_total", "source_asset_id", "template_id", "template_version_id"} {
		if value, ok := metadata[key]; ok {
			params[key] = sanitizeGenerationManifestValue(value)
		}
	}
}

func (s *Service) validateFanoutSourceAsset(session *models.EcommerceVisualWorkflowSession, sourceID string) error {
	if strings.TrimSpace(sourceID) == "" {
		return fmt.Errorf("source asset id is required")
	}
	if s.assetRepo == nil {
		return nil
	}
	asset, err := s.assetRepo.FindAssetByID(session.OrganizationID, sourceID)
	if err != nil {
		return fmt.Errorf("source asset %s is not available in organization", sourceID)
	}
	if strings.TrimSpace(asset.StorageKey) == "" || !assetUsableForGeneration(asset.Width, asset.Height) {
		return fmt.Errorf("source asset %s is not usable for generation", sourceID)
	}
	return nil
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func findForbiddenExecutionArtifactKey(raw any) (string, bool) {
	switch value := raw.(type) {
	case map[string]any:
		for key, child := range value {
			if isForbiddenExecutionArtifactKey(key) {
				return key, true
			}
			if found, ok := findForbiddenExecutionArtifactKey(child); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range value {
			if found, ok := findForbiddenExecutionArtifactKey(child); ok {
				return found, true
			}
		}
	}
	return "", false
}

func findForbiddenWritebackMetadataKey(raw any) (string, bool) {
	switch value := raw.(type) {
	case map[string]any:
		for key, child := range value {
			if isForbiddenExecutionArtifactKey(key) || isForbiddenWritebackMetadataKey(key) {
				return key, true
			}
			if found, ok := findForbiddenWritebackMetadataKey(child); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range value {
			if found, ok := findForbiddenWritebackMetadataKey(child); ok {
				return found, true
			}
		}
	}
	return "", false
}

func findForbiddenClientPromptEvidenceKey(raw any) (string, bool) {
	switch value := raw.(type) {
	case map[string]any:
		for key, child := range value {
			if isForbiddenClientPromptEvidenceKey(key) {
				return key, true
			}
			if found, ok := findForbiddenClientPromptEvidenceKey(child); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range value {
			if found, ok := findForbiddenClientPromptEvidenceKey(child); ok {
				return found, true
			}
		}
	}
	return "", false
}

func findForbiddenClientClosureEvidenceKey(raw any) (string, bool) {
	switch value := raw.(type) {
	case map[string]any:
		for key, child := range value {
			if isForbiddenClientClosureEvidenceKey(key) {
				return key, true
			}
			if found, ok := findForbiddenClientClosureEvidenceKey(child); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range value {
			if found, ok := findForbiddenClientClosureEvidenceKey(child); ok {
				return found, true
			}
		}
	}
	return "", false
}

func isForbiddenClientPromptEvidenceKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "prompt_center_status", "prompt_center_snapshot_id", "prompt_center_source":
		return true
	default:
		return false
	}
}

func isForbiddenClientClosureEvidenceKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "product_center_writeback_status", "delivery_download_status", "export_task_id", "export_package_id", "download_id", "charge_metering_status", "charge_session_id", "browser_e2e_status":
		return true
	default:
		return false
	}
}

func isForbiddenWritebackMetadataKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "runtime_job_id", "image_job_id", "provider_job_id", "job_id",
		"storage_key", "storagekey", "internal_storage_key",
		"charge", "charge_id", "chargeid", "charges", "charged", "charge_status",
		"billing", "billing_status", "billing_context", "billing_details", "billing_detail", "billable_item_code":
		return true
	default:
		return false
	}
}

func sanitizeWritebackRelationMetadata(raw map[string]any) map[string]any {
	cleaned, _ := sanitizeWritebackMetadataValue(raw).(map[string]any)
	if cleaned == nil {
		return map[string]any{}
	}
	return cleaned
}

func sanitizeWritebackMetadataValue(raw any) any {
	switch value := raw.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if isForbiddenExecutionArtifactKey(key) || isForbiddenWritebackMetadataKey(key) {
				continue
			}
			out[key] = sanitizeWritebackMetadataValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, child := range value {
			out = append(out, sanitizeWritebackMetadataValue(child))
		}
		return out
	default:
		return raw
	}
}

func assetRelationDTO(item models.EcomAssetRelation) AssetRelationDTO {
	return AssetRelationDTO{
		ID:             item.ID,
		OrganizationID: item.OrganizationID,
		AssetID:        item.AssetID,
		OwnerType:      item.OwnerType,
		OwnerID:        item.OwnerID,
		RelationType:   item.RelationType,
		AssetRole:      item.AssetRole,
		IsPrimary:      item.IsPrimary,
		PlatformCode:   item.PlatformCode,
		SiteCode:       item.SiteCode,
		LocaleCode:     item.LocaleCode,
		SortOrder:      item.SortOrder,
		Visibility:     item.Visibility,
		Metadata:       sanitizeWritebackRelationMetadata(decodeObject(item.Metadata)),
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func isForbiddenExecutionArtifactKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "compiled", "compiled_prompt", "final_prompt", "final_negative_prompt", "source_map", "content_hash", "source_map_hash", "schema_hash", "prompt_run_response", "provider", "provider_response", "run_response", "fake_execution", "execution_artifact",
		"runtime_job_id", "image_job_id", "provider_job_id", "provider_code", "provider_payload",
		"storage_key", "storage_keys", "storagekey", "internal_storage_key",
		"billing", "billing_truth", "billing_status", "billing_context", "billing_details", "billing_detail", "billable_item_code", "charge", "charge_id", "chargeid", "charges", "charged", "charge_status":
		return true
	default:
		return false
	}
}

func validVisualWorkflowStage(v string) bool {
	switch v {
	case models.VisualWorkflowStageSource,
		models.VisualWorkflowStageDeconstruction,
		models.VisualWorkflowStagePrompt,
		models.VisualWorkflowStageGeneration,
		models.VisualWorkflowStageReview,
		models.VisualWorkflowStageExport:
		return true
	default:
		return false
	}
}

func validVisualWorkflowStatus(v string) bool {
	switch v {
	case models.VisualWorkflowStatusDraft,
		models.VisualWorkflowStatusReady,
		models.VisualWorkflowStatusProcessing,
		models.VisualWorkflowStatusBlocked,
		models.VisualWorkflowStatusCompleted,
		models.VisualWorkflowStatusFailed,
		models.VisualWorkflowStatusCanceled:
		return true
	default:
		return false
	}
}

func validVisualReadiness(v string) bool {
	switch v {
	case models.VisualReadinessReady,
		models.VisualReadinessPartial,
		models.VisualReadinessMissing,
		models.VisualReadinessNeedsReview,
		models.VisualReadinessBlocked:
		return true
	default:
		return false
	}
}

func validVisualSourceKind(v string) bool {
	switch v {
	case models.VisualSourceKindUpload,
		models.VisualSourceKindPlatformSourceRef,
		models.VisualSourceKindProductAsset,
		models.VisualSourceKindURL,
		models.VisualSourceKindVideoFrame:
		return true
	default:
		return false
	}
}

func validVisualSourceStatus(v string) bool {
	switch v {
	case models.VisualSourceStatusReady,
		models.VisualSourceStatusPending,
		models.VisualSourceStatusContractNeeded:
		return true
	default:
		return false
	}
}

func validateReadinessMap(readiness map[string]any) error {
	for _, key := range []string{"overall", "source", "deconstruction", "prompt", "generation"} {
		if raw, ok := readiness[key]; ok {
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("readiness.%s must be a string", key)
			}
			if value != "" && !validVisualReadiness(value) {
				return fmt.Errorf("invalid readiness.%s: %s", key, value)
			}
		}
	}
	return nil
}

var allowPrivateSourceResolverHosts = false

var metaTagRe = regexp.MustCompile(`(?is)<meta\s+[^>]*(?:property|name)=["']([^"']+)["'][^>]*content=["']([^"']*)["'][^>]*>`)
var titleTagRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func resolveURLMetadata(raw string) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Hostname() == "" {
		return nil, fmt.Errorf("valid source_url is required")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http/https source_url is supported")
	}
	if isBlockedSourceHost(u.Hostname()) {
		return nil, fmt.Errorf("source_url host is not allowed for resolver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "V-Ecommerce-SourceResolver/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source_url fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("source_url returned unsupported status")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	text := string(body)
	meta := map[string]any{"url": u.String(), "host": u.Hostname(), "status_code": resp.StatusCode, "content_type": resp.Header.Get("Content-Type")}
	if m := titleTagRe.FindStringSubmatch(text); len(m) == 2 {
		meta["title"] = strings.TrimSpace(htmlUnescapeLite(m[1]))
	}
	og := map[string]any{}
	for _, m := range metaTagRe.FindAllStringSubmatch(text, 32) {
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.TrimSpace(htmlUnescapeLite(m[2]))
		if val == "" {
			continue
		}
		switch key {
		case "og:title", "twitter:title", "description", "og:description", "twitter:description", "og:image", "twitter:image", "og:type", "og:site_name":
			og[key] = val
		}
	}
	if len(og) > 0 {
		meta["open_graph"] = og
	}
	return sanitizeGenerationManifestValue(meta).(map[string]any), nil
}

func isBlockedSourceHost(host string) bool {
	if allowPrivateSourceResolverHosts {
		return false
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return true
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}

func htmlUnescapeLite(s string) string {
	repls := map[string]string{"&amp;": "&", "&quot;": "\"", "&#34;": "\"", "&#39;": "'", "&lt;": "<", "&gt;": ">", "\n": " ", "\t": " "}
	for old, newv := range repls {
		s = strings.ReplaceAll(s, old, newv)
	}
	return strings.TrimSpace(s)
}
