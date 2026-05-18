package visualworkflow

import (
	"fmt"
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
	view.BusinessFlow = buildBusinessWorkflowDAG(view)
	view.IntegrationVerdict = buildIntegrationVerdict(view)
	view.RollbackSnapshot = buildRollbackSnapshot(view)
	view.ReleaseReadiness = buildReleaseReadiness(view)
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
	promptReady := view.PromptPlan.Status == "ready" && (metadataString(view.PromptPlan.Metadata, "source") == "backend_intent_fusion" || metadataString(view.PromptPlan.Metadata, "prompt_center_status") == "validated")
	if promptReady {
		view.Readiness.Prompt = models.VisualReadinessReady
	} else {
		view.Readiness.Prompt = models.VisualReadinessBlocked
		if !containsBlockerTarget(view.Readiness.Blockers, promptBlocker.Code, promptBlocker.Target) {
			view.Readiness.Blockers = append(view.Readiness.Blockers, promptBlocker)
		}
	}
	if !promptReady {
		for _, blocker := range view.PromptPlan.Blockers {
			if blocker.Code == "CONTRACT_NEEDED" && strings.TrimSpace(blocker.Target) == "prompt_plan" && !containsBlockerTarget(view.Readiness.Blockers, blocker.Code, blocker.Target) {
				view.Readiness.Blockers = append(view.Readiness.Blockers, blocker)
			}
		}
	}
	generationBlocker := ReadinessBlocker{
		Code:    "CONTRACT_NEEDED",
		Target:  "generation",
		Message: "Generation execution contract is not finalized; no provider call was made.",
	}
	if generationHasProviderResult(view.GenerationVersions) {
		view.Readiness.Generation = models.VisualReadinessReady
		return
	}
	view.Readiness.Generation = models.VisualReadinessBlocked
	if !containsBlockerTarget(view.Readiness.Blockers, generationBlocker.Code, generationBlocker.Target) {
		view.Readiness.Blockers = append(view.Readiness.Blockers, generationBlocker)
	}
}

func generationHasProviderResult(versions []GenerationVersionDTO) bool {
	for _, version := range versions {
		if strings.TrimSpace(version.RuntimeJobID) != "" && len(version.ResultAssets) > 0 && (version.Status == "completed" || version.Stage == "result_available") {
			return true
		}
	}
	return false
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

func buildBusinessWorkflowDAG(view *StageViewDTO) *BusinessWorkflowDAGDTO {
	if view == nil {
		return nil
	}
	flow := &BusinessWorkflowDAGDTO{
		SchemaVersion: "ecommerce_business_flow.v1",
		FlowID:        view.SessionID,
		Status:        view.Readiness.Overall,
		Persistence:   "stage_view_projection",
		Nodes:         []BusinessWorkflowNodeDTO{},
		Edges: []BusinessWorkflowEdgeDTO{
			{From: "source", To: "deconstruction", Dependency: "source_assets_ready"},
			{From: "deconstruction", To: "prompt_plan", Dependency: "elements_available"},
			{From: "prompt_plan", To: "generation", Dependency: "prompt_plan_ready"},
			{From: "generation", To: "workshop", Dependency: "result_assets_available"},
			{From: "workshop", To: "product_center_writeback", Dependency: "selected_result_asset"},
			{From: "product_center_writeback", To: "delivery_download", Dependency: "adopted_asset_or_listing_version"},
			{From: "delivery_download", To: "charge_metering", Dependency: "billable_delivery_or_runtime_event"},
		},
	}
	flow.Nodes = append(flow.Nodes,
		BusinessWorkflowNodeDTO{NodeID: "source", Label: "Source asset intake", Owner: "ecommerce-backend", Status: view.Readiness.Source, Readiness: view.Readiness.Source, Evidence: map[string]any{"source_reference_count": len(view.SourceReferences)}},
		BusinessWorkflowNodeDTO{NodeID: "deconstruction", Label: "Prep Hub image understanding", Owner: "platform-runtime+ecommerce-backend", Status: businessDeconstructionStatus(view), Readiness: view.Readiness.Deconstruction, Evidence: businessDeconstructionEvidence(view), Blockers: blockersForBusinessNode(view, "deconstruction_job")},
		BusinessWorkflowNodeDTO{NodeID: "prompt_plan", Label: "Prompt planning", Owner: "ecommerce-backend+platform-runtime", Status: businessPromptStatus(view), Readiness: view.Readiness.Prompt, Evidence: map[string]any{"prompt_id": view.PromptPlan.PromptID, "prompt_plan_status": view.PromptPlan.Status}, Blockers: blockersForBusinessNode(view, "prompt_plan")},
		BusinessWorkflowNodeDTO{NodeID: "generation", Label: "Provider generation", Owner: "platform-runtime", Status: businessGenerationStatus(view), Readiness: view.Readiness.Generation, Evidence: businessGenerationEvidence(view), Blockers: blockersForBusinessNode(view, "generation")},
		BusinessWorkflowNodeDTO{NodeID: "workshop", Label: "Workshop review and selection", Owner: "ecommerce-frontend+ecommerce-backend", Status: businessWorkshopStatus(view), Evidence: businessWorkshopEvidence(view)},
		BusinessWorkflowNodeDTO{NodeID: "product_center_writeback", Label: "Product Center writeback", Owner: "ecommerce-backend", Status: businessWritebackStatus(view), Evidence: businessWritebackEvidence(view)},
		BusinessWorkflowNodeDTO{NodeID: "delivery_download", Label: "Delivery / Download Center", Owner: "ecommerce-backend", Status: businessMetadataStatus(view, "delivery_download_status", models.VisualReadinessMissing), Evidence: map[string]any{"source": "productcore/export_download"}},
		BusinessWorkflowNodeDTO{NodeID: "charge_metering", Label: "Charge and metering", Owner: "platform-backend+ecommerce-backend", Status: businessMetadataStatus(view, "charge_metering_status", models.VisualReadinessMissing), Evidence: map[string]any{"source": "platform_charge_sessions"}},
	)
	return flow
}

func businessDeconstructionStatus(view *StageViewDTO) string {
	if view.DeconstructionJob != nil && view.DeconstructionJob.Status != "" {
		return view.DeconstructionJob.Status
	}
	return view.Readiness.Deconstruction
}

func businessDeconstructionEvidence(view *StageViewDTO) map[string]any {
	evidence := map[string]any{"element_count": len(view.DeconstructionElements)}
	if view.DeconstructionJob != nil {
		evidence["job_id"] = view.DeconstructionJob.JobID
		evidence["runtime_job_id"] = view.DeconstructionJob.RuntimeJobID
		evidence["runtime_task_type"] = view.DeconstructionJob.RuntimeTaskType
	}
	return evidence
}

func businessPromptStatus(view *StageViewDTO) string {
	if strings.TrimSpace(view.PromptPlan.Status) != "" {
		return strings.TrimSpace(view.PromptPlan.Status)
	}
	return view.Readiness.Prompt
}

func businessGenerationStatus(view *StageViewDTO) string {
	latest := latestGenerationVersion(view.GenerationVersions)
	if latest != nil && latest.Status != "" {
		return latest.Status
	}
	return view.Readiness.Generation
}

func businessGenerationEvidence(view *StageViewDTO) map[string]any {
	evidence := map[string]any{"generation_version_count": len(view.GenerationVersions)}
	latest := latestGenerationVersion(view.GenerationVersions)
	if latest != nil {
		evidence["latest_version_id"] = latest.VersionID
		evidence["runtime_job_id"] = latest.RuntimeJobID
		evidence["result_asset_count"] = len(latest.ResultAssets)
		evidence["selected_result_asset_id"] = latest.SelectedResultAssetID
	}
	return evidence
}

func businessWorkshopStatus(view *StageViewDTO) string {
	latest := latestGenerationVersion(view.GenerationVersions)
	if latest == nil || len(latest.ResultAssets) == 0 {
		return models.VisualReadinessMissing
	}
	if latest.SelectedResultAssetID != "" {
		return models.VisualReadinessReady
	}
	return models.VisualReadinessPartial
}

func businessWorkshopEvidence(view *StageViewDTO) map[string]any {
	latest := latestGenerationVersion(view.GenerationVersions)
	if latest == nil {
		return map[string]any{"generation_version_count": len(view.GenerationVersions)}
	}
	return map[string]any{"latest_version_id": latest.VersionID, "result_asset_count": len(latest.ResultAssets), "selected_result_asset_id": latest.SelectedResultAssetID}
}

func businessWritebackStatus(view *StageViewDTO) string {
	for _, version := range view.GenerationVersions {
		if metadataString(version.Metadata, "writeback_relation_id") != "" || metadataString(version.Metadata, "writeback_asset_id") != "" || metadataString(version.Metadata, "writeback_status") == models.VisualReadinessReady || metadataNestedString(version.Metadata, "writeback", "status") == "succeeded" {
			return models.VisualReadinessReady
		}
	}
	return businessMetadataStatus(view, "product_center_writeback_status", models.VisualReadinessMissing)
}

func businessWritebackEvidence(view *StageViewDTO) map[string]any {
	for _, version := range view.GenerationVersions {
		if metadataString(version.Metadata, "writeback_relation_id") != "" || metadataString(version.Metadata, "writeback_asset_id") != "" || metadataString(version.Metadata, "writeback_status") != "" || metadataNestedString(version.Metadata, "writeback", "status") != "" {
			return map[string]any{"version_id": version.VersionID, "writeback_status": defaultString(metadataString(version.Metadata, "writeback_status"), metadataNestedString(version.Metadata, "writeback", "status")), "writeback_asset_id": defaultString(metadataString(version.Metadata, "writeback_asset_id"), metadataNestedString(version.Metadata, "writeback", "asset_id")), "writeback_relation_id": defaultString(metadataString(version.Metadata, "writeback_relation_id"), metadataNestedString(version.Metadata, "writeback", "asset_relation_id"))}
		}
	}
	return map[string]any{"source": "generation_version_metadata"}
}

func businessMetadataStatus(view *StageViewDTO, key, fallback string) string {
	for _, version := range view.GenerationVersions {
		if status := metadataString(version.Metadata, key); status != "" {
			return status
		}
	}
	return fallback
}

func metadataNestedString(metadata map[string]any, objectKey, valueKey string) string {
	if metadata == nil {
		return ""
	}
	raw, ok := metadata[objectKey]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(obj[valueKey]))
}

func latestGenerationVersion(versions []GenerationVersionDTO) *GenerationVersionDTO {
	if len(versions) == 0 {
		return nil
	}
	return &versions[len(versions)-1]
}

func blockersForBusinessNode(view *StageViewDTO, targets ...string) []ReadinessBlocker {
	if view == nil || len(view.Readiness.Blockers) == 0 {
		return nil
	}
	wanted := map[string]bool{}
	for _, target := range targets {
		wanted[target] = true
	}
	out := []ReadinessBlocker{}
	for _, blocker := range view.Readiness.Blockers {
		if wanted[blocker.Target] {
			out = append(out, blocker)
		}
	}
	return out
}

func buildIntegrationVerdict(view *StageViewDTO) *IntegrationVerdictDTO {
	if view == nil || view.BusinessFlow == nil {
		return nil
	}
	gates := make([]IntegrationVerdictGateDTO, 0, len(view.BusinessFlow.Nodes))
	ready := 0
	blocked := false
	failed := false
	for _, node := range view.BusinessFlow.Nodes {
		status := normalizeVerdictGateStatus(node.Status)
		if status == models.VisualReadinessReady || status == "completed" || status == "succeeded" {
			ready++
		}
		if status == "failed" || status == "error" {
			failed = true
		}
		if status == models.VisualReadinessBlocked || status == models.VisualDeconstructionStatusContractNeeded || status == "invalid" || len(node.Blockers) > 0 {
			blocked = true
		}
		gates = append(gates, IntegrationVerdictGateDTO{GateID: node.NodeID, Label: node.Label, Status: status, Evidence: node.Evidence})
	}
	verdict := "blocked"
	if failed {
		verdict = "fail"
	} else if blocked || len(view.Readiness.Blockers) > 0 {
		verdict = "blocked"
	} else if ready == len(gates) && len(gates) > 0 {
		verdict = "pass"
	}
	return &IntegrationVerdictDTO{SchemaVersion: "ecommerce_integration_verdict.v1", Status: verdict, ReadyCount: ready, TotalCount: len(gates), Gates: gates, Blockers: view.Readiness.Blockers}
}

func normalizeVerdictGateStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return models.VisualReadinessMissing
	}
	return status
}

func buildRollbackSnapshot(view *StageViewDTO) *RollbackSnapshotDTO {
	if view == nil {
		return nil
	}
	scopes := []RollbackScopeDTO{
		{ScopeID: "workflow_session", ResourceType: "visual_workflow_session", ResourceID: view.SessionID, Action: "preserve_state_and_retry_failed_node", Safe: true, Evidence: map[string]any{"current_stage": view.CurrentStage, "status": view.Status}},
	}
	if view.DeconstructionJob != nil {
		scopes = append(scopes, RollbackScopeDTO{ScopeID: "deconstruction_job", ResourceType: "visual_deconstruction_job", ResourceID: view.DeconstructionJob.JobID, Action: "cancel_retry_or_supersede", Safe: view.DeconstructionJob.Status != "completed", Evidence: businessDeconstructionEvidence(view)})
	}
	latest := latestGenerationVersion(view.GenerationVersions)
	if latest != nil {
		scopes = append(scopes, RollbackScopeDTO{ScopeID: "generation_version", ResourceType: "visual_generation_version", ResourceID: latest.VersionID, Action: "restore_previous_selected_version_or_retry", Safe: latest.Status != "completed" || latest.SelectedResultAssetID == "", Evidence: businessGenerationEvidence(view)})
		if latest.SelectedResultAssetID != "" {
			scopes = append(scopes, RollbackScopeDTO{ScopeID: "selected_asset", ResourceType: "generation_result_asset", ResourceID: latest.SelectedResultAssetID, Action: "reselect_previous_asset_or_unselect", Safe: true, Evidence: map[string]any{"version_id": latest.VersionID}})
		}
		if metadataString(latest.Metadata, "writeback_asset_id") != "" || metadataString(latest.Metadata, "writeback_relation_id") != "" {
			scopes = append(scopes, RollbackScopeDTO{ScopeID: "product_center_writeback", ResourceType: "product_asset_relation", ResourceID: metadataString(latest.Metadata, "writeback_relation_id"), Action: "restore_previous_relation_or_mark_superseded", Safe: false, Evidence: businessWritebackEvidence(view)})
		}
		if metadataString(latest.Metadata, "delivery_download_status") != "" {
			scopes = append(scopes, RollbackScopeDTO{ScopeID: "delivery_download", ResourceType: "export_package", ResourceID: metadataString(latest.Metadata, "export_package_id"), Action: "void_or_supersede_download_package", Safe: false, Evidence: map[string]any{"status": metadataString(latest.Metadata, "delivery_download_status")}})
		}
		if metadataString(latest.Metadata, "charge_metering_status") != "" {
			scopes = append(scopes, RollbackScopeDTO{ScopeID: "charge_metering", ResourceType: "charge_session", ResourceID: metadataString(latest.Metadata, "charge_session_id"), Action: "manual_compensation_policy_required", Safe: false, Evidence: map[string]any{"status": metadataString(latest.Metadata, "charge_metering_status")}})
		}
	}
	status := "available"
	if view.Readiness.Overall == models.VisualReadinessBlocked {
		status = "blocked"
	}
	return &RollbackSnapshotDTO{SchemaVersion: "ecommerce_rollback_snapshot.v1", SessionID: view.SessionID, Status: status, Scopes: scopes, Instructions: []string{"Retry failed runtime nodes through Platform runtime; do not mutate shared charge truth in Ecommerce.", "User-visible compensation requires product-owner approval before automatic adjustment."}, Metadata: map[string]any{"persistence": "stage_view_projection"}}
}

func buildReleaseReadiness(view *StageViewDTO) *ReleaseReadinessDTO {
	if view == nil {
		return nil
	}
	gates := []IntegrationVerdictGateDTO{
		{GateID: "business_flow", Label: "Business workflow DAG", Status: statusFromBool(view.BusinessFlow != nil), Evidence: map[string]any{"node_count": businessFlowNodeCount(view)}},
		{GateID: "integration_verdict", Label: "Integration verdict", Status: statusFromBool(view.IntegrationVerdict != nil && view.IntegrationVerdict.Status == "pass"), Evidence: map[string]any{"verdict": verdictStatus(view)}},
		{GateID: "rollback_snapshot", Label: "Rollback snapshot", Status: statusFromBool(view.RollbackSnapshot != nil && len(view.RollbackSnapshot.Scopes) > 0), Evidence: map[string]any{"scope_count": rollbackScopeCount(view)}},
		{GateID: "runtime_capability", Label: "Platform runtime capability", Status: runtimeCapabilityGateStatus(view), Evidence: map[string]any{"capability_count": len(view.RuntimeCapabilities)}},
		{GateID: "frontend_browser", Label: "Browser verification", Status: businessMetadataStatus(view, "browser_e2e_status", models.VisualReadinessMissing), Evidence: map[string]any{"source": "evidence_manifest"}},
	}
	status := "pass"
	for _, gate := range gates {
		if gate.Status == models.VisualReadinessBlocked || gate.Status == "blocked" || gate.Status == "failed" {
			status = "blocked"
			break
		}
		if gate.Status != models.VisualReadinessReady && gate.Status != "pass" {
			status = "blocked"
		}
	}
	return &ReleaseReadinessDTO{SchemaVersion: "ecommerce_release_readiness.v1", Status: status, Gates: gates, Blockers: view.Readiness.Blockers}
}

func statusFromBool(ok bool) string {
	if ok {
		return models.VisualReadinessReady
	}
	return models.VisualReadinessMissing
}

func businessFlowNodeCount(view *StageViewDTO) int {
	if view == nil || view.BusinessFlow == nil {
		return 0
	}
	return len(view.BusinessFlow.Nodes)
}

func verdictStatus(view *StageViewDTO) string {
	if view == nil || view.IntegrationVerdict == nil {
		return models.VisualReadinessMissing
	}
	return view.IntegrationVerdict.Status
}

func rollbackScopeCount(view *StageViewDTO) int {
	if view == nil || view.RollbackSnapshot == nil {
		return 0
	}
	return len(view.RollbackSnapshot.Scopes)
}

func runtimeCapabilityGateStatus(view *StageViewDTO) string {
	if view == nil {
		return models.VisualReadinessMissing
	}
	if view.RuntimeCapabilityError != nil {
		return models.VisualReadinessBlocked
	}
	if len(view.RuntimeCapabilities) == 0 {
		return models.VisualReadinessMissing
	}
	for _, item := range view.RuntimeCapabilities {
		if (item.TaskType == "image_understanding" || item.TaskType == "image_generation") && !item.Available {
			return models.VisualReadinessBlocked
		}
	}
	return models.VisualReadinessReady
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
