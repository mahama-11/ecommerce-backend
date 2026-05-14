package visualworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	capabilityReader RuntimeCapabilityReader
	runtimeCreator   RuntimeJobCreator
}

func NewService(repo *repository.VisualWorkflowRepository, productRepo *repository.ProductCenterRepository, assetRepo *repository.ImageRuntimeRepository) *Service {
	return &Service{repo: repo, productRepo: productRepo, assetRepo: assetRepo}
}

func (s *Service) WithPromptRepository(promptRepo *repository.PromptCenterRepository) *Service {
	s.promptRepo = promptRepo
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
		if err := validatePromptPlan(&promptPlan); err != nil {
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
	if kind == models.VisualSourceKindURL || kind == models.VisualSourceKindVideoFrame {
		status = models.VisualSourceStatusContractNeeded
		resolveStatus = models.VisualSourceStatusContractNeeded
		errorCode = "CONTRACT_NEEDED"
		errorMessage = "URL/video analysis is not implemented until platform source/runtime contracts are finalized"
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
	sourceID := strings.TrimSpace(req.SourceReferenceID)
	if sourceID == "" {
		if src, err := s.repo.LatestSourceReference(orgID, sessionID); err == nil {
			sourceID = src.ID
		} else if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	} else if _, err := s.repo.GetSourceReference(orgID, sessionID, sourceID); err != nil {
		return nil, fmt.Errorf("source_reference_id is not in this session: %w", err)
	}
	idempotencyKey := deconstructionIdempotencyKey(orgID, session.ID, sourceID, req)
	if existing, err := s.repo.FindDeconstructionJobByIdempotencyKey(orgID, sessionID, idempotencyKey); err == nil {
		return existing, nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	manifest := map[string]any{
		"session_id":          session.ID,
		"product_id":          session.ProductID,
		"sku_code":            session.SKUCode,
		"source_reference_id": sourceID,
		"requested_elements":  req.RequestedElements,
		"output": map[string]any{
			"include_bounding_boxes": true,
			"include_masks":          false,
			"schema_version":         "visual-deconstruction.v1",
		},
	}
	metadata := sanitizeDeconstructionRequestMetadata(req.Metadata)
	metadata["platform_product_code"] = "ecommerce"
	metadata["platform_task_type"] = "image_understanding"
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
		StageMessage:       "Visual deconstruction job is queued for Platform runtime orchestration",
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
	runtimeJob, err := s.runtimeCreator.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:    "ecommerce",
		TaskType:       "image_generation",
		ProviderMode:   "async",
		OrganizationID: session.OrganizationID,
		UserID:         session.UserID,
		SourceType:     "visual_generation",
		SourceID:       version.VersionID,
		IdempotencyKey: idempotencyKey,
		InputManifest:  mustJSON(manifest),
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
		"requested_variants": 1,
		"params_snapshot":    map[string]any{},
		"source_asset_ids":   []string{},
		"source_assets":      []map[string]any{},
		"ecommerce_snapshot": sanitizedGenerationManifest(session, version, promptPlan, intentSpec),
	}
	if version == nil || strings.TrimSpace(version.PromptID) == "" {
		return nil, fmt.Errorf("Prompt Center snapshot is required for visual generation runtime")
	}
	if s.promptRepo == nil {
		return nil, fmt.Errorf("Prompt Center snapshot is required for prompt_id runtime generation")
	}
	promptRun, err := s.promptRepo.FindPromptRunByID(session.OrganizationID, version.PromptID)
	if err != nil {
		return nil, fmt.Errorf("Prompt Center snapshot missing or not in organization")
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
		if err != nil || strings.TrimSpace(asset.StorageKey) == "" {
			return nil, fmt.Errorf("Prompt Center source asset snapshot is invalid")
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
	manifest["prompt_snapshot"] = map[string]any{
		"provider":        "",
		"model":           "",
		"system_prompt":   "",
		"style_prompt":    compiled.FinalNegativePrompt,
		"user_prompt":     compiled.FinalPrompt,
		"prompt_template": promptRun.TemplateCode,
	}
	manifest["params_snapshot"] = map[string]any{
		"prompt_id":           promptRun.ID,
		"template_id":         promptRun.TemplateID,
		"template_version_id": promptRun.TemplateVersionID,
		"schema_version":      promptRun.SchemaVersion,
		"content_hash":        promptRun.ContentHash,
		"source_map_hash":     promptRun.SourceMapHash,
	}
	manifest["source_asset_ids"] = sourceAssetIDs
	manifest["source_assets"] = sourceAssets
	return sanitizeGenerationManifestValue(manifest).(map[string]any), nil
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
		"output_contract": map[string]any{"schema": "visual_prompt_plan.v1", "required_fields": []string{"schema_version", "status", "prompt_id", "scene_type", "variables"}},
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

func (s *Service) InternalUpdatePromptPlannerRuntime(sessionID string, req InternalRuntimeUpdateRequest) (*PromptPlannerJobResponse, error) {
	session, err := s.repo.FindSessionByID(sessionID)
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
	session, err := s.repo.FindSessionByID(sessionID)
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
	var plan PromptPlanDTO
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("%w: prompt planner result must be valid prompt plan JSON", ErrInternalCallbackInvalid)
	}
	applyPromptPlanDefaults(&plan, session)
	if err := validatePromptPlan(&plan); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInternalCallbackInvalid, err)
	}
	session.PromptPlanJSON = encodePromptPlan(&plan)
	session.CurrentStage = models.VisualWorkflowStageGeneration
	session.Status = models.VisualWorkflowStatusReady
	metadata := decodeObject(session.Metadata)
	metadata["prompt_planner"] = map[string]any{"status": "completed", "stage": req.Stage, "progress": req.Progress}
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

func buildPromptPlannerPrompt(session *models.EcommerceVisualWorkflowSession, intentSpec *IntentSpecDTO, existingPlan *PromptPlanDTO, req CreatePromptPlannerJobRequest) string {
	payload := map[string]any{"instruction": "Return strict JSON visual_prompt_plan.v1 for provider-executable ecommerce visual generation. Include prompt_id/status/scene_type/variables/source_assets when known. Do not include execution artifact or credential fields.", "product_id": session.ProductID, "sku_code": session.SKUCode, "marketplace": req.Marketplace, "locale": req.Locale, "drift_controls": req.DriftControls, "prompt_variables": req.PromptVariables, "prompt_id": req.PromptID, "template_id": req.TemplateID, "intent_spec": intentSpec, "existing_prompt_plan": existingPlan}
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
				return value
			}
		}
		if asset, ok := variant["asset"].(map[string]any); ok {
			for _, key := range []string{"inline_data", "text", "content", "body"} {
				if value, ok := asset[key].(string); ok && strings.TrimSpace(value) != "" {
					return value
				}
			}
		}
	}
	return ""
}

func (s *Service) createPlatformDeconstructionRuntimeJob(item *models.EcommerceVisualDeconstructionJob, manifest map[string]any) error {
	if item == nil || s.runtimeCreator == nil {
		return nil
	}
	manifest = sanitizedDeconstructionManifest(manifest)
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
		InputManifest:  mustJSON(manifest),
		Metadata: mustJSON(map[string]any{
			"product_id":          item.ProductID,
			"sku_code":            item.SKUCode,
			"session_id":          item.SessionID,
			"source_reference_id": item.SourceReferenceID,
			"capability_code":     item.CapabilityCode,
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
	if len(elementInputs) == 0 {
		if len(input.Variants) > 0 {
			return nil, fmt.Errorf("%w: visual deconstruction result must contain deconstruction elements; image generation variants are not accepted", ErrInternalCallbackInvalid)
		}
		if status == models.VisualDeconstructionStatusCompleted {
			return nil, fmt.Errorf("%w: visual deconstruction completed result contains no elements", ErrInternalCallbackInvalid)
		}
	}
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

	elements := make([]models.EcommerceVisualDeconstructionElement, 0, len(elementInputs))
	for idx, in := range elementInputs {
		elements = append(elements, internalResultElementToModel(item, in, idx))
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

func (s *Service) HasGenerationVersion(versionID string) bool {
	if strings.TrimSpace(versionID) == "" {
		return false
	}
	_, _, _, err := s.findGenerationVersionByID(strings.TrimSpace(versionID))
	return err == nil
}

func (s *Service) InternalUpdateGenerationRuntime(versionID string, input InternalRuntimeUpdateRequest) (*GenerationVersionDTO, error) {
	session, versions, idx, err := s.findGenerationVersionByID(strings.TrimSpace(versionID))
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
	session, versions, idx, err := s.findGenerationVersionByID(strings.TrimSpace(versionID))
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
	return input.Metadata.DeconstructionElements
}

func internalResultElementToModel(job *models.EcommerceVisualDeconstructionJob, input InternalResultElementRequest, idx int) models.EcommerceVisualDeconstructionElement {
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
		Metadata:        mustJSON(sanitizeDeconstructionRequestMetadata(input.Metadata)),
	}
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
	return item, s.repo.UpdateDeconstructionElement(item)
}

func (s *Service) ApplyAttentionTree(orgID, sessionID string, req ApplyAttentionTreeRequest) ([]models.EcommerceVisualDeconstructionElement, error) {
	if len(req.Decisions) == 0 {
		return nil, fmt.Errorf("attention decisions are required")
	}
	if _, err := s.repo.GetSession(orgID, sessionID); err != nil {
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
		if err := applyAttentionDecisionToElement(item, decision.Decision, decision.GroupPath, decision.TargetAssetID, decision.Rationale, decision.Confidence, decision.Metadata); err != nil {
			return nil, err
		}
		if err := s.repo.UpdateDeconstructionElement(item); err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, nil
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
