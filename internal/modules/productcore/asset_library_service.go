package productcore

import (
	"ecommerce-service/internal/repository"
	"encoding/json"
	"fmt"
	"strings"
)

const defaultAssetLibraryVisibility = "library"
const defaultAssetLibraryStatus = "active"

type assetRelationMetadata struct {
	Tags   []string `json:"tags,omitempty"`
	Status string   `json:"status,omitempty"`
}

func (s *Service) ListAssetLibrary(orgID string, input AssetLibraryFilterInput) (*AssetLibraryListResponse, error) {
	filter := normalizeAssetLibraryFilter(input)
	records, total, err := s.repo.ListAssetLibrary(repository.Scope{OrgID: orgID}, repository.AssetLibraryFilter{
		SKUCode:    filter.SKUCode,
		ProductID:  filter.ProductID,
		SourceType: filter.SourceType,
		AssetRole:  filter.AssetRole,
		Visibility: filter.Visibility,
		Status:     filter.Status,
		Tag:        filter.Tag,
		Query:      filter.Query,
		Limit:      filter.Limit,
		Offset:     filter.Offset,
	})
	if err != nil {
		return nil, err
	}
	items := make([]AssetLibraryItem, 0, len(records))
	for _, record := range records {
		items = append(items, buildAssetLibraryItem(record))
	}
	return &AssetLibraryListResponse{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *Service) AssetLibraryStats(orgID string, input AssetLibraryFilterInput, groupBy string) (*AssetLibraryStatsResponse, error) {
	filter := normalizeAssetLibraryFilter(input)
	rows, err := s.repo.AssetLibraryStats(repository.Scope{OrgID: orgID}, repository.AssetLibraryFilter{
		SKUCode:    filter.SKUCode,
		ProductID:  filter.ProductID,
		SourceType: filter.SourceType,
		AssetRole:  filter.AssetRole,
		Visibility: filter.Visibility,
		Status:     filter.Status,
		Tag:        filter.Tag,
		Query:      filter.Query,
	}, strings.TrimSpace(groupBy))
	if err != nil {
		return nil, err
	}
	groups := make([]AssetLibraryStatsGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, AssetLibraryStatsGroup{Key: row.Key, Count: row.Count})
	}
	return &AssetLibraryStatsResponse{Groups: groups}, nil
}

func (s *Service) UpdateAssetLibraryGovernance(orgID string, userID string, relationID string, input UpdateAssetGovernanceInput) (*AssetLibraryItem, error) {
	return s.applyAssetGovernancePatch(repository.Scope{OrgID: orgID, UserID: userID}, strings.TrimSpace(relationID), input)
}

func (s *Service) BatchUpdateAssetLibraryGovernance(orgID string, userID string, input BatchAssetGovernanceInput) (*BatchAssetGovernanceResponse, error) {
	scope := repository.Scope{OrgID: orgID, UserID: userID}
	ids := sanitizeStringList(input.RelationIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("relation_ids are required")
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("too many relation_ids; max 100")
	}
	if input.Patch.IsPrimary != nil && *input.Patch.IsPrimary && len(ids) > 1 {
		return nil, fmt.Errorf("is_primary=true can only be applied to one relation at a time")
	}
	resp := &BatchAssetGovernanceResponse{Results: make([]BatchAssetGovernanceResultItem, 0, len(ids)), Items: make([]AssetLibraryItem, 0, len(ids)), Total: len(ids)}
	for _, id := range ids {
		item, err := s.applyAssetGovernancePatch(scope, id, input.Patch)
		if err != nil {
			resp.Failed++
			resp.Results = append(resp.Results, BatchAssetGovernanceResultItem{RelationID: id, OK: false, Error: err.Error()})
			continue
		}
		resp.Success++
		resp.Results = append(resp.Results, BatchAssetGovernanceResultItem{RelationID: id, OK: true})
		resp.Items = append(resp.Items, *item)
	}
	return resp, nil
}

func (s *Service) GetAssetLibraryLineage(orgID string, relationID string) (*AssetLibraryLineageResponse, error) {
	record, err := s.repo.GetAssetLibraryRecord(repository.Scope{OrgID: orgID}, strings.TrimSpace(relationID))
	if err != nil {
		return nil, err
	}
	lineage := extractAssetLineage(record.AssetMetadata)
	return &AssetLibraryLineageResponse{AssetID: record.AssetID, RelationID: record.RelationID, ProductID: record.ProductID, SKUCode: record.SKUCode, Lineage: lineage}, nil
}

func (s *Service) applyAssetGovernancePatch(scope repository.Scope, relationID string, input UpdateAssetGovernanceInput) (*AssetLibraryItem, error) {
	relation, err := s.repo.GetAssetLibraryRelation(scope, relationID)
	if err != nil {
		return nil, err
	}
	rolePtr := input.AssetRole
	if rolePtr == nil {
		rolePtr = input.Role
	}
	if rolePtr != nil {
		role := strings.TrimSpace(*rolePtr)
		if role == "" || !isValidAssetRole(role) {
			return nil, fmt.Errorf("invalid asset role")
		}
		relation.AssetRole = role
	}
	if input.IsPrimary != nil {
		relation.IsPrimary = *input.IsPrimary
		if *input.IsPrimary {
			if err := s.repo.ClearPrimaryProductAssets(scope, relation.OwnerID, relation.ID); err != nil {
				return nil, err
			}
		}
	}
	if input.SortOrder != nil {
		relation.SortOrder = *input.SortOrder
	}
	if input.Visibility != nil {
		visibility := strings.TrimSpace(*input.Visibility)
		if visibility == "" {
			visibility = defaultAssetLibraryVisibility
		}
		relation.Visibility = visibility
	}
	metadata := decodeRelationMetadata(relation.Metadata)
	if input.Tags != nil {
		metadata.Tags = sanitizeStringList(input.Tags)
	}
	if input.Status != nil {
		metadata.Status = normalizeAssetLibraryStatus(*input.Status)
	}
	relation.Metadata = encodeRelationMetadata(metadata)
	if relation.Visibility == "" {
		relation.Visibility = defaultAssetLibraryVisibility
	}
	if _, err := s.repo.UpdateProductAssetRelation(scope, *relation); err != nil {
		return nil, err
	}
	record, err := s.repo.GetAssetLibraryRecord(scope, relation.ID)
	if err != nil {
		return nil, err
	}
	item := buildAssetLibraryItem(*record)
	return &item, nil
}

func normalizeAssetLibraryFilter(input AssetLibraryFilterInput) AssetLibraryFilterInput {
	input.SKUCode = strings.TrimSpace(input.SKUCode)
	input.ProductID = strings.TrimSpace(input.ProductID)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.AssetRole = strings.TrimSpace(firstNonEmpty(input.AssetRole, input.Role))
	input.Visibility = strings.TrimSpace(input.Visibility)
	rawStatus := strings.TrimSpace(input.Status)
	if rawStatus != "" {
		input.Status = normalizeAssetLibraryStatus(rawStatus)
	} else {
		input.Status = ""
	}
	input.Tag = strings.TrimSpace(input.Tag)
	input.Query = strings.TrimSpace(input.Query)
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}
	if input.Offset < 0 {
		input.Offset = 0
	}
	return input
}

func buildAssetLibraryItem(record repository.AssetLibraryRecord) AssetLibraryItem {
	visibility := record.Visibility
	if visibility == "" {
		visibility = defaultAssetLibraryVisibility
	}
	relationMeta := decodeRelationMetadata(record.RelationMetadata)
	lineage := extractAssetLineage(record.AssetMetadata)
	asset := &AssetLibraryAsset{
		ID:           record.AssetID,
		AssetType:    record.AssetType,
		SourceType:   record.SourceType,
		MimeType:     record.MimeType,
		Width:        record.Width,
		Height:       record.Height,
		FileName:     record.FileName,
		Metadata:     sanitizeAssetLibraryMetadata(record.AssetMetadata),
		ContentURL:   fmt.Sprintf("/api/v1/ecommerce/assets/%s/content", record.AssetID),
		PreviewURL:   fmt.Sprintf("/api/v1/ecommerce/assets/%s/content", record.AssetID),
		ReferenceURI: fmt.Sprintf("ecommerce://assets/%s", record.AssetID),
		CreatedAt:    record.AssetCreatedAt,
		UpdatedAt:    record.AssetUpdatedAt,
	}
	return AssetLibraryItem{
		ProductID:    record.ProductID,
		SKUCode:      record.SKUCode,
		RelationID:   record.RelationID,
		ProductTitle: record.ProductTitle,
		Asset:        asset,
		Lineage:      lineage,
		Governance: AssetLibraryGovernance{
			AssetRole:    record.AssetRole,
			RelationType: record.RelationType,
			IsPrimary:    record.IsPrimary,
			SortOrder:    record.SortOrder,
			Visibility:   visibility,
			Status:       normalizeAssetLibraryStatus(relationMeta.Status),
			Tags:         relationMeta.Tags,
			PlatformCode: record.PlatformCode,
			SiteCode:     record.SiteCode,
			LocaleCode:   record.LocaleCode,
		},
		CreatedAt: record.RelationCreatedAt,
		UpdatedAt: record.RelationUpdatedAt,
	}
}

func extractAssetLineage(raw string) AssetLibraryLineage {
	var metadata map[string]any
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &metadata) != nil {
		return AssetLibraryLineage{}
	}
	jobID := firstStringValue(metadata, "job_id", "jobId", "generation_job_id", "generation_task_id")
	return AssetLibraryLineage{
		PromptID:          firstStringValue(metadata, "prompt_id", "promptId"),
		JobID:             jobID,
		GenerationTaskID:  firstNonEmpty(firstStringValue(metadata, "generation_task_id", "generationTaskId"), jobID),
		RuntimeJobID:      firstStringValue(metadata, "runtime_job_id", "runtimeJobId"),
		ProviderJobID:     firstStringValue(metadata, "provider_job_id", "providerJobId"),
		TemplateID:        firstStringValue(metadata, "template_id", "templateId"),
		TemplateVersionID: firstStringValue(metadata, "template_version_id", "templateVersionId"),
		PromptContentHash: firstStringValue(metadata, "prompt_content_hash", "promptContentHash", "content_hash"),
	}
}

func firstStringValue(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := metadata[key]; ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func decodeRelationMetadata(raw string) assetRelationMetadata {
	var metadata assetRelationMetadata
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &metadata)
	}
	metadata.Tags = sanitizeStringList(metadata.Tags)
	metadata.Status = normalizeAssetLibraryStatus(metadata.Status)
	return metadata
}

func encodeRelationMetadata(metadata assetRelationMetadata) string {
	metadata.Tags = sanitizeStringList(metadata.Tags)
	metadata.Status = normalizeAssetLibraryStatus(metadata.Status)
	if len(metadata.Tags) == 0 && metadata.Status == defaultAssetLibraryStatus {
		return "{}"
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func normalizeAssetLibraryStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return defaultAssetLibraryStatus
	}
	return status
}

func sanitizeAssetLibraryMetadata(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "{}"
	}
	stripStorageKeys(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func stripStorageKeys(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "storage_key" || key == "storageKey" || key == "internal_storage_key" {
				delete(typed, key)
				continue
			}
			stripStorageKeys(child)
		}
	case []any:
		for _, child := range typed {
			stripStorageKeys(child)
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
