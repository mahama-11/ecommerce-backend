package visualworkflow

import (
	"errors"
	"fmt"
	"strings"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

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
		if err := s.refreshIntentInputManifest(orgID, sessionID, nil, nil); err != nil {
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
		selected := decision.Decision == "keep" || decision.Decision == "replace" || decision.Decision == "crop"
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
	if err := s.refreshIntentInputManifest(orgID, sessionID, req.DriftControls, nil); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) refreshIntentInputManifest(orgID, sessionID string, driftControls map[string]any, promptVariables map[string]any) error {
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
	intent.Selections = mergeIntentSelections(intentSelectionsFromElements(elements), fixedPromptQuestionSelections(intent.Selections))
	if intent.Requirements == nil {
		intent.Requirements = map[string]any{}
	}
	if driftControls != nil {
		intent.Requirements["attribute_drift"] = sanitizeGenerationManifestValue(driftControls)
	}
	if intent.Metadata == nil {
		intent.Metadata = map[string]any{}
	}
	inputManifest := buildIntentFusionInputManifest(intent.Source.SourceReferences, intent.Selections, elements, promptVariables)
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

func fixedPromptQuestionSelections(selections []IntentElementDTO) []IntentElementDTO {
	out := make([]IntentElementDTO, 0, len(selections))
	for _, selection := range selections {
		metadata := selection.Metadata
		slot := metadataString(metadata, "prompt_slot")
		if slot == "" && strings.HasPrefix(selection.ElementID, "fixed:") {
			slot = strings.TrimPrefix(selection.ElementID, "fixed:")
			if metadata == nil {
				metadata = map[string]any{}
			}
			metadata["prompt_slot"] = slot
		}
		if slot == "" || strings.TrimSpace(selection.Decision) == "" {
			continue
		}
		if fmt.Sprint(metadata["fixed_prompt_question"]) != "true" && !strings.HasPrefix(selection.ElementID, "fixed:") {
			continue
		}
		selection.Metadata = metadata
		if strings.TrimSpace(selection.ElementKey) == "" {
			selection.ElementKey = slot
		}
		out = append(out, selection)
	}
	return out
}

func mergeIntentSelections(primary []IntentElementDTO, fixed []IntentElementDTO) []IntentElementDTO {
	out := make([]IntentElementDTO, 0, len(primary)+len(fixed))
	seen := map[string]bool{}
	selectionKey := func(selection IntentElementDTO) string {
		if slot := metadataString(selection.Metadata, "prompt_slot"); slot != "" {
			return "slot:" + slot
		}
		if strings.TrimSpace(selection.ElementID) != "" {
			return "element:" + strings.TrimSpace(selection.ElementID)
		}
		return "key:" + strings.TrimSpace(selection.ElementKey)
	}
	for _, selection := range primary {
		key := selectionKey(selection)
		if key == "key:" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, selection)
	}
	for _, selection := range fixed {
		key := selectionKey(selection)
		if key == "key:" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, selection)
	}
	return out
}

func buildIntentFusionInputManifest(sources []IntentSourceReferenceDTO, selections []IntentElementDTO, elements []models.EcommerceVisualDeconstructionElement, promptVariables map[string]any) map[string]any {
	_ = promptVariables
	skuFacts := make([]map[string]any, 0)
	referenceStrategies := make([]map[string]any, 0)
	rawSkuFactCount := 0
	rawReferenceStrategyCount := 0
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
			rawSkuFactCount++
			if promptUsableDeconstructionElement(elements[i], entry) {
				skuFacts = append(skuFacts, entry)
			}
		case "reference":
			rawReferenceStrategyCount++
			if promptUsableDeconstructionElement(elements[i], entry) {
				referenceStrategies = append(referenceStrategies, entry)
			}
		}
	}
	if len(skuFacts) == 0 {
		skuFacts = append(skuFacts, promptFallbackIntentEntry("sku", sources, rawSkuFactCount))
	}
	if len(referenceStrategies) == 0 {
		referenceStrategies = append(referenceStrategies, promptFallbackIntentEntry("reference", sources, rawReferenceStrategyCount))
	}
	ready := len(sources) >= 2 && len(selections) > 0
	readiness := "blocked"
	blockers := []ReadinessBlocker{}
	if ready {
		readiness = "ready"
	} else {
		if len(sources) < 2 {
			blockers = append(blockers, ReadinessBlocker{Code: "DUAL_TRACK_SOURCE_REQUIRED", Target: "source_references", Message: "SKU and reference images are required before image-plan composition."})
		}
		if len(selections) == 0 {
			blockers = append(blockers, ReadinessBlocker{Code: "ATTENTION_DECISION_REQUIRED", Target: "intent_input", Message: "Four prep choices are required before image-plan composition."})
		}
	}
	safeSelections := sanitizeIntentSelectionsForPrompt(selections)
	return map[string]any{
		"schema_version":               "visual-intent-input.v1",
		"source_references":            sources,
		"selections":                   safeSelections,
		"selection_count":              len(safeSelections),
		"sku_facts":                    skuFacts,
		"sku_fact_count":               len(skuFacts),
		"raw_sku_fact_count":           rawSkuFactCount,
		"reference_strategies":         referenceStrategies,
		"reference_strategy_count":     len(referenceStrategies),
		"raw_reference_strategy_count": rawReferenceStrategyCount,
		"decision_tree":                decisionTreeProjection(selections),
		"readiness":                    readiness,
		"blockers":                     blockers,
		"requires_prompt_diff":         true,
	}
}

func promptFallbackIntentEntry(role string, sources []IntentSourceReferenceDTO, rawCount int) map[string]any {
	description := "按当前已上传图片直接生成出图方案；图片理解结果不足时，仅使用可见商品轮廓、参考素材氛围和用户已确认的取舍选择，不再要求额外确认。"
	label := "图片输入"
	elementType := "reference_strategy"
	elementKey := "fallback_reference_strategy"
	if role == "sku" {
		label = "SKU 图片输入"
		elementType = "product_fact"
		elementKey = "fallback_sku_fact"
		description = "按当前 SKU 图片直接生成出图方案；保持商品主体完整清晰，无法确认的细节按常规电商商品图处理。"
	} else if role == "reference" {
		label = "参考素材输入"
		description = "按当前参考素材直接生成出图方案；只参考整体氛围、场景、光线和构图关系，不强制复刻不清晰细节。"
	}
	sourceReferenceID := ""
	for _, source := range sources {
		if strings.TrimSpace(source.Role) == role {
			sourceReferenceID = source.SourceReferenceID
			break
		}
	}
	return map[string]any{
		"element_id":          "fallback:" + role,
		"element_type":        elementType,
		"element_key":         elementKey,
		"label":               label,
		"source_role":         role,
		"source_reference_id": sourceReferenceID,
		"decision":            "keep",
		"confidence":          0,
		"readiness":           "fallback",
		"raw_element_count":   rawCount,
		"value": map[string]any{
			"description": description,
			"summary":     description,
		},
	}
}

func sanitizeIntentSelectionsForPrompt(selections []IntentElementDTO) []IntentElementDTO {
	out := make([]IntentElementDTO, 0, len(selections))
	for _, selection := range selections {
		if promptComposerValueText(selection.Value) == "" {
			selection.Value = map[string]any{"description": ""}
		}
		out = append(out, selection)
	}
	return out
}

func promptCompositionFromIntentFusion(intent *IntentSpecDTO, manifest map[string]any) map[string]any {
	if intent == nil || manifest == nil {
		return map[string]any{}
	}
	slotDecisions := promptComposerSlotDecisions(intent.Selections)
	skuEntries := filterPromptComposerAnalysisEntries(manifestEntries(manifest["sku_facts"]), "sku", slotDecisions)
	referenceEntries := filterPromptComposerAnalysisEntries(manifestEntries(manifest["reference_strategies"]), "reference", slotDecisions)
	skuText := promptComposerAnalysisText("SKU 解析结果", skuEntries)
	referenceText := promptComposerAnalysisText("参考素材解析结果", referenceEntries)
	selectionText := promptComposerSelectionText(intent.Selections)
	biasText := promptComposerBiasText(intent.Requirements["attribute_drift"])
	objectiveText := promptComposerObjectiveText(slotDecisions, skuText, referenceText)
	negativeText := promptComposerNegativeText(slotDecisions)
	sections := []map[string]any{}
	for _, section := range []struct{ title, text string }{
		{"出图目标", objectiveText},
		{"SKU 解析结果", skuText},
		{"参考素材解析结果", referenceText},
		{"四问选择", selectionText},
		{"侧重配置", biasText},
		{"必须避免", negativeText},
	} {
		if strings.TrimSpace(section.text) != "" {
			sections = append(sections, map[string]any{"title": section.title, "text": section.text})
		}
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		parts = append(parts, fmt.Sprintf("%s：%s", section["title"], section["text"]))
	}
	return map[string]any{
		"objective_text":          objectiveText,
		"sku_analysis_text":       skuText,
		"reference_analysis_text": referenceText,
		"selection_intent_text":   selectionText,
		"bias_intent_text":        biasText,
		"negative_prompt_text":    negativeText,
		"prompt_sections":         sections,
		"composed_prompt_text":    strings.TrimSpace(strings.Join(compactUniqueStrings(parts), "\n")),
	}
}
