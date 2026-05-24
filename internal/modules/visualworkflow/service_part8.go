package visualworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

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
	fanoutRunID := buildID("gfr")
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
		fanoutTaskID := fmt.Sprintf("%s:%s:%02d", fanoutID, fanoutRunID, slotIndex+1)
		providerConfig, _ := sanitizeGenerationManifestValue(req.ProviderConfig).(map[string]any)
		providerConfig = mergeObjectMaps(providerConfig, fanoutTemplateProviderConfig(templateID))
		basePrompt := promptPlanRuntimeText(session, nil, &promptPlan)
		detailRequirement := defaultString(pair.detailRequirement, fanoutTemplateDetailRequirement(templateID))
		metadata := mergeObjectMaps(req.Metadata, map[string]any{
			"source":               "sandbox_generation_fanout",
			"idempotency_key":      fmt.Sprintf("generation-fanout:%s:%s:%s:%02d:%s:%s", session.ID, fanoutID, fanoutRunID, slotIndex+1, sourceID, templateID),
			"fanout_id":            fanoutID,
			"fanout_run_id":        fanoutRunID,
			"fanout_task_id":       fanoutTaskID,
			"fanout_index":         slotIndex,
			"fanout_total":         total,
			"source_asset_id":      sourceID,
			"template_id":          strings.TrimSpace(templateID),
			"template_version_id":  templateVersionID,
			"scene_tag":            pair.sceneTag,
			"detail_requirement":   detailRequirement,
			"negative_requirement": pair.negativeRequirement,
			"prompt_composition":   buildFanoutPromptComposition(basePrompt, pair.sceneTag, detailRequirement, pair.negativeRequirement, req.PromptVariables),
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
