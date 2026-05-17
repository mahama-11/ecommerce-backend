package visualworkflow

import (
	"strings"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"

	"gorm.io/gorm"
)

func sessionDTO(item *models.EcommerceVisualWorkflowSession) *SessionDTO {
	if item == nil {
		return nil
	}
	readiness := defaultReadiness()
	if strings.TrimSpace(item.ReadinessJSON) != "" {
		if decoded := decodeObject(item.ReadinessJSON); len(decoded) > 0 {
			readiness = readinessFromMap(decoded)
		}
	}
	return &SessionDTO{
		ID:                 item.ID,
		OrganizationID:     item.OrganizationID,
		UserID:             item.UserID,
		ProductID:          item.ProductID,
		SKUCode:            item.SKUCode,
		ToolSlug:           item.ToolSlug,
		TemplateID:         item.TemplateID,
		TemplateVersionID:  item.TemplateVersionID,
		CurrentStage:       item.CurrentStage,
		Status:             item.Status,
		Readiness:          readiness,
		IntentSpec:         decodeIntentSpec(item.IntentSpecJSON, item),
		PromptPlan:         decodePromptPlan(item.PromptPlanJSON, item),
		GenerationVersions: decodeArray(item.GenerationVersionsJSON),
		IdempotencyKey:     item.IdempotencyKey,
		Metadata:           decodeObject(item.Metadata),
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
}

func sessionDTOs(items []models.EcommerceVisualWorkflowSession) []*SessionDTO {
	dtos := make([]*SessionDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, sessionDTO(&items[i]))
	}
	return dtos
}

func (s *Service) buildStageView(session *models.EcommerceVisualWorkflowSession) (*StageViewDTO, error) {
	readiness := defaultReadiness()
	if strings.TrimSpace(session.ReadinessJSON) != "" {
		if decoded := decodeObject(session.ReadinessJSON); len(decoded) > 0 {
			readiness = readinessFromMap(decoded)
		}
	}
	view := &StageViewDTO{
		SessionID:              session.ID,
		ProductID:              session.ProductID,
		SKUCode:                session.SKUCode,
		ToolSlug:               session.ToolSlug,
		TemplateID:             session.TemplateID,
		TemplateVersionID:      session.TemplateVersionID,
		CurrentStage:           session.CurrentStage,
		Status:                 session.Status,
		Readiness:              readiness,
		DeconstructionElements: []DeconstructionElementDTO{},
		IntentSpec:             decodeIntentSpec(session.IntentSpecJSON, session),
		PromptPlan:             decodePromptPlan(session.PromptPlanJSON, session),
		GenerationVersions:     decodeArray(session.GenerationVersionsJSON),
		UpdatedAt:              session.UpdatedAt,
	}
	if sources, err := s.repo.ListSourceReferences(session.OrganizationID, session.ID); err == nil {
		applySourceReferenceReadiness(view, sources)
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if job, err := s.repo.LatestDeconstructionJob(session.OrganizationID, session.ID); err == nil {
		view.DeconstructionJob = jobDTO(job)
		if job.Status == models.VisualDeconstructionStatusCompleted {
			view.Readiness.Deconstruction = models.VisualReadinessReady
		} else if job.Status == models.VisualDeconstructionStatusContractNeeded {
			view.Readiness.Deconstruction = models.VisualReadinessBlocked
			view.Readiness.Blockers = append(view.Readiness.Blockers, ReadinessBlocker{Code: "CONTRACT_NEEDED", Message: job.ErrorMessage, Target: "deconstruction_job"})
		} else {
			view.Readiness.Deconstruction = models.VisualReadinessPartial
		}
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	elements, err := s.repo.ListDeconstructionElements(session.OrganizationID, session.ID)
	if err != nil {
		return nil, err
	}
	for _, element := range elements {
		view.DeconstructionElements = append(view.DeconstructionElements, elementDTO(element))
	}
	s.applyPromptAndGenerationContractReadiness(view)
	s.applyRuntimeCapabilityReadiness(view)
	view.Readiness.Overall = computeOverall(view.Readiness)
	return view, nil
}

func applySourceReferenceReadiness(view *StageViewDTO, sources []models.EcommerceVisualSourceReference) {
	view.Readiness.Blockers = withoutSourceReadinessBlockers(view.Readiness.Blockers)
	if len(sources) == 0 {
		view.Readiness.Source = models.VisualReadinessBlocked
		appendReadinessBlocker(view, ReadinessBlocker{Code: "DUAL_TRACK_SOURCE_REQUIRED", Target: "source_references", Message: "Upload both a Target SKU source and an Inspiration Reference source before dual-track parsing."})
		return
	}
	view.SourceReferences = make([]SourceReferenceDTO, 0, len(sources))
	var latest *SourceReferenceDTO
	hasSKU := false
	hasReference := false
	hasContractNeeded := false
	for i := range sources {
		dto := sourceDTO(&sources[i])
		view.SourceReferences = append(view.SourceReferences, *dto)
		if latest == nil {
			copyDTO := *dto
			latest = &copyDTO
		}
		role := sourceReferenceRole(dto)
		switch role {
		case "sku":
			hasSKU = true
		case "reference":
			hasReference = true
		}
		if sources[i].Status == models.VisualSourceStatusContractNeeded {
			hasContractNeeded = true
			appendReadinessBlocker(view, ReadinessBlocker{Code: "CONTRACT_NEEDED", Message: sources[i].ErrorMessage, Target: "source_reference"})
		}
	}
	view.SourceReference = latest
	if hasContractNeeded || !hasSKU || !hasReference {
		view.Readiness.Source = models.VisualReadinessBlocked
		if !hasSKU {
			appendReadinessBlocker(view, ReadinessBlocker{Code: "DUAL_TRACK_SKU_SOURCE_REQUIRED", Target: "source_references", Message: "Target SKU source is required for dual-track visual workflow."})
		}
		if !hasReference {
			appendReadinessBlocker(view, ReadinessBlocker{Code: "DUAL_TRACK_REFERENCE_SOURCE_REQUIRED", Target: "source_references", Message: "Inspiration Reference source is required for dual-track visual workflow."})
		}
		return
	}
	view.Readiness.Source = models.VisualReadinessReady
}

func withoutSourceReadinessBlockers(blockers []ReadinessBlocker) []ReadinessBlocker {
	if len(blockers) == 0 {
		return blockers
	}
	filtered := make([]ReadinessBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		code := strings.ToUpper(strings.TrimSpace(blocker.Code))
		target := strings.TrimSpace(blocker.Target)
		if target == "source_reference" || target == "source_references" || strings.HasPrefix(code, "DUAL_TRACK_") || code == "SOURCE_MISSING" {
			continue
		}
		filtered = append(filtered, blocker)
	}
	return filtered
}

func sourceReferenceRole(src *SourceReferenceDTO) string {
	if src == nil {
		return ""
	}
	role, _ := src.Metadata["source_role"].(string)
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "reference" || role == "sku" {
		return role
	}
	if src.SourceKind == "url" {
		return "reference"
	}
	return "sku"
}

func appendReadinessBlocker(view *StageViewDTO, blocker ReadinessBlocker) {
	if blocker.Code == "" {
		return
	}
	if containsBlockerTarget(view.Readiness.Blockers, blocker.Code, blocker.Target) {
		return
	}
	view.Readiness.Blockers = append(view.Readiness.Blockers, blocker)
}

var visualWorkflowRuntimeCapabilityTasks = []string{"image_understanding", "image_generation", "image_inpainting", "video_keyframe"}

func (s *Service) applyPromptAndGenerationContractReadiness(view *StageViewDTO) {
	promptBlocker := ReadinessBlocker{
		Code:    "CONTRACT_NEEDED",
		Target:  "prompt_plan",
		Message: "Prompt execution/preview contract is not finalized; no provider or fake Prompt Center execution was called.",
	}
	if view.PromptPlan.Status == "ready" && metadataString(view.PromptPlan.Metadata, "source") == "backend_intent_fusion" {
		view.Readiness.Prompt = models.VisualReadinessReady
	} else {
		view.Readiness.Prompt = models.VisualReadinessBlocked
		if !containsBlockerTarget(view.Readiness.Blockers, promptBlocker.Code, promptBlocker.Target) {
			view.Readiness.Blockers = append(view.Readiness.Blockers, promptBlocker)
		}
	}
	for _, blocker := range view.PromptPlan.Blockers {
		if blocker.Code == "CONTRACT_NEEDED" && strings.TrimSpace(blocker.Target) == "prompt_plan" && !containsBlockerTarget(view.Readiness.Blockers, blocker.Code, blocker.Target) {
			view.Readiness.Blockers = append(view.Readiness.Blockers, blocker)
		}
	}
	generationBlocker := ReadinessBlocker{
		Code:    "CONTRACT_NEEDED",
		Target:  "generation",
		Message: "Generation execution contract is not finalized; no provider call was made.",
	}
	view.Readiness.Generation = models.VisualReadinessBlocked
	if !containsBlockerTarget(view.Readiness.Blockers, generationBlocker.Code, generationBlocker.Target) {
		view.Readiness.Blockers = append(view.Readiness.Blockers, generationBlocker)
	}
}

func containsBlockerTarget(blockers []ReadinessBlocker, code, target string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code && blocker.Target == target {
			return true
		}
	}
	return false
}

func (s *Service) applyRuntimeCapabilityReadiness(view *StageViewDTO) {
	if s.capabilityReader == nil {
		return
	}
	matrix, err := s.capabilityReader.ListRuntimeCapabilities("ecommerce", "")
	if err != nil {
		message := safePlatformCapabilityErrorMessage
		view.RuntimeCapabilityError = &RuntimeCapabilityError{Code: "PLATFORM_CAPABILITY_ERROR", Message: message}
		view.Readiness.Blockers = append(view.Readiness.Blockers, ReadinessBlocker{Code: "PLATFORM_CAPABILITY_ERROR", Message: message, Target: "runtime_capabilities"})
		return
	}
	if matrix == nil {
		return
	}
	wanted := map[string]bool{}
	for _, task := range visualWorkflowRuntimeCapabilityTasks {
		wanted[task] = true
	}
	itemsByTask := map[string]platform.RuntimeCapabilityItem{}
	for _, item := range matrix.Items {
		if wanted[item.TaskType] {
			itemsByTask[item.TaskType] = item
		}
	}
	for _, task := range visualWorkflowRuntimeCapabilityTasks {
		item, ok := itemsByTask[task]
		if !ok {
			continue
		}
		view.RuntimeCapabilities = append(view.RuntimeCapabilities, runtimeCapabilityProjection(item))
		if !item.Available || item.Status == "unavailable" {
			s.applyRuntimeCapabilityBlocker(view, item)
		}
	}
}

func runtimeCapabilityProjection(item platform.RuntimeCapabilityItem) RuntimeCapabilityProjection {
	return RuntimeCapabilityProjection{
		TaskType:          item.TaskType,
		Status:            item.Status,
		Available:         item.Available,
		UnavailableReason: item.UnavailableReason,
		ContractStatus:    item.ContractStatus,
		Reasons:           item.Reasons,
	}
}

func (s *Service) applyRuntimeCapabilityBlocker(view *StageViewDTO, item platform.RuntimeCapabilityItem) {
	if item.TaskType != "image_understanding" && item.TaskType != "image_generation" {
		return
	}
	reason := firstRuntimeCapabilityReason(item)
	message := reason.Message
	if strings.TrimSpace(message) == "" {
		message = "Platform runtime capability is unavailable"
	}
	blocker := ReadinessBlocker{Code: "PLATFORM_CAPABILITY_UNAVAILABLE", Message: message, Target: "runtime_capabilities." + item.TaskType}
	view.Readiness.Blockers = append(view.Readiness.Blockers, blocker)
	if item.TaskType == "image_understanding" && needsDeconstructionCapability(view) {
		view.Readiness.Deconstruction = models.VisualReadinessBlocked
	}
	if item.TaskType == "image_generation" {
		view.Readiness.Generation = models.VisualReadinessBlocked
	}
}

func firstRuntimeCapabilityReason(item platform.RuntimeCapabilityItem) platform.RuntimeCapabilityReason {
	if len(item.Reasons) > 0 {
		return item.Reasons[0]
	}
	return platform.RuntimeCapabilityReason{Code: item.UnavailableReason, Message: "Platform runtime capability is unavailable"}
}

func needsDeconstructionCapability(view *StageViewDTO) bool {
	if view.DeconstructionJob == nil {
		return false
	}
	if view.DeconstructionJob.Status == models.VisualDeconstructionStatusCompleted {
		return false
	}
	return view.DeconstructionJob.RuntimeTaskType == "image_understanding" || view.DeconstructionJob.UnavailableReason == "contract-needed" || view.DeconstructionJob.Status == models.VisualDeconstructionStatusContractNeeded
}

func defaultReadiness() ReadinessDTO {
	return ReadinessDTO{
		Overall:        models.VisualReadinessMissing,
		Source:         models.VisualReadinessMissing,
		Deconstruction: models.VisualReadinessMissing,
		Prompt:         models.VisualReadinessMissing,
		Generation:     models.VisualReadinessMissing,
		Blockers: []ReadinessBlocker{
			{Code: "SOURCE_MISSING", Message: "A source reference is required before visual deconstruction", Target: "source_reference"},
		},
	}
}

func readinessFromMap(raw map[string]any) ReadinessDTO {
	out := defaultReadiness()
	if v, ok := raw["overall"].(string); ok && v != "" {
		out.Overall = v
	}
	if v, ok := raw["source"].(string); ok && v != "" {
		out.Source = v
	}
	if v, ok := raw["deconstruction"].(string); ok && v != "" {
		out.Deconstruction = v
	}
	if v, ok := raw["prompt"].(string); ok && v != "" {
		out.Prompt = v
	}
	if v, ok := raw["generation"].(string); ok && v != "" {
		out.Generation = v
	}
	return out
}

func computeOverall(r ReadinessDTO) string {
	if r.Source == models.VisualReadinessBlocked || r.Deconstruction == models.VisualReadinessBlocked || r.Prompt == models.VisualReadinessBlocked || r.Generation == models.VisualReadinessBlocked || len(r.Blockers) > 0 {
		return models.VisualReadinessBlocked
	}
	if r.Source == models.VisualReadinessReady && r.Deconstruction == models.VisualReadinessReady && r.Prompt == models.VisualReadinessReady && r.Generation == models.VisualReadinessReady {
		return models.VisualReadinessReady
	}
	if r.Source == models.VisualReadinessMissing {
		return models.VisualReadinessMissing
	}
	return models.VisualReadinessPartial
}

func sourceDTO(item *models.EcommerceVisualSourceReference) *SourceReferenceDTO {
	out := &SourceReferenceDTO{
		ID:              item.ID,
		SourceKind:      item.SourceKind,
		SourceRef:       item.SourceRef,
		AssetID:         item.AssetID,
		AssetRelationID: item.AssetRelationID,
		MimeType:        item.MimeType,
		Status:          item.Status,
		ResolveStatus:   item.ResolveStatus,
		ErrorCode:       item.ErrorCode,
		ErrorMessage:    item.ErrorMessage,
		Metadata:        sanitizeDeconstructionRequestMetadata(decodeObject(item.Metadata)),
	}
	if item.AssetID != "" {
		out.AssetContentURL = "/api/v1/ecommerce/assets/" + item.AssetID + "/content"
	}
	if item.Status == models.VisualSourceStatusContractNeeded || item.ResolveStatus == models.VisualSourceStatusContractNeeded {
		out.UnavailableReason = "contract-needed"
	}
	return out
}

func jobDTO(item *models.EcommerceVisualDeconstructionJob) *DeconstructionJobDTO {
	out := &DeconstructionJobDTO{
		JobID:           item.ID,
		RuntimeJobID:    item.RuntimeJobID,
		Status:          item.Status,
		Stage:           item.Stage,
		StageMessage:    item.StageMessage,
		Progress:        item.Progress,
		CapabilityCode:  item.CapabilityCode,
		RuntimeTaskType: item.RuntimeTaskType,
		ErrorCode:       item.ErrorCode,
		ErrorMessage:    item.ErrorMessage,
	}
	if item.Status == models.VisualDeconstructionStatusContractNeeded || item.ErrorCode == "CONTRACT_NEEDED" {
		out.UnavailableReason = "contract-needed"
	}
	return out
}

func elementDTO(item models.EcommerceVisualDeconstructionElement) DeconstructionElementDTO {
	metadata := decodeObject(item.Metadata)
	out := DeconstructionElementDTO{
		ID:                item.ID,
		ElementType:       item.ElementType,
		ElementKey:        item.ElementKey,
		Label:             item.Label,
		Confidence:        item.Confidence,
		MaskAssetID:       item.MaskAssetID,
		SourceAssetID:     item.SourceAssetID,
		Value:             decodeObject(item.ValueJSON),
		Readiness:         item.Readiness,
		Selected:          item.Selected,
		Confirmed:         item.Confirmed,
		SourceReferenceID: metadataString(metadata, "source_reference_id"),
		SourceRole:        metadataString(metadata, "source_role"),
		Decision:          metadataString(metadata, "decision"),
		SortOrder:         item.SortOrder,
	}
	if item.BoundingBoxJSON != "" {
		out.BoundingBox = decodeObject(item.BoundingBoxJSON)
	}
	if item.MaskAssetID != "" {
		out.MaskAssetContentURL = "/api/v1/ecommerce/assets/" + item.MaskAssetID + "/content"
	}
	return out
}
