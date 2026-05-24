package visualworkflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"ecommerce-service/internal/models"
)

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
		models.VisualSourceStatusContractNeeded,
		models.VisualSourceStatusArchived:
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
