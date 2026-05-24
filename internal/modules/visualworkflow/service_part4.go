package visualworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
)

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
		ProviderCode:   deconstructionProviderCode(decodeObject(item.Metadata)),
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

const sharedImageUnderstandingPrompt = `你是电商商品图像理解引擎。请解析这张图片，只返回合法 JSON，不要 markdown，不要解释。

必须返回两个固定的结构性回答，字段必须存在：
1. 图片中的产品信息：写入 product_info，描述商品主体、品类、形状、颜色、材质、纹理、可见文字/Logo、关键卖点、遮挡/不确定信息。
2. 图片中的背景信息：写入 background_info，描述场景、背景物体、光影、构图、色彩氛围、风格、可替换/应保留的背景元素。

可以额外发散补充 additional_observations，但不得省略 product_info 和 background_info。
返回格式：{"deconstruction_elements":[{"source_role":"sku|reference","source_reference_id":"SOURCE_REFERENCE_ID","element_type":"product_fact","element_key":"product_info","label":"图片中的产品信息","value":{"description":"...","attributes":["..."],"uncertainty":["..."]},"confidence":0.0,"readiness":"ready|needs_review"},{"source_role":"sku|reference","source_reference_id":"SOURCE_REFERENCE_ID","element_type":"background","element_key":"background_info","label":"图片中的背景信息","value":{"description":"...","scene":"...","lighting":"...","composition":"...","style":"...","keep":["..."],"replaceable":["..."]},"confidence":0.0,"readiness":"ready|needs_review"}],"additional_observations":[{"element_key":"...","label":"...","value":{"description":"..."},"confidence":0.0}]}。
请使用请求中提供的 source_reference_id；如果无法判断某项，请保留字段并在 uncertainty 中说明。`

func visualDeconstructionUnderstandingPrompt() string {
	return sharedImageUnderstandingPrompt
}

func visualDeconstructionUnderstandingPromptForSource(sourceRole, sourceReferenceID string) string {
	role := strings.ToLower(strings.TrimSpace(sourceRole))
	if role != "sku" && role != "reference" {
		role = "reference"
	}
	sourceReferenceID = strings.TrimSpace(sourceReferenceID)
	if sourceReferenceID == "" {
		sourceReferenceID = "当前 source_reference_id"
	}
	prompt := strings.ReplaceAll(sharedImageUnderstandingPrompt, "sku|reference", role)
	prompt = strings.ReplaceAll(prompt, "SOURCE_REFERENCE_ID", sourceReferenceID)
	return prompt + fmt.Sprintf("\n\n本次只解析当前这一张图片：source_role 必须填写 %s，source_reference_id 必须填写 %s。返回 JSON 中不要照抄占位符。", role, sourceReferenceID)
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
	primarySourceRole := ""
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
		primarySourceRole = role
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
		if strings.TrimSpace(fmt.Sprint(asset["source_url"])) == "" && strings.TrimSpace(src.StorageKey) != "" {
			asset["storage_key"] = src.StorageKey
		}
		hasUsableSource := strings.TrimSpace(fmt.Sprint(asset["source_url"])) != "" || strings.TrimSpace(fmt.Sprint(asset["storage_key"])) != ""
		if !hasUsableSource {
			continue
		}
		sourceAssets = append(sourceAssets, asset)
	}
	if len(sourceAssets) == 0 {
		return nil, fmt.Errorf("deconstruction runtime requires at least one resolved source asset")
	}
	understandingPrompt := visualDeconstructionUnderstandingPromptForSource(primarySourceRole, item.SourceReferenceID)
	return map[string]any{
		"input_mode":                  "dual_track_sources",
		"source_role_output_required": true,
		"source_reference_id":         item.SourceReferenceID,
		"source_reference_ids":        businessManifest["source_reference_ids"],
		"source_references":           businessManifest["source_references"],
		"source_asset_ids":            sourceAssetIDs,
		"source_assets":               sourceAssets,
		"prompt_snapshot": map[string]any{
			"user_prompt": understandingPrompt,
		},
		"params_snapshot": map[string]any{
			"schema_version":       "ecommerce.visual_deconstruction.runtime.v1",
			"required_tracks":      []string{"sku", "reference"},
			"requested_elements":   businessManifest["requested_elements"],
			"output_schema":        "visual-deconstruction.v2",
			"understanding_prompt": understandingPrompt,
			"required_fixed_keys":  []string{"product_info", "background_info"},
			"optional_extra_key":   "additional_observations",
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
				primary = &sources[i]
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
		cleaned = map[string]any{}
	}
	if provider := deconstructionProviderCode(raw); provider != "" {
		cleaned["provider_code"] = provider
	}
	return cleaned
}

func generationProviderCode(metadata map[string]any) string {
	provider := strings.TrimSpace(metadataString(metadata, "provider_code"))
	if provider == "" {
		provider = strings.TrimSpace(metadataString(metadata, "generation_provider_code"))
	}
	if provider == "" {
		if uiConfig, ok := metadata["ui_execution_config"].(map[string]any); ok {
			provider = strings.TrimSpace(metadataString(uiConfig, "generation_provider_code"))
			if provider == "" {
				if providerConfig, ok := uiConfig["provider_config"].(map[string]any); ok {
					provider = strings.TrimSpace(metadataString(providerConfig, "generation_provider_code"))
				}
			}
		}
	}
	if provider == "" {
		if execConfig, ok := metadata["execution_config"].(map[string]any); ok {
			provider = strings.TrimSpace(metadataString(execConfig, "generation_provider_code"))
			if provider == "" {
				if providerConfig, ok := execConfig["provider_config"].(map[string]any); ok {
					provider = strings.TrimSpace(metadataString(providerConfig, "generation_provider_code"))
				}
			}
		}
	}
	switch provider {
	case "", "auto", "default":
		return ""
	case "comfyui_bridge", "gemini_image_generation", "minimax_image_generation", "volcengine":
		return provider
	default:
		return ""
	}
}

func deconstructionProviderCode(metadata map[string]any) string {
	provider := strings.TrimSpace(metadataString(metadata, "provider_code"))
	switch provider {
	case "", "auto", "default":
		return ""
	case "comfyui_bridge", "gemini_visual_understanding":
		return provider
	default:
		return ""
	}
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
	if message := userFacingVisualRuntimeMessage(input.StageMessage); message != "" {
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
	if message := userFacingVisualRuntimeMessage(input.ErrorMessage); message != "" {
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
	item.StageMessage = defaultString(userFacingVisualRuntimeMessage(input.StageMessage), visualResultStageMessage(item.Status))
	item.ErrorCode = strings.TrimSpace(input.ErrorCode)
	item.ErrorMessage = userFacingVisualRuntimeMessage(input.ErrorMessage)
	applyVisualDeconstructionCompletionTimestamps(item)

	sourceRefs, err := s.repo.ListSourceReferences(item.OrganizationID, item.SessionID)
	if err != nil {
		return nil, err
	}
	sourceIndex := deconstructionResultSourceIndex(sourceRefs)
	if len(elementInputs) == 0 && item.Status == models.VisualDeconstructionStatusCompleted {
		elementInputs = fallbackVisualResultElementsFromProviderText(input, sourceRefs)
	}
	if item.Status == models.VisualDeconstructionStatusCompleted {
		elementInputs = projectSingleImageUnderstandingElements(item, elementInputs, sourceIndex)
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

func userFacingVisualRuntimeMessage(message string) string {
	text := strings.TrimSpace(message)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "traceback") || strings.Contains(lower, "provider_submit_failed") || strings.Contains(lower, "python") || strings.Contains(lower, "comfyui bridge request failed") || strings.Contains(lower, "gemini") {
		return "图片识别服务暂时不可用，请稍后重试；系统不会用假结果继续下一步。"
	}
	return text
}
