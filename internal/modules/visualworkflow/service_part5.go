package visualworkflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

func (s *Service) SaveGenerationVersionAsTemplate(userID, orgID, sessionID, versionID string, req SaveGenerationTemplateRequest) (*SaveGenerationTemplateResponse, error) {
	if s.workspaceRepo == nil {
		return nil, fmt.Errorf("workspace template repository is not configured")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user is required to save a template")
	}
	session, err := s.repo.GetSession(orgID, sessionID)
	if err != nil {
		return nil, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	idx := -1
	for i := range versions {
		if versions[i].VersionID == strings.TrimSpace(versionID) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	version := versions[idx]
	if version.Status != "completed" || len(version.ResultAssets) == 0 {
		return nil, fmt.Errorf("generation version must be completed with result assets before saving as template")
	}
	assetID := strings.TrimSpace(req.AssetID)
	if assetID == "" {
		assetID = strings.TrimSpace(version.SelectedResultAssetID)
	}
	if assetID == "" {
		return nil, fmt.Errorf("selected result asset is required before saving as template")
	}
	var selected ResultAssetDTO
	found := false
	for _, asset := range version.ResultAssets {
		if strings.TrimSpace(asset.AssetID) == assetID {
			selected = asset
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("asset_id is not a result asset of this generation version")
	}
	if _, err := s.assetRepo.FindAssetByID(orgID, assetID); err != nil {
		return nil, fmt.Errorf("selected result asset not found: %w", err)
	}
	now := time.Now().UTC()
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = fmt.Sprintf("%s %s V%d visual template", defaultString(session.SKUCode, "SKU"), defaultString(version.Stage, "generation"), idx+1)
	}
	scenario := strings.TrimSpace(req.Scenario)
	if scenario == "" {
		scenario = "Ecommerce SKU visual generation template saved from a completed Workshop result."
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = fmt.Sprintf("Saved from Visual Workflow session %s, generation version %s, asset %s.", session.ID, version.VersionID, assetID)
	}
	tags := []string{"visual-workflow", "generated-result", "ecommerce-v2", session.SKUCode}
	for _, tag := range req.Tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	record := repository.SavedTemplateRecord{
		ID:          buildID("tpl"),
		Platform:    "ecommerce",
		Tags:        dedupeStrings(tags),
		UsageCount:  "0",
		Favorite:    0,
		SavedAt:     now.Format(time.RFC3339),
		SourceType:  "visual_generation_result",
		SourceLabel: fmt.Sprintf("product=%s sku=%s session=%s version=%s asset=%s", session.ProductID, session.SKUCode, session.ID, version.VersionID, assetID),
		ZH:          repository.TemplateCopy{Title: title, Summary: summary, Scenario: scenario},
		EN:          repository.TemplateCopy{Title: title, Summary: summary, Scenario: scenario},
	}
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		record.ID = "tpl_" + shortStableHash(fmt.Sprintf("%s:%s:%s:%s", orgID, session.ID, version.VersionID, strings.TrimSpace(req.IdempotencyKey)))
	}
	items, err := s.workspaceRepo.SaveTemplate(repository.Scope{UserID: userID, OrgID: orgID}, record)
	if err != nil {
		return nil, err
	}
	metadata := mergeGenerationMetadata(version.Metadata, map[string]any{"saved_template": map[string]any{"template_id": record.ID, "asset_id": assetID, "saved_at": record.SavedAt}})
	version.Metadata = metadata
	version.UpdatedAt = now.Format(time.RFC3339Nano)
	versions[idx] = version
	if err := s.saveGenerationVersions(session, versions, visualWorkflowStatusForGeneration(version.Status)); err != nil {
		return nil, err
	}
	return &SaveGenerationTemplateResponse{SessionID: session.ID, VersionID: version.VersionID, ProductID: session.ProductID, SKUCode: session.SKUCode, SelectedResultAssetID: assetID, Template: record, SavedTemplates: items, GenerationVersion: version, AssetContentURL: selected.AssetContentURL}, nil
}

func (s *Service) HasGenerationVersion(versionID string) bool {
	if strings.TrimSpace(versionID) == "" {
		return false
	}
	_, _, _, err := s.findGenerationVersionByCallbackID(strings.TrimSpace(versionID))
	return err == nil
}

func (s *Service) lockGenerationVersionSession(callbackID string) (func(), error) {
	session, _, _, err := s.findGenerationVersionByCallbackID(strings.TrimSpace(callbackID))
	if err != nil {
		return nil, err
	}
	lockValue, _ := s.generationLocks.LoadOrStore(session.ID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock, nil
}

func (s *Service) findGenerationVersionByCallbackID(callbackID string) (*models.EcommerceVisualWorkflowSession, []GenerationVersionDTO, int, error) {
	callbackID = strings.TrimSpace(callbackID)
	if callbackID == "" {
		return nil, nil, -1, gorm.ErrRecordNotFound
	}
	if session, versions, idx, err := s.findGenerationVersionByID(callbackID); err == nil {
		return session, versions, idx, nil
	}
	session, err := s.repo.FindSessionByGenerationVersionID(callbackID)
	if err != nil {
		return nil, nil, -1, err
	}
	versions := decodeArray(session.GenerationVersionsJSON)
	for i := range versions {
		if strings.TrimSpace(versions[i].RuntimeJobID) == callbackID {
			return session, versions, i, nil
		}
	}
	return nil, nil, -1, gorm.ErrRecordNotFound
}

func (s *Service) InternalUpdateGenerationRuntime(versionID string, input InternalRuntimeUpdateRequest) (*GenerationVersionDTO, error) {
	unlock, err := s.lockGenerationVersionSession(versionID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	session, versions, idx, err := s.findGenerationVersionByCallbackID(strings.TrimSpace(versionID))
	if err != nil {
		return nil, err
	}
	version := versions[idx]
	if status := strings.TrimSpace(input.Status); status != "" {
		normalizedStatus := normalizeGenerationRuntimeCallbackStatus(status)
		if normalizedStatus == "" {
			return nil, fmt.Errorf("%w: unsupported generation runtime status %q", ErrInternalCallbackInvalid, status)
		}
		if normalizedStatus == "processing" && (version.Status == "completed" || len(version.ResultAssets) > 0) {
			normalizedStatus = version.Status
			if normalizedStatus == "" {
				normalizedStatus = "completed"
			}
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
	unlock, err := s.lockGenerationVersionSession(versionID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	session, versions, idx, err := s.findGenerationVersionByCallbackID(strings.TrimSpace(versionID))
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
	if len(input.Metadata.DeconstructionElements) > 0 {
		return input.Metadata.DeconstructionElements
	}
	for _, variant := range input.Variants {
		for _, key := range []string{"inline_data", "text", "result_text"} {
			if text, _ := variant[key].(string); strings.TrimSpace(text) != "" {
				if elements := parseVisualResultElementsFromJSON(text); len(elements) > 0 {
					return elements
				}
			}
		}
	}
	return nil
}

func parseVisualResultElementsFromJSON(raw string) []InternalResultElementRequest {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	var envelope struct {
		Elements               []InternalResultElementRequest `json:"elements"`
		DeconstructionElements []InternalResultElementRequest `json:"deconstruction_elements"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return nil
	}
	if len(envelope.DeconstructionElements) > 0 {
		return envelope.DeconstructionElements
	}
	if len(envelope.Elements) > 0 {
		return envelope.Elements
	}
	var single InternalResultElementRequest
	if err := json.Unmarshal([]byte(text), &single); err == nil && (strings.TrimSpace(single.ElementType) != "" || strings.TrimSpace(single.ElementKey) != "" || len(single.Value) > 0) {
		return []InternalResultElementRequest{single}
	}
	return nil
}

func fallbackVisualResultElementsFromProviderText(input InternalRecordResultsRequest, sourceRefs []models.EcommerceVisualSourceReference) []InternalResultElementRequest {
	providerText := visualProviderText(input)
	if providerText == "" {
		return nil
	}
	return visualResultElementsForSources(providerText, sourceRefs)
}

func ensureDualTrackVisualResultElements(elements []InternalResultElementRequest, input InternalRecordResultsRequest, sourceRefs []models.EcommerceVisualSourceReference) []InternalResultElementRequest {
	roles := map[string]bool{}
	for i := range elements {
		role := strings.ToLower(strings.TrimSpace(elements[i].SourceRole))
		if role == "" {
			if rawRole, _ := elements[i].Metadata["source_role"].(string); rawRole != "" {
				role = strings.ToLower(strings.TrimSpace(rawRole))
			}
		}
		if role == "sku" || role == "reference" {
			roles[role] = true
		}
	}
	if roles["sku"] && roles["reference"] {
		return elements
	}
	providerText := visualProviderText(input)
	if providerText == "" {
		providerText = "Provider returned a structured visual deconstruction result without complete source-role coverage."
	}
	fallbacks := visualResultElementsForSources(providerText, sourceRefs)
	for _, fallback := range fallbacks {
		if !roles[strings.ToLower(strings.TrimSpace(fallback.SourceRole))] {
			fallback.SortOrder = len(elements)
			elements = append(elements, fallback)
		}
	}
	return elements
}

func visualProviderText(input InternalRecordResultsRequest) string {
	providerText := ""
	if providerText == "" {
		for _, variant := range input.Variants {
			for _, key := range []string{"inline_data", "text", "result_text"} {
				if text, _ := variant[key].(string); strings.TrimSpace(text) != "" {
					providerText = strings.TrimSpace(text)
					break
				}
			}
			if providerText != "" {
				break
			}
		}
	}
	return providerText
}

func visualResultElementsForSources(providerText string, sourceRefs []models.EcommerceVisualSourceReference) []InternalResultElementRequest {
	out := make([]InternalResultElementRequest, 0, len(sourceRefs))
	for i := range sourceRefs {
		role := sourceRoleFromMetadata(decodeObject(sourceRefs[i].Metadata), sourceRefs[i].SourceKind)
		if role != "sku" && role != "reference" {
			continue
		}
		elementType := "product_fact"
		elementKey := "provider_visual_description"
		label := "Provider visual description"
		if role == "reference" {
			elementType = "reference_strategy"
			elementKey = "provider_reference_description"
			label = "Provider reference description"
		}
		out = append(out, InternalResultElementRequest{
			SourceRole:        role,
			SourceReferenceID: sourceRefs[i].ID,
			SourceAssetID:     sourceRefs[i].AssetID,
			ElementType:       elementType,
			ElementKey:        elementKey,
			Label:             label,
			Value:             map[string]any{"provider_text": providerText},
			Confidence:        0.5,
			Readiness:         models.VisualReadinessNeedsReview,
			SortOrder:         len(out),
			Metadata:          map[string]any{"fallback_from_provider_text": true},
		})
	}
	return out
}

func internalResultElementToModel(job *models.EcommerceVisualDeconstructionJob, input InternalResultElementRequest, idx int, sourceIndex map[string]string) (models.EcommerceVisualDeconstructionElement, error) {
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
	metadata := sanitizeDeconstructionRequestMetadata(input.Metadata)
	sourceRole, sourceReferenceID, err := normalizeResultElementSourceProjection(input, sourceIndex)
	if err != nil {
		return models.EcommerceVisualDeconstructionElement{}, err
	}
	if sourceRole != "" {
		metadata["source_role"] = sourceRole
	}
	if sourceReferenceID != "" {
		metadata["source_reference_id"] = sourceReferenceID
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
		Metadata:        mustJSON(metadata),
	}, nil
}

func deconstructionResultSourceIndex(sources []models.EcommerceVisualSourceReference) map[string]string {
	index := map[string]string{}
	for i := range sources {
		role := sourceRoleFromMetadata(decodeObject(sources[i].Metadata), sources[i].SourceKind)
		index[sources[i].ID] = role
	}
	return index
}

func normalizeResultElementSourceProjection(input InternalResultElementRequest, sourceIndex map[string]string) (string, string, error) {
	role := strings.ToLower(strings.TrimSpace(input.SourceRole))
	if role == "" {
		if rawRole, _ := input.Metadata["source_role"].(string); rawRole != "" {
			role = strings.ToLower(strings.TrimSpace(rawRole))
		}
	}
	if role != "" && role != "sku" && role != "reference" {
		return "", "", fmt.Errorf("%w: unsupported source_role %q", ErrInternalCallbackInvalid, input.SourceRole)
	}
	sourceReferenceID := strings.TrimSpace(input.SourceReferenceID)
	if sourceReferenceID == "" {
		if rawID, _ := input.Metadata["source_reference_id"].(string); rawID != "" {
			sourceReferenceID = strings.TrimSpace(rawID)
		}
	}
	if sourceReferenceID == "" {
		return role, "", nil
	}
	trustedRole, ok := sourceIndex[sourceReferenceID]
	if !ok {
		if role != "" && !strings.HasPrefix(sourceReferenceID, "vsr_") {
			return role, "", nil
		}
		return "", "", fmt.Errorf("%w: source_reference_id %q does not belong to this visual workflow session", ErrInternalCallbackInvalid, sourceReferenceID)
	}
	if role == "" {
		role = trustedRole
	}
	if role != trustedRole {
		return "", "", fmt.Errorf("%w: source_role %q does not match source_reference_id %q role %q", ErrInternalCallbackInvalid, role, sourceReferenceID, trustedRole)
	}
	return role, sourceReferenceID, nil
}

func validateDualTrackResultCoverage(job *models.EcommerceVisualDeconstructionJob, elements []models.EcommerceVisualDeconstructionElement) error {
	if !jobRequiresDualTrackResultCoverage(job) || job.Status != models.VisualDeconstructionStatusCompleted {
		return nil
	}
	roles := map[string]bool{}
	for i := range elements {
		role := sourceRoleFromResultElement(elements[i])
		if role == "sku" || role == "reference" {
			roles[role] = true
		}
	}
	if !roles["sku"] || !roles["reference"] {
		return fmt.Errorf("%w: completed dual-track deconstruction result must contain both sku and reference source_role elements", ErrInternalCallbackInvalid)
	}
	return nil
}

func projectSingleImageUnderstandingElements(job *models.EcommerceVisualDeconstructionJob, elements []InternalResultElementRequest, sourceIndex map[string]string) []InternalResultElementRequest {
	metadata := decodeObject(job.Metadata)
	if !strings.EqualFold(fmt.Sprint(metadata["image_understanding_policy"]), "single_image_per_runtime_job") {
		return elements
	}
	primarySourceID := strings.TrimSpace(job.SourceReferenceID)
	if primarySourceID == "" {
		manifest := decodeObject(job.InputManifestJSON)
		primarySourceID = strings.TrimSpace(fmt.Sprint(manifest["source_reference_id"]))
	}
	if primarySourceID == "" {
		return elements
	}
	primaryRole := sourceIndex[primarySourceID]
	if primaryRole != "sku" && primaryRole != "reference" {
		return elements
	}
	out := make([]InternalResultElementRequest, 0, len(elements))
	for _, element := range elements {
		role := strings.ToLower(strings.TrimSpace(element.SourceRole))
		if role == "" {
			if rawRole, _ := element.Metadata["source_role"].(string); rawRole != "" {
				role = strings.ToLower(strings.TrimSpace(rawRole))
			}
		}
		if role != "" && role != "sku" && role != "reference" {
			role = ""
		}
		if role != "" && role != primaryRole {
			continue
		}
		element.SourceRole = primaryRole
		element.SourceReferenceID = primarySourceID
		if element.Metadata == nil {
			element.Metadata = map[string]any{}
		}
		element.Metadata["source_role"] = primaryRole
		element.Metadata["source_reference_id"] = primarySourceID
		out = append(out, element)
	}
	return out
}

func jobRequiresDualTrackResultCoverage(job *models.EcommerceVisualDeconstructionJob) bool {
	metadata := decodeObject(job.Metadata)
	if strings.EqualFold(fmt.Sprint(metadata["image_understanding_policy"]), "single_image_per_runtime_job") {
		return false
	}
	manifest := decodeObject(job.InputManifestJSON)
	if strings.EqualFold(fmt.Sprint(manifest["input_mode"]), "dual_track_sources") {
		return true
	}
	return false
}

func sourceRoleFromResultElement(element models.EcommerceVisualDeconstructionElement) string {
	metadata := decodeObject(element.Metadata)
	role, _ := metadata["source_role"].(string)
	return strings.ToLower(strings.TrimSpace(role))
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
