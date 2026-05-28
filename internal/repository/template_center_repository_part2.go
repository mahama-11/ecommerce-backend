package repository

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/templateutil"

	"gorm.io/gorm"
)

func (r *TemplateCenterRepository) BuildUseResponse(scope Scope, templateID string) (*UseTemplateResponse, error) {
	detail, err := r.GetCatalogDetail(scope, templateID, "zh")
	if err != nil {
		return nil, err
	}
	execution := detail.Schema.ExecutionSchema
	toolBinding := detail.Schema.ToolBinding
	targetRoute := stringValue(execution["route"])
	resp := &UseTemplateResponse{
		TargetRoute:          targetRoute,
		ExecutorType:         detail.Catalog.ExecutorType,
		ToolSlug:             templateutil.DeriveToolSlug(targetRoute, stringValue(toolBinding["toolSlug"]), stringValue(toolBinding["tool_slug"]), detail.Catalog.Slug, detail.Catalog.ExternalCode),
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
		SupportsAsyncJob: boolValue(execution["supportsAsyncJob"]),
		SupportsBatch:    boolValue(execution["supportsBatch"]),
	}
	return resp, nil
}

func (r *TemplateCenterRepository) CreateUsageEvent(item *models.TemplateUsageEvent) error {
	return r.db.Create(item).Error
}

func (r *TemplateCenterRepository) ListInstances(scope Scope, locale string) ([]TemplateInstanceListItem, error) {
	var items []models.TemplateInstance
	if err := r.db.Where("user_id = ? AND organization_id = ? AND is_archived = ?", scope.UserID, scope.OrgID, false).Order("saved_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []TemplateInstanceListItem{}, nil
	}

	presetIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.PresetTemplateID != "" {
			presetIDs = append(presetIDs, item.PresetTemplateID)
		}
	}

	localeMap := map[string]models.TemplateCatalogLocale{}
	if len(presetIDs) > 0 {
		var locales []models.TemplateCatalogLocale
		if err := r.db.Where("template_catalog_id IN ? AND locale IN ?", presetIDs, []string{normalizedLocale(locale), "zh"}).Find(&locales).Error; err != nil {
			return nil, err
		}
		for _, item := range locales {
			if _, exists := localeMap[item.TemplateCatalogID]; exists && item.Locale != normalizedLocale(locale) {
				continue
			}
			if item.Locale == normalizedLocale(locale) || localeMap[item.TemplateCatalogID].ID == "" {
				localeMap[item.TemplateCatalogID] = item
			}
		}
	}

	out := make([]TemplateInstanceListItem, 0, len(items))
	for _, item := range items {
		loc := localeMap[item.PresetTemplateID]
		title := item.SourceLabel
		summary := ""
		scenario := ""
		if loc.Name != "" {
			title = loc.Name
			summary = loc.Summary
			scenario = loc.ScenarioDescription
		}
		if summary == "" {
			summary = item.SourceLabel
		}

		out = append(out, TemplateInstanceListItem{
			ID:               item.ID,
			PresetTemplateID: item.PresetTemplateID,
			Title:            title,
			Summary:          summary,
			Scenario:         scenario,
			Modality:         item.Modality,
			ExecutorType:     item.ExecutorType,
			Series:           item.Series,
			CapabilityType:   item.CapabilityType,
			PlatformTags:     decodeStringArray(item.PlatformTagsJSON),
			IndustryTags:     decodeStringArray(item.IndustryTagsJSON),
			SourceType:       item.SourceType,
			SourceLabel:      item.SourceLabel,
			Status:           item.Status,
			IsFavorite:       item.IsFavorite,
			SavedAt:          item.SavedAt.Format(time.RFC3339),
			UpdatedAt:        item.UpdatedAt.Format(time.RFC3339),
			EditableSchema:   decodeJSONMap(item.EditableSchemaJSON),
			PromptLayers:     decodeJSONMap(item.PromptLayersJSON),
		})
	}
	return out, nil
}

func (r *TemplateCenterRepository) applyCatalogFilter(q *gorm.DB, filter TemplateCatalogFilter) *gorm.DB {
	q = q.Where("status = ?", "published")
	if filter.Modality != "" {
		q = q.Where("modality = ?", filter.Modality)
	}
	if filter.Series != "" {
		q = q.Where("series = ?", filter.Series)
	}
	if filter.Capability != "" {
		q = q.Where("capability_type = ?", filter.Capability)
	}
	if filter.FeaturedOnly {
		q = q.Where("is_featured = ?", true)
	}
	if filter.Platform != "" {
		q = q.Where("platform_tags_json LIKE ?", fmt.Sprintf("%%%q%%", filter.Platform))
	}
	return q
}

func (r *TemplateCenterRepository) loadLocales(locale string, catalogs []models.TemplateCatalog) ([]models.TemplateCatalogLocale, map[string]models.TemplateCatalogLocale, error) {
	if len(catalogs) == 0 {
		return nil, map[string]models.TemplateCatalogLocale{}, nil
	}
	ids := make([]string, 0, len(catalogs))
	for _, item := range catalogs {
		ids = append(ids, item.ID)
	}
	var locales []models.TemplateCatalogLocale
	if err := r.db.Where("template_catalog_id IN ? AND locale IN ?", ids, []string{locale, "zh"}).Find(&locales).Error; err != nil {
		return nil, nil, err
	}
	localeMap := map[string]models.TemplateCatalogLocale{}
	for _, item := range locales {
		if _, exists := localeMap[item.TemplateCatalogID]; exists && item.Locale != locale {
			continue
		}
		if item.Locale == locale || localeMap[item.TemplateCatalogID].ID == "" {
			localeMap[item.TemplateCatalogID] = item
		}
	}
	return locales, localeMap, nil
}

func (r *TemplateCenterRepository) getLocale(templateID, locale string) (models.TemplateCatalogLocale, error) {
	var item models.TemplateCatalogLocale
	if err := r.db.Where("template_catalog_id = ? AND locale = ?", templateID, locale).First(&item).Error; err == nil {
		return item, nil
	}
	if err := r.db.Where("template_catalog_id = ? AND locale = ?", templateID, "zh").First(&item).Error; err != nil {
		return item, err
	}
	return item, nil
}

func (r *TemplateCenterRepository) favoriteSet(scope Scope, catalogs []models.TemplateCatalog) (map[string]bool, error) {
	set := map[string]bool{}
	if scope.UserID == "" || scope.OrgID == "" || len(catalogs) == 0 {
		return set, nil
	}
	ids := make([]string, 0, len(catalogs))
	for _, item := range catalogs {
		ids = append(ids, item.ID)
	}
	var favorites []models.TemplateFavorite
	if err := r.db.Where("user_id = ? AND organization_id = ? AND template_catalog_id IN ?", scope.UserID, scope.OrgID, ids).Find(&favorites).Error; err != nil {
		return nil, err
	}
	for _, item := range favorites {
		set[item.TemplateCatalogID] = true
	}
	return set, nil
}

func (r *TemplateCenterRepository) favoriteCounts(catalogs []models.TemplateCatalog) (map[string]int64, error) {
	counts := map[string]int64{}
	if len(catalogs) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(catalogs))
	for _, item := range catalogs {
		ids = append(ids, item.ID)
	}
	type row struct {
		TemplateCatalogID string
		Count             int64
	}
	var rows []row
	if err := r.db.Model(&models.TemplateFavorite{}).Select("template_catalog_id, count(*) as count").Where("template_catalog_id IN ?", ids).Group("template_catalog_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, item := range rows {
		counts[item.TemplateCatalogID] = item.Count
	}
	return counts, nil
}

func (r *TemplateCenterRepository) useCounts(catalogs []models.TemplateCatalog) (map[string]int64, error) {
	counts := map[string]int64{}
	if len(catalogs) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(catalogs))
	for _, item := range catalogs {
		ids = append(ids, item.ID)
	}
	type row struct {
		TemplateCatalogID string
		Count             int64
	}
	var rows []row
	if err := r.db.Model(&models.TemplateUsageEvent{}).Select("template_catalog_id, count(*) as count").Where("template_catalog_id IN ? AND event_type IN ?", ids, []string{"use", "execute_success"}).Group("template_catalog_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, item := range rows {
		counts[item.TemplateCatalogID] = item.Count
	}
	return counts, nil
}

func toCatalogListItem(item models.TemplateCatalog, locale models.TemplateCatalogLocale, isFavorited bool, favoriteCount, useCount int64, binding schemaBinding) CatalogListItem {
	return CatalogListItem{
		ID:                   item.ID,
		Slug:                 item.Slug,
		ToolSlug:             binding.ToolSlug,
		TargetRoute:          binding.TargetRoute,
		ExternalCode:         item.ExternalCode,
		Name:                 locale.Name,
		Summary:              locale.Summary,
		Modality:             item.Modality,
		ExecutorType:         item.ExecutorType,
		Series:               item.Series,
		CapabilityType:       item.CapabilityType,
		InteractionMode:      item.InteractionMode,
		CoverAssetURL:        item.CoverAssetURL,
		PlatformTags:         decodeStringArray(item.PlatformTagsJSON),
		IndustryTags:         decodeStringArray(item.IndustryTagsJSON),
		ScenarioTags:         decodeStringArray(item.ScenarioTagsJSON),
		InputModes:           binding.InputModes,
		ProductCategories:    binding.ProductCategories,
		ProviderCapabilities: binding.ProviderCapabilities,
		Applicability:        binding.Applicability,
		IsFeatured:           item.IsFeatured,
		RecommendScore:       item.RecommendScore,
		IsFavorited:          isFavorited,
		FavoriteCount:        favoriteCount,
		UseCount:             useCount,
		SuccessRateHint:      item.SuccessRateHint,
	}
}

type schemaBinding struct {
	ToolSlug             string
	TargetRoute          string
	InputModes           []string
	ProductCategories    []string
	ProviderCapabilities []string
	Applicability        map[string]any
}

func (r *TemplateCenterRepository) loadSchemaBindings(catalogs []models.TemplateCatalog) (map[string]schemaBinding, error) {
	out := map[string]schemaBinding{}
	versionToCatalog := map[string]models.TemplateCatalog{}
	versionIDs := make([]string, 0, len(catalogs))
	for _, item := range catalogs {
		if strings.TrimSpace(item.CurrentVersionID) == "" {
			continue
		}
		versionIDs = append(versionIDs, item.CurrentVersionID)
		versionToCatalog[item.CurrentVersionID] = item
	}
	if len(versionIDs) == 0 {
		return out, nil
	}
	var schemas []models.TemplateCatalogSchema
	if err := r.db.Where("template_version_id IN ?", versionIDs).Find(&schemas).Error; err != nil {
		return nil, err
	}
	for _, schema := range schemas {
		catalog, ok := versionToCatalog[schema.TemplateVersionID]
		if !ok {
			continue
		}
		out[catalog.ID] = schemaBindingFromSchema(schema, catalog)
	}
	return out, nil
}

func schemaBindingFromSchema(schema models.TemplateCatalogSchema, catalog models.TemplateCatalog) schemaBinding {
	execution := decodeJSONMap(schema.ExecutionSchemaJSON)
	toolBinding := decodeJSONMap(schema.ToolBindingJSON)
	inputSchema := decodeJSONMap(schema.InputSchemaJSON)
	targetRoute := stringValue(execution["route"])
	applicability := applicabilityFromSchema(schema, catalog)
	return schemaBinding{
		ToolSlug:             templateutil.DeriveToolSlug(targetRoute, stringValue(toolBinding["toolSlug"]), stringValue(toolBinding["tool_slug"]), catalog.Slug, catalog.ExternalCode),
		TargetRoute:          targetRoute,
		InputModes:           firstNonEmptyStringSlice(stringSliceFromAny(firstNonNil(applicability["input_modes"], applicability["inputModes"], toolBinding["inputModes"], toolBinding["input_modes"])), stringSliceFromAny(inputSchema["input_modes"]), stringSliceFromAny(inputSchema["inputModes"]), singleStringSlice(inputSchema["input_mode"]), singleStringSlice(inputSchema["inputMode"])),
		ProductCategories:    stringSliceFromAny(firstNonNil(applicability["product_categories"], applicability["productCategories"])),
		ProviderCapabilities: stringSliceFromAny(firstNonNil(applicability["provider_capabilities"], applicability["providerCapabilities"], toolBinding["providerCapabilities"], toolBinding["provider_capabilities"])),
		Applicability:        applicability,
	}
}

func toExamples(items []models.TemplateCatalogExample) []CatalogExampleDTO {
	result := make([]CatalogExampleDTO, 0, len(items))
	sort.Slice(items, func(i, j int) bool { return items[i].SortOrder < items[j].SortOrder })
	for _, item := range items {
		result = append(result, CatalogExampleDTO{
			ID:              item.ID,
			ExampleType:     item.ExampleType,
			Title:           item.Title,
			Description:     item.Description,
			AssetRef:        item.AssetRef,
			SourceRef:       item.SourceRef,
			StorageKey:      item.StorageKey,
			AssetID:         item.AssetID,
			MimeType:        item.MimeType,
			Checksum:        item.Checksum,
			InputAssetURL:   item.InputAssetURL,
			OutputAssetURL:  item.OutputAssetURL,
			PreviewAssetURL: item.PreviewAssetURL,
			VideoPosterURL:  item.VideoPosterURL,
		})
	}
	return result
}

func decodeStringArray(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

func decodeJSONMap(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func matchesCatalogContextFilter(item CatalogListItem, filter TemplateCatalogFilter) bool {
	if filter.ToolSlug != "" && !strings.EqualFold(item.ToolSlug, filter.ToolSlug) {
		return false
	}
	if filter.InputMode != "" && len(item.InputModes) > 0 && !containsStringFold(item.InputModes, filter.InputMode) {
		return false
	}
	if filter.ProductCategory != "" {
		if containsStringFold(stringSliceFromAny(firstNonNil(item.Applicability["product_category_exclude"], item.Applicability["productCategoryExclude"], item.Applicability["product_categories_exclude"], item.Applicability["productCategoriesExclude"])), filter.ProductCategory) {
			return false
		}
		if len(item.ProductCategories) > 0 && !containsStringFold(item.ProductCategories, filter.ProductCategory) {
			return false
		}
	}
	if filter.ProviderCapability != "" && len(item.ProviderCapabilities) > 0 && !containsStringFold(item.ProviderCapabilities, filter.ProviderCapability) {
		return false
	}
	if filter.Industry != "" && !containsStringFold(item.IndustryTags, filter.Industry) {
		return false
	}
	if filter.Scenario != "" && !containsStringFold(item.ScenarioTags, filter.Scenario) {
		return false
	}
	return true
}

func applicabilityFromSchema(schema models.TemplateCatalogSchema, catalog models.TemplateCatalog) map[string]any {
	policy := decodeJSONMap(schema.PolicySchemaJSON)
	toolBinding := decodeJSONMap(schema.ToolBindingJSON)
	inputSchema := decodeJSONMap(schema.InputSchemaJSON)
	app := mapValueFromAny(firstNonNil(policy["applicability"], toolBinding["applicability"], inputSchema["applicability"]))
	if app == nil {
		app = map[string]any{}
	}
	if _, ok := app["platforms"]; !ok {
		app["platforms"] = decodeStringArray(catalog.PlatformTagsJSON)
	}
	if _, ok := app["industries"]; !ok {
		app["industries"] = decodeStringArray(catalog.IndustryTagsJSON)
	}
	if _, ok := app["scenarios"]; !ok {
		app["scenarios"] = decodeStringArray(catalog.ScenarioTagsJSON)
	}
	return app
}

func requiredAssetsFromSchema(schema models.TemplateCatalogSchema) []TemplateRequiredAssetDTO {
	inputSchema := decodeJSONMap(schema.InputSchemaJSON)
	raw := firstNonNil(inputSchema["required_assets"], inputSchema["requiredAssets"])
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]TemplateRequiredAssetDTO, 0, len(items))
	for _, item := range items {
		m := mapValueFromAny(item)
		if m == nil {
			continue
		}
		slot := stringValue(m["slot"])
		if slot == "" {
			continue
		}
		out = append(out, TemplateRequiredAssetDTO{Slot: slot, Role: stringValue(m["role"]), Label: stringValue(m["label"]), Required: boolValue(m["required"]), Constraints: mapValueFromAny(m["constraints"])})
	}
	return out
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func mapValueFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func singleStringSlice(value any) []string {
	if s := stringValue(value); s != "" {
		return []string{s}
	}
	return nil
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func containsStringFold(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}

func applyCatalogSort(q *gorm.DB, sortBy string) *gorm.DB {
	switch sortBy {
	case "newest":
		return q.Order("created_at desc").Order("recommend_score desc").Order("sort_order asc")
	case "most_used":
		return q.Order("recommend_score desc").Order("sort_order asc")
	case "alphabetical":
		return q.Order("slug asc")
	case "most_favorited":
		return q.Order("recommend_score desc").Order("sort_order asc")
	default:
		return q.Order("is_featured desc").Order("recommend_score desc").Order("sort_order asc")
	}
}

func sortCatalogItems(items []CatalogListItem, sortBy string) {
	switch sortBy {
	case "newest":
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].RecommendScore == items[j].RecommendScore {
				return items[i].Slug < items[j].Slug
			}
			return items[i].RecommendScore > items[j].RecommendScore
		})
	case "most_used":
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].UseCount == items[j].UseCount {
				if items[i].RecommendScore == items[j].RecommendScore {
					return items[i].Slug < items[j].Slug
				}
				return items[i].RecommendScore > items[j].RecommendScore
			}
			return items[i].UseCount > items[j].UseCount
		})
	case "most_favorited":
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].FavoriteCount == items[j].FavoriteCount {
				if items[i].RecommendScore == items[j].RecommendScore {
					return items[i].Slug < items[j].Slug
				}
				return items[i].RecommendScore > items[j].RecommendScore
			}
			return items[i].FavoriteCount > items[j].FavoriteCount
		})
	case "alphabetical":
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
	default:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].IsFeatured == items[j].IsFeatured {
				if items[i].RecommendScore == items[j].RecommendScore {
					return items[i].Slug < items[j].Slug
				}
				return items[i].RecommendScore > items[j].RecommendScore
			}
			return items[i].IsFeatured
		})
	}
}

func sortedFacetBuckets(counts map[string]int64) []CatalogFacetBucket {
	out := make([]CatalogFacetBucket, 0, len(counts))
	for key, count := range counts {
		out = append(out, CatalogFacetBucket{Key: key, Label: key, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func normalizedLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return "en"
	}
	return "zh"
}

func matchesKeyword(item CatalogListItem, keyword string) bool {
	parts := []string{strings.ToLower(item.Name), strings.ToLower(item.Summary), strings.ToLower(item.Modality), strings.ToLower(item.Series), strings.ToLower(item.CapabilityType)}
	for _, tag := range item.PlatformTags {
		parts = append(parts, strings.ToLower(tag))
	}
	for _, tag := range item.IndustryTags {
		parts = append(parts, strings.ToLower(tag))
	}
	for _, p := range parts {
		if strings.Contains(p, keyword) {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}
