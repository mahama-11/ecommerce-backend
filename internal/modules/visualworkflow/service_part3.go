package visualworkflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/promptcenter"
	"ecommerce-service/internal/platform"

	"gorm.io/gorm"
)

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

	if err := s.refreshIntentInputManifest(orgID, sessionID, req.DriftControls, req.PromptVariables); err != nil {
		session, getErr := s.repo.GetSession(orgID, sessionID)
		if getErr != nil {
			return nil, err
		}
		return s.persistPromptPlannerBlocked(session, "INTENT_INPUT_REQUIRED", err.Error())
	}
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	plan := decodePromptPlan(session.PromptPlanJSON, session)
	if strings.TrimSpace(plan.Status) != "ready" {
		code := "INTENT_INPUT_REQUIRED"
		message := "Complete image analysis and four prep choices before generating the image plan."
		if len(plan.Blockers) > 0 {
			code = strings.TrimSpace(plan.Blockers[0].Code)
			message = strings.TrimSpace(plan.Blockers[0].Message)
		}
		return s.persistPromptPlannerBlocked(session, code, message)
	}
	if text, ok := plan.Variables["composed_prompt_text"].(string); !ok || strings.TrimSpace(text) == "" {
		return s.persistPromptPlannerBlocked(session, "PROMPT_COMPOSITION_REQUIRED", "Image plan composition is missing; regenerate after completing Prep choices.")
	}
	if strings.TrimSpace(plan.TemplateID) == "" {
		plan.TemplateID = strings.TrimSpace(req.TemplateID)
	}
	if strings.TrimSpace(plan.TemplateID) == "" {
		plan.TemplateID = strings.TrimSpace(session.TemplateID)
	}
	if strings.TrimSpace(plan.TemplateID) == "" {
		plan.TemplateID = "tpl_product_visual_generation_v1"
	}
	if strings.TrimSpace(plan.SceneType) == "" {
		plan.SceneType = strings.TrimSpace(req.Marketplace)
	}
	if strings.TrimSpace(plan.SceneType) == "" {
		plan.SceneType = "product_visual_generation"
	}
	if err := s.attachPromptCenterSnapshot(session, &plan); err != nil {
		return s.persistPromptPlannerBlocked(session, "PROMPT_SNAPSHOT_FAILED", safePlatformRuntimeJobCreateErrorMessage)
	}
	metadata := decodeObject(session.Metadata)
	metadata["prompt_planner"] = map[string]any{
		"status":          "completed",
		"stage":           "deterministic_composed",
		"progress":        100,
		"prompt_id":       plan.PromptID,
		"idempotency_key": strings.TrimSpace(req.IdempotencyKey),
		"source":          "deterministic_v1",
	}
	plan.Metadata = mergeObjectMaps(plan.Metadata, map[string]any{"source": "backend_intent_fusion", "planner_mode": "deterministic_v1", "updated_from": "generate_image_plan"})
	session.PromptPlanJSON = encodePromptPlan(&plan)
	session.Metadata = mustJSON(sanitizeGenerationManifestValue(metadata))
	session.CurrentStage = models.VisualWorkflowStagePrompt
	session.Status = models.VisualWorkflowStatusReady
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &PromptPlannerJobResponse{SessionID: session.ID, Status: "completed", Stage: "deterministic_composed", Progress: 100, IdempotencyKey: strings.TrimSpace(req.IdempotencyKey)}, nil
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
