package templatecenter

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"ecommerce-service/internal/models"
	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"ecommerce-service/internal/templateutil"
	"ecommerce-service/pkg/logger"
	"ecommerce-service/pkg/metrics"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Service struct {
	repo     *repository.TemplateCenterRepository
	audit    *auditmodule.Service
	platform *platform.Client
}

func NewService(repo *repository.TemplateCenterRepository, audit *auditmodule.Service, platformClient *platform.Client) *Service {
	return &Service{repo: repo, audit: audit, platform: platformClient}
}

func (s *Service) SeedPresetCatalog() error {
	if err := s.repo.SeedIfEmpty(seedCatalogs()); err != nil {
		return err
	}
	summary, err := s.repo.SeedBuiltinSummary()
	if err != nil {
		return err
	}
	log := logger.With("module", "templatecenter")
	if summary.ExampleCount == 0 {
		log.Warn("template center seed summary indicates missing examples", "catalog_count", summary.CatalogCount, "version_count", summary.VersionCount, "example_count", summary.ExampleCount)
		return nil
	}
	log.Info("template center seed summary", "catalog_count", summary.CatalogCount, "version_count", summary.VersionCount, "example_count", summary.ExampleCount)
	return nil
}

func (s *Service) ListCatalog(scope repository.Scope, filter repository.TemplateCatalogFilter) ([]repository.CatalogListItem, error) {
	if s.platformEnabled() {
		items, err := s.listPlatformCatalog(scope, filter)
		if err == nil {
			return items, nil
		}
		logger.With("module", "templatecenter").Warn("platform template catalog unavailable; falling back to local seed", "error", err)
	}
	items, err := s.repo.ListCatalog(scope, filter)
	if err != nil {
		return nil, err
	}
	return s.enrichCatalogListItems(items), nil
}

func (s *Service) Facets(filter repository.TemplateCatalogFilter) (*repository.CatalogFacets, error) {
	return s.repo.ListFacets(filter)
}

func (s *Service) Recommendations(scope repository.Scope, locale string) ([]repository.CatalogListItem, error) {
	if s.platformEnabled() {
		items, err := s.listPlatformCatalog(scope, repository.TemplateCatalogFilter{Locale: locale, FeaturedOnly: true, Limit: 8, SortBy: "recommended"})
		if err == nil {
			if len(items) > 8 {
				items = items[:8]
			}
			return items, nil
		}
		logger.With("module", "templatecenter").Warn("platform template recommendations unavailable; falling back to local seed", "error", err)
	}
	items, err := s.repo.ListCatalog(scope, repository.TemplateCatalogFilter{Locale: locale, FeaturedOnly: true, Limit: 8})
	if err != nil {
		return nil, err
	}
	return s.enrichCatalogListItems(items), nil
}

func (s *Service) Detail(scope repository.Scope, templateID, locale string) (*repository.CatalogDetail, error) {
	return s.catalogDetail(scope, templateID, locale)
}

func (s *Service) catalogDetail(scope repository.Scope, templateID, locale string) (*repository.CatalogDetail, error) {
	if s.platformEnabled() {
		item, err := s.platformCatalogDetail(scope, templateID)
		if err == nil {
			return item, nil
		}
		if err != gorm.ErrRecordNotFound {
			logger.With("module", "templatecenter").Warn("platform template detail unavailable; falling back to local seed", "template_id", templateID, "error", err)
		}
	}
	item, err := s.repo.GetCatalogDetail(scope, templateID, locale)
	if err != nil {
		return nil, err
	}
	s.enrichCatalogDetail(item)
	return item, nil
}

func mapPlatformCatalogListItems(items []platform.PlatformTemplateCatalogItem) []repository.CatalogListItem {
	result := make([]repository.CatalogListItem, 0, len(items))
	for _, item := range items {
		raw := item.Raw
		execution := firstMapAnyValue(raw, "executionSchema", "execution_schema")
		targetRoute := stringMapValue(execution, "route")
		toolBinding := firstMapAnyValue(raw, "toolBinding", "tool_binding")
		result = append(result, repository.CatalogListItem{
			ID:              item.TemplateID,
			Slug:            item.Slug,
			ToolSlug:        templateutil.DeriveToolSlug(targetRoute, stringMapValue(toolBinding, "toolSlug"), stringMapValue(toolBinding, "tool_slug"), templateutil.ToolSlugFromCatalog(item.Slug, stringMapValue(raw, "external_code"))),
			TargetRoute:     targetRoute,
			ExternalCode:    stringMapValue(raw, "external_code"),
			Name:            item.Name,
			Summary:         item.Summary,
			Modality:        item.Modality,
			ExecutorType:    stringMapValue(raw, "executor_type"),
			Series:          item.Series,
			CapabilityType:  item.CapabilityType,
			InteractionMode: stringMapValue(raw, "interaction_mode"),
			CoverAssetURL:   item.CoverAssetURL,
			PlatformTags:    item.Platforms,
			IndustryTags:    stringSliceMapValue(raw, "industry_tags"),
			ScenarioTags:    stringSliceMapValue(raw, "scenario_tags"),
			IsFeatured:      boolMapValue(raw, "featured"),
			RecommendScore:  item.RecommendScore,
			SuccessRateHint: float64MapValue(raw, "success_rate_hint"),
		})
	}
	return result
}

func mapPlatformCatalogDetail(detail *platform.PlatformTemplateCatalogDetail) *repository.CatalogDetail {
	raw := detail.DetailRaw
	catalog := repository.CatalogListItem{
		ID:             detail.Item.TemplateID,
		Slug:           detail.Item.Slug,
		Name:           detail.Item.Name,
		Summary:        detail.Item.Summary,
		Modality:       detail.Item.Modality,
		Series:         detail.Item.Series,
		CapabilityType: detail.Item.CapabilityType,
		CoverAssetURL:  detail.Item.CoverAssetURL,
		PlatformTags:   detail.Item.Platforms,
		RecommendScore: detail.Item.RecommendScore,
	}
	return &repository.CatalogDetail{
		Catalog: catalog,
		Locale: repository.CatalogLocaleDTO{
			Description: firstNonEmpty(stringMapValue(raw, "summary"), detail.Item.Summary),
		},
		Version: repository.CatalogVersionDTO{
			ID:             firstNonEmpty(stringMapValue(raw, "current_version_id"), detail.Item.TemplateID+"_platform"),
			Status:         "published",
			VersionNo:      1,
			VersionLabel:   "platform-projection",
			SourceAssetRef: firstNonEmpty(stringMapValue(raw, "source_asset_ref"), stringMapValue(raw, "sourceAssetRef")),
		},
		Schema: repository.CatalogSchemaDTO{
			InputSchema:      firstMapAnyValue(raw, "inputSchema", "input_schema"),
			OutputSchema:     firstMapAnyValue(raw, "outputSchema", "output_schema"),
			ExecutionSchema:  firstMapAnyValue(raw, "executionSchema", "execution_schema"),
			PromptLayers:     firstMapAnyValue(raw, "promptLayers", "prompt_layers"),
			PolicySchema:     firstMapAnyValue(raw, "policySchema", "policy_schema"),
			DefaultVariables: firstMapAnyValue(raw, "defaultVariables", "default_variables"),
			ToolBinding:      firstMapAnyValue(raw, "toolBinding", "tool_binding"),
		},
		Examples: mapPlatformExamples(raw["examples"]),
	}
}

func stringMapValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func stringSliceMapValue(input map[string]any, key string) []string {
	raw, ok := input[key].([]any)
	if !ok {
		if typed, ok := input[key].([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			out = append(out, value)
		}
	}
	return out
}

func mapAnyValue(input map[string]any, key string) map[string]any {
	value, _ := input[key].(map[string]any)
	return value
}

func firstMapAnyValue(input map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value := mapAnyValue(input, key); value != nil {
			return value
		}
	}
	return map[string]any{}
}

func boolMapValue(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
}

func float64MapValue(input map[string]any, key string) float64 {
	switch typed := input[key].(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func mapPlatformExamples(value any) []repository.CatalogExampleDTO {
	rawExamples, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]repository.CatalogExampleDTO, 0, len(rawExamples))
	for _, raw := range rawExamples {
		example, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, repository.CatalogExampleDTO{
			ID:              stringMapValue(example, "id"),
			ExampleType:     firstNonEmpty(stringMapValue(example, "exampleType"), stringMapValue(example, "type")),
			Title:           stringMapValue(example, "title"),
			Description:     stringMapValue(example, "description"),
			AssetRef:        stringMapValue(example, "assetRef"),
			SourceRef:       stringMapValue(example, "sourceRef"),
			StorageKey:      stringMapValue(example, "storageKey"),
			AssetID:         stringMapValue(example, "assetId"),
			MimeType:        stringMapValue(example, "mimeType"),
			Checksum:        stringMapValue(example, "checksum"),
			InputAssetURL:   stringMapValue(example, "inputAssetUrl"),
			OutputAssetURL:  stringMapValue(example, "outputAssetUrl"),
			PreviewAssetURL: firstNonEmpty(stringMapValue(example, "previewAssetUrl"), stringMapValue(example, "preview")),
			VideoPosterURL:  stringMapValue(example, "videoPosterUrl"),
		})
	}
	return items
}

func (s *Service) Favorites(scope repository.Scope, locale string) ([]repository.CatalogListItem, error) {
	if s.platformEnabled() {
		favoriteIDs, err := s.repo.FavoriteTemplateIDs(scope)
		if err != nil {
			return nil, err
		}
		if len(favoriteIDs) == 0 {
			return []repository.CatalogListItem{}, nil
		}
		items, err := s.listPlatformCatalog(scope, repository.TemplateCatalogFilter{Locale: locale})
		if err == nil {
			favoriteSet := make(map[string]struct{}, len(favoriteIDs))
			for _, id := range favoriteIDs {
				favoriteSet[id] = struct{}{}
			}
			out := make([]repository.CatalogListItem, 0, len(favoriteIDs))
			for _, item := range items {
				if _, ok := favoriteSet[item.ID]; ok {
					item.IsFavorited = true
					out = append(out, item)
				}
			}
			return out, nil
		}
		logger.With("module", "templatecenter").Warn("platform template favorites unavailable; falling back to local seed", "error", err)
	}
	items, err := s.repo.ListFavorites(scope, locale)
	if err != nil {
		return nil, err
	}
	return s.enrichCatalogListItems(items), nil
}

func (s *Service) Instances(scope repository.Scope, locale string) ([]repository.TemplateInstanceListItem, error) {
	return s.repo.ListInstances(scope, locale)
}

func (s *Service) AddFavorite(c *gin.Context, scope repository.Scope, templateID string) error {
	if _, err := s.catalogDetail(scope, templateID, "zh"); err != nil {
		return err
	}
	if err := s.repo.AddFavorite(scope, templateID); err != nil {
		return err
	}
	metrics.IncBusinessCounter("ecommerce_template_center_favorite_total")
	_ = s.recordUsage(c, scope, templateID, "favorite", "success", nil)
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.favorite", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template favorited"})
	}
	return nil
}

func (s *Service) RemoveFavorite(c *gin.Context, scope repository.Scope, templateID string) error {
	if err := s.repo.RemoveFavorite(scope, templateID); err != nil {
		return err
	}
	_ = s.recordUsage(c, scope, templateID, "unfavorite", "success", nil)
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.unfavorite", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template unfavorited"})
	}
	return nil
}

func (s *Service) CopyToMyTemplates(c *gin.Context, scope repository.Scope, templateID string) (*models.TemplateInstance, error) {
	if s.platformEnabled() {
		detail, err := s.platformCatalogDetail(scope, templateID)
		if err == nil {
			instance, err := s.copyPlatformTemplate(scope, detail)
			if err != nil {
				return nil, err
			}
			metrics.IncBusinessCounter("ecommerce_template_center_copy_total")
			_ = s.recordUsage(c, scope, templateID, "copy", "success", map[string]any{"templateInstanceId": instance.ID, "managed_source": "platform_projection"})
			if s.audit != nil {
				_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.copy_to_my_templates", TargetType: "template_instance", TargetID: instance.ID, Status: "success", Details: "template copied from platform projection", AfterSnapshot: instance})
			}
			return instance, nil
		}
		if err != gorm.ErrRecordNotFound {
			logger.With("module", "templatecenter").Warn("platform template copy unavailable; falling back to local seed", "template_id", templateID, "error", err)
		}
	}
	instance, err := s.repo.CopyToMyTemplates(scope, templateID)
	if err != nil {
		return nil, err
	}
	metrics.IncBusinessCounter("ecommerce_template_center_copy_total")
	_ = s.recordUsage(c, scope, templateID, "copy", "success", map[string]any{"templateInstanceId": instance.ID})
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.copy_to_my_templates", TargetType: "template_instance", TargetID: instance.ID, Status: "success", Details: "template copied to my templates", AfterSnapshot: instance})
	}
	return instance, nil
}

func (s *Service) Use(c *gin.Context, scope repository.Scope, templateID string) (*repository.UseTemplateResponse, error) {
	if s.platformEnabled() {
		detail, err := s.platformCatalogDetail(scope, templateID)
		if err == nil {
			result := buildPlatformUseResponse(detail)
			metrics.IncBusinessCounter("ecommerce_template_center_use_total")
			_ = s.recordUsage(c, scope, templateID, "use", "success", map[string]any{"managed_source": "platform_projection", "targetRoute": result.TargetRoute})
			if s.audit != nil {
				_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.use", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "platform template use route resolved", AfterSnapshot: result})
			}
			return result, nil
		}
		if err != gorm.ErrRecordNotFound {
			logger.With("module", "templatecenter").Warn("platform template use unavailable; falling back to local seed", "template_id", templateID, "error", err)
		}
	}
	result, err := s.repo.BuildUseResponse(scope, templateID)
	if err != nil {
		return nil, err
	}
	metrics.IncBusinessCounter("ecommerce_template_center_use_total")
	_ = s.recordUsage(c, scope, templateID, "use", "success", result)
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.use", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template use route resolved", AfterSnapshot: result})
	}
	return result, nil
}

func (s *Service) recordUsage(c *gin.Context, scope repository.Scope, templateID, eventType, status string, payload any) error {
	payloadJSON, _ := json.Marshal(payload)
	return s.repo.CreateUsageEvent(&models.TemplateUsageEvent{ID: buildID("tpl_evt"), EventType: eventType, TemplateCatalogID: templateID, UserID: scope.UserID, OrganizationID: scope.OrgID, RequestID: c.GetString("requestID"), TraceID: c.GetString("traceID"), RoutePath: c.FullPath(), Status: status, PayloadJSON: string(payloadJSON), CreatedAt: time.Now()})
}

func buildID(prefix string) string {
	return prefix + "_" + time.Now().Format("20060102150405.000000000")
}

func (s *Service) enrichCatalogListItems(items []repository.CatalogListItem) []repository.CatalogListItem {
	if s.platform == nil || len(items) == 0 {
		for idx := range items {
			if items[idx].ToolSlug == "" {
				items[idx].ToolSlug = templateutil.ToolSlugFromCatalog(items[idx].Slug, items[idx].ExternalCode)
			}
		}
		return items
	}
	sourceRefs := make([]string, 0, len(items))
	for _, item := range items {
		if item.ToolSlug == "" {
			item.ToolSlug = templateutil.ToolSlugFromCatalog(item.Slug, item.ExternalCode)
		}
		if item.CoverAssetURL != "" {
			continue
		}
		toolSlug := item.ToolSlug
		if item.ExternalCode == "" || toolSlug == "" {
			continue
		}
		sourceRefs = append(sourceRefs, fmt.Sprintf("templates/%s/%s/example-1", toolSlug, item.ExternalCode))
	}
	resolved := s.resolveExampleAssets(sourceRefs)
	for idx := range items {
		if items[idx].ToolSlug == "" {
			items[idx].ToolSlug = templateutil.ToolSlugFromCatalog(items[idx].Slug, items[idx].ExternalCode)
		}
		toolSlug := items[idx].ToolSlug
		if items[idx].CoverAssetURL != "" || items[idx].ExternalCode == "" || toolSlug == "" {
			continue
		}
		sourceRef := fmt.Sprintf("templates/%s/%s/example-1", toolSlug, items[idx].ExternalCode)
		if asset, ok := resolved[sourceRef]; ok {
			items[idx].CoverAssetURL = buildExamplePreviewURL(asset.StorageKey)
		}
	}
	return items
}

func (s *Service) enrichCatalogDetail(item *repository.CatalogDetail) {
	if s.platform == nil || item == nil || len(item.Examples) == 0 {
		return
	}
	sourceRefs := make([]string, 0, len(item.Examples))
	for _, example := range item.Examples {
		if example.SourceRef != "" {
			sourceRefs = append(sourceRefs, example.SourceRef)
		}
	}
	resolved := s.resolveExampleAssets(sourceRefs)
	for idx := range item.Examples {
		sourceRef := item.Examples[idx].SourceRef
		if sourceRef == "" {
			continue
		}
		asset, ok := resolved[sourceRef]
		if !ok {
			continue
		}
		item.Examples[idx].StorageKey = asset.StorageKey
		item.Examples[idx].AssetID = asset.ID
		item.Examples[idx].MimeType = asset.MimeType
		item.Examples[idx].Checksum = asset.Checksum
		item.Examples[idx].PreviewAssetURL = buildExamplePreviewURL(asset.StorageKey)
	}
	if item.Catalog.CoverAssetURL == "" && len(item.Examples) > 0 && item.Examples[0].PreviewAssetURL != "" {
		item.Catalog.CoverAssetURL = item.Examples[0].PreviewAssetURL
	}
}

func (s *Service) resolveExampleAssets(sourceRefs []string) map[string]platform.AssetRecord {
	if s.platform == nil || len(sourceRefs) == 0 {
		return map[string]platform.AssetRecord{}
	}
	inputs := make([]platform.ResolveAssetInput, 0, len(sourceRefs))
	seen := map[string]struct{}{}
	for _, sourceRef := range sourceRefs {
		if sourceRef == "" {
			continue
		}
		if _, ok := seen[sourceRef]; ok {
			continue
		}
		seen[sourceRef] = struct{}{}
		inputs = append(inputs, platform.ResolveAssetInput{
			ProductCode: "ecommerce",
			Category:    "template-examples",
			SourceType:  "template_example",
			SourceRef:   sourceRef,
		})
	}
	items, err := s.platform.ResolveAssets(inputs)
	if err != nil {
		return map[string]platform.AssetRecord{}
	}
	out := make(map[string]platform.AssetRecord, len(items))
	for _, item := range items {
		out[item.SourceRef] = item
	}
	return out
}

func buildExamplePreviewURL(storageKey string) string {
	if storageKey == "" {
		return ""
	}
	return "/api/v1/ecommerce/template-center/assets/preview?storage_key=" + storageKey
}

func (s *Service) platformEnabled() bool {
	return s.platform != nil && strings.TrimSpace(s.platform.BaseURL()) != ""
}

func (s *Service) listPlatformCatalog(scope repository.Scope, filter repository.TemplateCatalogFilter) ([]repository.CatalogListItem, error) {
	allItems := make([]platform.PlatformTemplateCatalogItem, 0, 64)
	offset := 0
	const pageSize = 200
	for {
		result, err := s.platform.InternalTemplateCatalog(platform.InternalTemplateCatalogInput{
			ProductCode:   "ecommerce",
			ToolSlug:      filter.ToolSlug,
			Limit:         pageSize,
			Offset:        offset,
			PublishedOnly: true,
		})
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, result.Items...)
		if len(result.Items) < pageSize || len(allItems) >= result.Total {
			break
		}
		offset += pageSize
	}
	items := mapPlatformCatalogListItems(allItems)
	if len(items) == 0 {
		localItems, err := s.repo.ListCatalog(scope, filter)
		if err != nil {
			return nil, err
		}
		return s.enrichCatalogListItems(localItems), nil
	}
	favoriteIDs, favoriteErr := s.repo.FavoriteTemplateIDs(scope)
	if favoriteErr != nil {
		return nil, favoriteErr
	}
	favoriteSet := make(map[string]struct{}, len(favoriteIDs))
	for _, id := range favoriteIDs {
		favoriteSet[id] = struct{}{}
	}
	filtered := make([]repository.CatalogListItem, 0, len(items))
	for _, item := range items {
		item.IsFavorited = false
		if _, ok := favoriteSet[item.ID]; ok {
			item.IsFavorited = true
		}
		if matchesPlatformCatalogFilter(item, filter) {
			filtered = append(filtered, item)
		}
	}
	sortPlatformCatalogItems(filtered, filter.SortBy)
	if filter.Offset > 0 && filter.Offset < len(filtered) {
		filtered = filtered[filter.Offset:]
	} else if filter.Offset >= len(filtered) {
		return []repository.CatalogListItem{}, nil
	}
	if filter.Limit > 0 && filter.Limit < len(filtered) {
		filtered = filtered[:filter.Limit]
	}
	return s.enrichCatalogListItems(filtered), nil
}

func (s *Service) platformCatalogDetail(scope repository.Scope, templateID string) (*repository.CatalogDetail, error) {
	result, err := s.platform.InternalTemplateCatalogDetail("ecommerce:" + templateID)
	if err != nil {
		if platform.IsNotFound(err) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	detail := mapPlatformCatalogDetail(result)
	favoriteIDs, favoriteErr := s.repo.FavoriteTemplateIDs(scope)
	if favoriteErr != nil {
		return nil, favoriteErr
	}
	for _, id := range favoriteIDs {
		if id == templateID {
			detail.Catalog.IsFavorited = true
			break
		}
	}
	s.enrichCatalogDetail(detail)
	return detail, nil
}

func (s *Service) copyPlatformTemplate(scope repository.Scope, detail *repository.CatalogDetail) (*models.TemplateInstance, error) {
	platformJSON, _ := json.Marshal(detail.Catalog.PlatformTags)
	industryJSON, _ := json.Marshal(detail.Catalog.IndustryTags)
	editableJSON, _ := json.Marshal(map[string]any{
		"inputSchema":      detail.Schema.InputSchema,
		"outputSchema":     detail.Schema.OutputSchema,
		"executionSchema":  detail.Schema.ExecutionSchema,
		"defaultVariables": detail.Schema.DefaultVariables,
	})
	promptJSON, _ := json.Marshal(detail.Schema.PromptLayers)
	instance := &models.TemplateInstance{
		ID:                 buildID("inst"),
		UserID:             scope.UserID,
		OrganizationID:     scope.OrgID,
		PresetTemplateID:   detail.Catalog.ID,
		PresetVersionID:    detail.Version.ID,
		SourceType:         "preset_catalog",
		SourceLabel:        detail.Catalog.Name,
		Modality:           detail.Catalog.Modality,
		ExecutorType:       detail.Catalog.ExecutorType,
		Series:             detail.Catalog.Series,
		CapabilityType:     detail.Catalog.CapabilityType,
		Status:             "published",
		IsArchived:         false,
		IsFavorite:         detail.Catalog.IsFavorited,
		EditableSchemaJSON: string(editableJSON),
		PromptLayersJSON:   string(promptJSON),
		PlatformTagsJSON:   string(platformJSON),
		IndustryTagsJSON:   string(industryJSON),
		SavedAt:            time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := s.repo.CreateTemplateInstance(instance); err != nil {
		return nil, err
	}
	return instance, nil
}

func buildPlatformUseResponse(detail *repository.CatalogDetail) *repository.UseTemplateResponse {
	execution := detail.Schema.ExecutionSchema
	toolBinding := detail.Schema.ToolBinding
	targetRoute := stringMapValue(execution, "route")
	return &repository.UseTemplateResponse{
		TargetRoute:          targetRoute,
		ExecutorType:         detail.Catalog.ExecutorType,
		ToolSlug:             templateutil.DeriveToolSlug(targetRoute, stringMapValue(toolBinding, "toolSlug"), stringMapValue(toolBinding, "tool_slug"), detail.Catalog.Slug, detail.Catalog.ExternalCode),
		PrefilledInputSchema: detail.Schema.InputSchema,
		PreloadedTemplatePayload: map[string]any{
			"templateId":       detail.Catalog.ID,
			"externalCode":     detail.Catalog.ExternalCode,
			"templateSlug":     detail.Catalog.Slug,
			"templateName":     detail.Catalog.Name,
			"modality":         detail.Catalog.Modality,
			"executorType":     detail.Catalog.ExecutorType,
			"promptLayers":     detail.Schema.PromptLayers,
			"defaultVariables": detail.Schema.DefaultVariables,
		},
		SupportsAsyncJob: boolMapValue(execution, "supportsAsyncJob"),
		SupportsBatch:    boolMapValue(execution, "supportsBatch"),
	}
}

func matchesPlatformCatalogFilter(item repository.CatalogListItem, filter repository.TemplateCatalogFilter) bool {
	if filter.ToolSlug != "" && !strings.EqualFold(item.ToolSlug, filter.ToolSlug) {
		return false
	}
	if filter.Modality != "" && !strings.EqualFold(item.Modality, filter.Modality) {
		return false
	}
	if filter.Series != "" && !strings.EqualFold(item.Series, filter.Series) {
		return false
	}
	if filter.Capability != "" && !strings.EqualFold(item.CapabilityType, filter.Capability) {
		return false
	}
	if filter.Platform != "" && !containsFold(item.PlatformTags, filter.Platform) {
		return false
	}
	if filter.FeaturedOnly && !item.IsFeatured {
		return false
	}
	keyword := strings.TrimSpace(strings.ToLower(filter.Keyword))
	if keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Name), keyword) ||
		strings.Contains(strings.ToLower(item.Summary), keyword) ||
		strings.Contains(strings.ToLower(item.ExternalCode), keyword) ||
		strings.Contains(strings.ToLower(item.ID), keyword)
}

func sortPlatformCatalogItems(items []repository.CatalogListItem, sortBy string) {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "latest":
		sort.SliceStable(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	case "name":
		sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	default:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].RecommendScore == items[j].RecommendScore {
				return items[i].Name < items[j].Name
			}
			return items[i].RecommendScore > items[j].RecommendScore
		})
	}
}

func containsFold(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}

func (s *Service) DownloadExampleAsset(storageKey string) (io.ReadCloser, map[string]string, error) {
	if s.platform == nil {
		return nil, nil, fmt.Errorf("platform client is not configured")
	}
	body, header, err := s.platform.DownloadAsset(storageKey)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	if contentType := header.Get("Content-Type"); contentType != "" {
		out["Content-Type"] = contentType
	}
	if cacheControl := header.Get("Cache-Control"); cacheControl != "" {
		out["Cache-Control"] = cacheControl
	}
	if contentLength := header.Get("Content-Length"); contentLength != "" {
		out["Content-Length"] = contentLength
	}
	return body, out, nil
}
