package visualworkflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"ecommerce-service/internal/billinggate"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/promptcenter"
)

func chargeContextID(ctx *billinggate.Context) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.ChargeSessionID)
}

func generationRuntimeRouteSnapshot(version *GenerationVersionDTO) map[string]any {
	providerCode := generationProviderCode(versionMetadata(version))
	preferred := []string{"comfyui_bridge", "gemini_image_generation", "minimax_image_generation", "volcengine"}
	if providerCode != "" {
		preferred = append([]string{providerCode}, preferred...)
	}
	route := map[string]any{
		"objective":           "quality",
		"preferred_providers": compactUniqueStrings(preferred),
	}
	if version == nil || len(version.Metadata) == 0 {
		return route
	}
	if value := strings.TrimSpace(metadataString(version.Metadata, "objective")); value != "" {
		route["objective"] = value
	}
	if raw, ok := version.Metadata["preferred_providers"]; ok {
		switch typed := raw.(type) {
		case []string:
			if providers := compactUniqueStrings(typed); len(providers) > 0 {
				route["preferred_providers"] = providers
			}
		case []any:
			providers := make([]string, 0, len(typed))
			for _, item := range typed {
				providers = append(providers, strings.TrimSpace(fmt.Sprint(item)))
			}
			if providers = compactUniqueStrings(providers); len(providers) > 0 {
				route["preferred_providers"] = providers
			}
		case string:
			parts := strings.Split(typed, ",")
			if providers := compactUniqueStrings(parts); len(providers) > 0 {
				route["preferred_providers"] = providers
			}
		}
	}
	return route
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
	referenceSourceAssetIDs := make([]string, 0, len(bindings))
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
		if strings.Contains(strings.ToLower(strings.TrimSpace(binding.Slot)), "reference") {
			referenceSourceAssetIDs = append(referenceSourceAssetIDs, asset.ID)
		}
		sourceAssets = append(sourceAssets, map[string]any{
			"id":          asset.ID,
			"storage_key": asset.StorageKey,
			"mime_type":   asset.MimeType,
			"width":       asset.Width,
			"height":      asset.Height,
		})
	}
	if fanoutSourceID := metadataString(version.Metadata, "source_asset_id"); fanoutSourceID != "" {
		if ids, assets := s.runtimeSourceAssetsForIDs(session.OrganizationID, fanoutFirstSourceAssetIDs(fanoutSourceID, referenceSourceAssetIDs)); len(ids) > 0 {
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
	referenceSourceAssetIDs := make([]string, 0, len(promptPlan.SourceAssets))
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
		if strings.EqualFold(strings.TrimSpace(binding.Role), "reference") {
			referenceSourceAssetIDs = append(referenceSourceAssetIDs, asset.ID)
		}
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
		if ids, assets := s.runtimeSourceAssetsForIDs(session.OrganizationID, fanoutFirstSourceAssetIDs(fanoutSourceID, referenceSourceAssetIDs)); len(ids) > 0 {
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

func fanoutFirstSourceAssetIDs(primary string, existing []string) []string {
	ids := []string{strings.TrimSpace(primary)}
	ids = append(ids, existing...)
	return compactUniqueStrings(ids)
}

func assetUsableForGeneration(width, height int) bool {

	if width <= 0 || height <= 0 {
		return true
	}
	return width >= 14 && height >= 14
}

func fanoutSlotPrompt(base string, metadata map[string]any) string {
	if composed := promptCompositionTextFromMetadata(metadata); composed != "" {
		return composed
	}
	parts := []string{strings.TrimSpace(base)}
	if scene := metadataString(metadata, "scene_tag"); scene != "" {
		parts = append(parts, "Image type / scene: "+scene)
	}
	if detail := metadataString(metadata, "detail_requirement"); detail != "" {
		parts = append(parts, "Slot-specific detail requirements: "+detail)
	}
	return strings.TrimSpace(strings.Join(compactUniqueStrings(parts), "\n"))
}

func promptCompositionTextFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	composition, _ := metadata["prompt_composition"].(map[string]any)
	if composition == nil {
		return ""
	}
	return metadataString(composition, "composed_prompt_text")
}

func promptCompositionNegativeTextFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	composition, _ := metadata["prompt_composition"].(map[string]any)
	if composition == nil {
		return ""
	}
	return metadataString(composition, "negative_prompt_text")
}

func buildFanoutPromptComposition(base, sceneTag, detailRequirement, negativeRequirement string, promptVariables map[string]any) map[string]any {
	composer, _ := promptVariables["prompt_composer"].(map[string]any)
	templateText := strings.Join(compactUniqueStrings([]string{metadataString(composer, "template_prompt_text"), fanoutDiversityInstruction(sceneTag, detailRequirement)}), "\n")
	if templateText == "" {
		templateText = strings.Join(compactUniqueStrings([]string{strings.TrimSpace(detailRequirement), fanoutDiversityInstruction(sceneTag, detailRequirement)}), "\n")
	}
	diyText := metadataString(composer, "diy_prompt_text")
	negativeText := strings.Join(compactUniqueStrings([]string{strings.TrimSpace(negativeRequirement), metadataString(composer, "negative_prompt_text")}), "\n")
	sections := []map[string]any{}
	for _, section := range []struct{ title, text string }{
		{"基础出图要求", base},
		{"素材模板结果", templateText},
		{"手动填入结果", diyText},
	} {
		if strings.TrimSpace(section.text) != "" {
			sections = append(sections, map[string]any{"title": section.title, "text": strings.TrimSpace(section.text)})
		}
	}
	parts := []string{}
	for _, section := range sections {
		parts = append(parts, fmt.Sprintf("%s：%s", section["title"], section["text"]))
	}
	if strings.TrimSpace(sceneTag) != "" {
		parts = append(parts, "图片类型/场景："+strings.TrimSpace(sceneTag))
	}
	return map[string]any{
		"template_prompt_text": templateText,
		"diy_prompt_text":      diyText,
		"negative_prompt_text": negativeText,
		"prompt_sections":      sections,
		"composed_prompt_text": strings.TrimSpace(strings.Join(compactUniqueStrings(parts), "\n")),
	}
}

func fanoutDiversityInstruction(sceneTag, detailRequirement string) string {
	scene := strings.TrimSpace(sceneTag)
	detail := strings.TrimSpace(detailRequirement)
	identifier := strings.TrimSpace(scene)
	if identifier == "" {
		identifier = detail
	}
	if identifier == "" {
		identifier = "当前槽位"
	}
	return fmt.Sprintf("强制差异化：当前槽位=%s；槽位要求=%s；不得与其他槽位构图相同，必须在背景、画幅、镜头距离、光线氛围和商品呈现方式上形成清晰差异。", identifier, defaultString(detail, identifier))
}

func fanoutNegativePrompt(base string, metadata map[string]any) string {
	negativeParts := []string{metadataString(metadata, "negative_requirement"), promptCompositionNegativeTextFromMetadata(metadata)}
	negative := strings.Join(compactUniqueStrings(negativeParts), "\n")
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
	if modelID := strings.TrimSpace(metadataString(providerConfig, "model_id")); modelID != "" {
		params["model"] = modelID
	}
	if providerCode := strings.TrimSpace(metadataString(providerConfig, "generation_provider_code")); providerCode != "" {
		params["generation_provider_code"] = providerCode
		if providerCode == "minimax_image_generation" {
			params["minimax_subject_type"] = "character"
		}
	}
	if negative := strings.TrimSpace(generationFirstNonEmpty(metadataString(providerConfig, "negative_prompt"), metadataString(providerConfig, "negative_requirement"), metadataString(providerConfig, "negative_prompt_text"), metadataString(providerConfig, "avoid"))); negative != "" {
		params["negative_prompt"] = negative
	}
	resolutionID, _ := providerConfig["resolution_id"].(string)
	switch strings.TrimSpace(resolutionID) {
	case "1024-square":
		params["width"] = 1024
		params["height"] = 1024
		params["resolution_id"] = "1024-square"
		params["aspect_ratio"] = "1:1"
	case "720-wide":
		params["width"] = 1280
		params["height"] = 720
		params["resolution_id"] = "720-wide"
		params["aspect_ratio"] = "16:9"
	case "768x1024-poster":
		params["width"] = 768
		params["height"] = 1024
		params["resolution_id"] = "768x1024-poster"
		params["aspect_ratio"] = "3:4"
	case "1365x768-wide":
		params["width"] = 1365
		params["height"] = 768
		params["resolution_id"] = "1365x768-wide"
		params["aspect_ratio"] = "16:9"
	}
	if width, height, ok := generationDimensionsFromProviderConfig(providerConfig); ok {
		params["width"] = width
		params["height"] = height
		params["aspect_ratio"] = generationAspectRatio(width, height)

		params["resolution_id"] = fmt.Sprintf("%dx%d", width, height)
	}
	if aspect := strings.TrimSpace(generationFirstNonEmpty(metadataString(providerConfig, "aspect_ratio"), metadataString(providerConfig, "aspectRatio"), metadataString(providerConfig, "ratio"))); aspect != "" {
		params["aspect_ratio"] = aspect
	}
	for _, key := range []string{"scene_tag", "detail_requirement", "negative_requirement"} {
		if text := metadataString(metadata, key); text != "" {
			params[key] = text
		}
	}
}

func generationDimensionsFromProviderConfig(providerConfig map[string]any) (int, int, bool) {
	if providerConfig == nil {
		return 0, 0, false
	}
	width := generationIntFromAny(providerConfig["width"])
	height := generationIntFromAny(providerConfig["height"])
	if width <= 0 || height <= 0 {
		if dims, ok := providerConfig["dimensions"].(string); ok {
			width, height = parseDimensionString(dims)
		}
	}
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func generationFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseDimensionString(value string) (int, int) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "×", "x"))
	parts := strings.Split(trimmed, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	width, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return width, height
}

func generationIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func generationAspectRatio(width, height int) string {
	if width <= 0 || height <= 0 {
		return "1:1"
	}
	gcd := intGCD(width, height)
	return fmt.Sprintf("%d:%d", width/gcd, height/gcd)
}

func intGCD(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func promptPlanRuntimeText(session *models.EcommerceVisualWorkflowSession, version *GenerationVersionDTO, promptPlan *PromptPlanDTO) string {
	if promptPlan == nil {
		return ""
	}
	for _, key := range []string{"composed_prompt_text", "final_prompt", "user_prompt", "positive_prompt", "generation_prompt", "prompt", "creative_brief"} {
		if text := metadataString(promptPlan.Metadata, key); strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if promptPlan.Variables != nil {
			if text, ok := promptPlan.Variables[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
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
