package visualworkflow

import (
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/models"
)

func promptComposerObjectiveText(slotDecisions map[string]string, skuText, referenceText string) string {
	parts := []string{"保留 SKU 商品主体完整清晰；只更换或重建背景，不改变商品品类、轮廓、颜色、材质、Logo/文字和关键结构。"}
	if slotDecisions["sku_product"] == "keep" || slotDecisions["sku_product"] == "replace" || slotDecisions["sku_product"] == "" {
		if strings.TrimSpace(skuText) != "" {
			parts = append(parts, "主体依据仅来自 SKU 商品图："+strings.TrimSpace(skuText))
		} else {
			parts = append(parts, "主体依据仅来自 SKU 商品图；以输入图中的可见商品轮廓和细节为准。")
		}
	}
	if slotDecisions["reference_background"] == "keep" || slotDecisions["reference_background"] == "replace" {
		if strings.TrimSpace(referenceText) != "" {
			parts = append(parts, "背景/光线/构图依据来自参考素材："+strings.TrimSpace(referenceText))
		} else {
			parts = append(parts, "背景/光线/构图参考上传素材的整体氛围，不复刻参考图中的商品主体。")
		}
	} else if slotDecisions["sku_background"] == "keep" || slotDecisions["sku_background"] == "replace" {
		parts = append(parts, "沿用 SKU 原图背景关系，保持电商商品图干净可售卖。")
	} else {
		parts = append(parts, "背景使用干净、有层次、无杂物的电商场景；突出商品，不抢主体。")
	}
	parts = append(parts, "画面必须是单一主商品，主体居中或稳定占据视觉中心，边缘完整，不裁切、不融化、不变形。")
	return strings.Join(compactUniqueStrings(parts), " ")
}

func promptComposerNegativeText(slotDecisions map[string]string) string {
	parts := []string{"不要丢失 SKU 主体；不要改变商品类型；不要生成杂乱背景；不要多出额外同类商品；不要文字水印、畸变、模糊、裁切、遮挡。"}
	if slotDecisions["sku_background"] == "drop" {
		parts = append(parts, "不要保留 SKU 原图背景。")
	}
	if slotDecisions["reference_product"] == "drop" {
		parts = append(parts, "不要把参考素材中的产品、人物或无关物体带入画面。")
	}
	if slotDecisions["reference_background"] == "drop" {
		parts = append(parts, "不要采用参考素材背景风格。")
	}
	return strings.Join(compactUniqueStrings(parts), " ")
}

func manifestEntries(raw any) []map[string]any {
	out := []map[string]any{}
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		for _, item := range typed {
			if entry, ok := item.(map[string]any); ok {
				out = append(out, entry)
			}
		}
	}
	return out
}

func promptComposerSlotDecisions(selections []IntentElementDTO) map[string]string {
	decisions := map[string]string{}
	for _, selection := range selections {
		slot := metadataString(selection.Metadata, "prompt_slot")
		if slot == "" {
			continue
		}
		decision := strings.ToLower(strings.TrimSpace(selection.Decision))
		if decision == "" {
			continue
		}
		decisions[slot] = decision
	}
	return decisions
}

func filterPromptComposerAnalysisEntries(entries []map[string]any, role string, slotDecisions map[string]string) []map[string]any {
	if len(entries) == 0 || len(slotDecisions) == 0 {
		return entries
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		dimension := promptComposerEntryDimension(entry)
		if dimension != "" && slotDecisions[role+"_"+dimension] == "drop" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func promptComposerEntryDimension(entry map[string]any) string {
	elementType := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["element_type"])))
	elementKey := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["element_key"])))
	label := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["label"])))
	combined := strings.Join([]string{elementType, elementKey, label}, " ")
	if strings.Contains(combined, "background") || strings.Contains(combined, "scene") || strings.Contains(combined, "style") || strings.Contains(combined, "lighting") || strings.Contains(combined, "composition") || strings.Contains(combined, "reference_strategy") {
		return "background"
	}
	if strings.Contains(combined, "product") || strings.Contains(combined, "sku") || strings.Contains(combined, "visual_description") || strings.Contains(combined, "product_fact") || strings.Contains(combined, "material") || strings.Contains(combined, "geometry") {
		return "product"
	}
	return ""
}

func promptComposerAnalysisText(prefix string, entries []map[string]any) string {
	parts := []string{}
	for _, entry := range entries {
		label := strings.TrimSpace(fmt.Sprint(entry["label"]))
		value := promptComposerValueText(entry["value"])
		line := strings.TrimSpace(strings.Join(compactUniqueStrings([]string{label, value}), "："))
		if line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(compactUniqueStrings(parts), "；"))
}

func promptComposerValueText(raw any) string {
	valueMap, ok := raw.(map[string]any)
	if !ok {
		text := strings.TrimSpace(fmt.Sprint(raw))
		if promptComposerLooksLikeInternalJSON(text) {
			return ""
		}
		return text
	}
	for _, key := range []string{"description", "summary", "text", "provider_text", "style", "shape", "material", "color", "value", "label"} {
		text := strings.TrimSpace(fmt.Sprint(valueMap[key]))
		if text == "" || text == "<nil>" || promptComposerLooksLikeInternalJSON(text) {
			continue
		}
		return text
	}
	return ""
}

func promptComposerLooksLikeInternalJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "deconstruction_elements") || strings.Contains(lower, "source_reference_id") || strings.Contains(lower, "provider_job_id")
}

func promptUsableDeconstructionElement(element models.EcommerceVisualDeconstructionElement, entry map[string]any) bool {
	readiness := strings.ToLower(strings.TrimSpace(element.Readiness))
	if readiness == "failed" || readiness == "blocked" || readiness == "invalid" {
		return false
	}
	if strings.TrimSpace(promptComposerValueText(entry["value"])) == "" {
		return false
	}
	role := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["source_role"])))
	if role == "reference" {

		return true
	}

	if readiness == "needs_review" || (element.Confidence > 0 && element.Confidence < 0.6) {
		return false
	}
	return true
}

func promptComposerSelectionText(selections []IntentElementDTO) string {
	parts := []string{}
	for _, selection := range selections {
		metadata := selection.Metadata
		slot := metadataString(metadata, "prompt_slot")
		if slot == "" && fmt.Sprint(metadata["fixed_prompt_question"]) != "true" {
			continue
		}
		label := strings.TrimSpace(selection.Label)
		if label == "" {
			label = strings.TrimSpace(selection.ElementKey)
		}
		decision := strings.TrimSpace(selection.Decision)
		switch slot {
		case "sku_product":
			if decision == "keep" || decision == "replace" {
				parts = append(parts, "保留 SKU 原图产品："+label)
			} else if decision == "drop" {
				parts = append(parts, "不要使用 SKU 原图产品："+label)
			}
		case "sku_background":
			if decision == "keep" || decision == "replace" {
				parts = append(parts, "保留 SKU 原图背景："+label)
			} else if decision == "drop" {
				parts = append(parts, "不要使用 SKU 原图背景："+label)
			}
		case "reference_product":
			if decision == "keep" || decision == "replace" {
				parts = append(parts, "参考素材产品进入画面："+label)
			} else if decision == "drop" {
				parts = append(parts, "不要使用参考素材产品："+label)
			}
		case "reference_background":
			if decision == "keep" || decision == "replace" {
				parts = append(parts, "采用参考素材背景风格："+label)
			} else if decision == "drop" {
				parts = append(parts, "不要使用参考素材背景："+label)
			}
		default:
			if decision != "" && label != "" {
				parts = append(parts, decision+"："+label)
			}
		}
	}
	return strings.TrimSpace(strings.Join(compactUniqueStrings(parts), "；"))
}

func promptComposerBiasText(raw any) string {
	controls, _ := raw.(map[string]any)
	if controls == nil {
		return ""
	}
	skuWeight := intFromAny(controls["sku_weight"], intFromAny(controls["sku_bias"], -1))
	referenceWeight := intFromAny(controls["reference_weight"], intFromAny(controls["reference_bias"], -1))
	parts := []string{}
	if skuWeight >= 0 {
		parts = append(parts, fmt.Sprintf("侧重 SKU 原图 %d%%", skuWeight))
	}
	if referenceWeight >= 0 {
		parts = append(parts, fmt.Sprintf("侧重参考素材 %d%%", referenceWeight))
	}
	return strings.Join(parts, "，")
}

func promptPlanFromIntentFusion(session *models.EcommerceVisualWorkflowSession, intent *IntentSpecDTO, manifest map[string]any) *PromptPlanDTO {
	status := "blocked"
	blockers := []ReadinessBlocker{}
	ready := strings.TrimSpace(fmt.Sprint(manifest["readiness"])) == "ready"
	if ready {
		status = "ready"
	} else if rawBlockers, ok := manifest["blockers"].([]ReadinessBlocker); ok {
		blockers = rawBlockers
	} else {
		blockers = []ReadinessBlocker{{Code: "INTENT_INPUT_REQUIRED", Target: "prompt_plan", Message: "Backend intent fusion input is not ready."}}
	}
	variables := map[string]any{
		"intent_input_manifest": manifest,
		"attribute_drift":       intent.Requirements["attribute_drift"],
	}
	if ready {
		variables = mergeObjectMaps(variables, promptCompositionFromIntentFusion(intent, manifest))
	}
	plan := &PromptPlanDTO{
		SchemaVersion:     promptPlanSchemaVersion,
		Status:            status,
		SceneType:         intent.SceneType,
		TemplateID:        strings.TrimSpace(session.TemplateID),
		TemplateVersionID: strings.TrimSpace(session.TemplateVersionID),
		Variables:         variables,
		SourceAssets:      promptPlanSourceAssetsFromIntentSources(intent.Source.SourceReferences),
		Blockers:          blockers,
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
	case "keep", "replace", "drop", "crop", "needs_review":
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
	if nextStatus := visualWorkflowStatusForGeneration(version.Status); nextStatus != "" {
		session.Status = nextStatus
	} else if version.Status == "contract_needed" || version.Status == "blocked" {
		session.Status = models.VisualWorkflowStatusBlocked
	}
	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return &version, nil
}

func fanoutTemplateProviderConfig(templateID string) map[string]any {
	switch strings.TrimSpace(templateID) {
	case "industrial-poster":
		return map[string]any{"resolution_id": "768x1024-poster"}
	case "lifestyle-scene":
		return map[string]any{"resolution_id": "1365x768-wide"}
	case "amazon-hero":
		return map[string]any{"resolution_id": "1024-square"}
	default:
		return map[string]any{}
	}
}

func fanoutTemplateDetailRequirement(templateID string) string {
	switch strings.TrimSpace(templateID) {
	case "amazon-hero":
		return "模板：amazon-hero；Amazon 主图；纯白背景，主体居中，完整商品轮廓，禁止场景道具。"
	case "industrial-poster":
		return "模板：industrial-poster；工业风营销海报；深色背景，强对比光影，海报式纵向构图，突出质感和卖点。"
	case "lifestyle-scene":
		return "模板：lifestyle-scene；真实使用场景；横向生活方式构图，桌面/居家环境，强调使用氛围与代入感。"
	default:
		return ""
	}
}
