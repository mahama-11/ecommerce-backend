package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TemplateCenterRepository struct{ db *gorm.DB }

func NewTemplateCenterRepository(db *gorm.DB) *TemplateCenterRepository {
	return &TemplateCenterRepository{db: db}
}

type TemplateCatalogFilter struct {
	Locale       string
	Keyword      string
	Modality     string
	Series       string
	Capability   string
	Platform     string
	SortBy       string
	FeaturedOnly bool
	Limit        int
	Offset       int
}

type CatalogFacetBucket struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

type CatalogFacets struct {
	Platforms    []CatalogFacetBucket `json:"platforms"`
	Modalities   []CatalogFacetBucket `json:"modalities"`
	Series       []CatalogFacetBucket `json:"series"`
	Capabilities []CatalogFacetBucket `json:"capabilities"`
}

type CatalogListItem struct {
	ID              string   `json:"id"`
	Slug            string   `json:"slug"`
	ExternalCode    string   `json:"externalCode,omitempty"`
	Name            string   `json:"name"`
	Summary         string   `json:"summary"`
	Modality        string   `json:"modality"`
	ExecutorType    string   `json:"executorType"`
	Series          string   `json:"series"`
	CapabilityType  string   `json:"capabilityType"`
	InteractionMode string   `json:"interactionMode"`
	CoverAssetURL   string   `json:"coverAssetUrl,omitempty"`
	PlatformTags    []string `json:"platformTags"`
	IndustryTags    []string `json:"industryTags"`
	ScenarioTags    []string `json:"scenarioTags"`
	IsFeatured      bool     `json:"isFeatured"`
	RecommendScore  int      `json:"recommendScore"`
	IsFavorited     bool     `json:"isFavorited"`
	FavoriteCount   int64    `json:"favoriteCount"`
	UseCount        int64    `json:"useCount"`
	SuccessRateHint float64  `json:"successRateHint"`
}

type CatalogDetail struct {
	Catalog  CatalogListItem     `json:"catalog"`
	Locale   CatalogLocaleDTO    `json:"locale"`
	Version  CatalogVersionDTO   `json:"version"`
	Schema   CatalogSchemaDTO    `json:"schema"`
	Examples []CatalogExampleDTO `json:"examples"`
}

type RuntimePromptTemplate struct {
	TemplateID       string         `json:"templateId"`
	ExternalCode     string         `json:"externalCode,omitempty"`
	Slug             string         `json:"slug"`
	Name             string         `json:"name"`
	ToolSlug         string         `json:"toolSlug,omitempty"`
	PromptLayers     map[string]any `json:"promptLayers"`
	DefaultVariables map[string]any `json:"defaultVariables"`
}

type CatalogLocaleDTO struct {
	Description         string `json:"description"`
	ScenarioDescription string `json:"scenarioDescription,omitempty"`
	InputDescription    string `json:"inputDescription,omitempty"`
	OutputDescription   string `json:"outputDescription,omitempty"`
}

type CatalogVersionDTO struct {
	ID             string `json:"id"`
	VersionNo      int    `json:"versionNo"`
	VersionLabel   string `json:"versionLabel"`
	Status         string `json:"status"`
	SourceAssetRef string `json:"sourceAssetRef,omitempty"`
}

type CatalogSchemaDTO struct {
	InputSchema      map[string]any `json:"inputSchema"`
	OutputSchema     map[string]any `json:"outputSchema"`
	ExecutionSchema  map[string]any `json:"executionSchema"`
	PromptLayers     map[string]any `json:"promptLayers"`
	PolicySchema     map[string]any `json:"policySchema,omitempty"`
	DefaultVariables map[string]any `json:"defaultVariables"`
	ToolBinding      map[string]any `json:"toolBinding"`
}

type CatalogExampleDTO struct {
	ID              string `json:"id"`
	ExampleType     string `json:"exampleType"`
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	AssetRef        string `json:"assetRef,omitempty"`
	SourceRef       string `json:"sourceRef,omitempty"`
	StorageKey      string `json:"storageKey,omitempty"`
	AssetID         string `json:"assetId,omitempty"`
	MimeType        string `json:"mimeType,omitempty"`
	Checksum        string `json:"checksum,omitempty"`
	InputAssetURL   string `json:"inputAssetUrl,omitempty"`
	OutputAssetURL  string `json:"outputAssetUrl,omitempty"`
	PreviewAssetURL string `json:"previewAssetUrl,omitempty"`
	VideoPosterURL  string `json:"videoPosterUrl,omitempty"`
}

type UseTemplateResponse struct {
	TargetRoute              string         `json:"targetRoute"`
	ExecutorType             string         `json:"executorType"`
	ToolSlug                 string         `json:"toolSlug,omitempty"`
	PrefilledInputSchema     map[string]any `json:"prefilledInputSchema"`
	PreloadedTemplatePayload map[string]any `json:"preloadedTemplatePayload"`
	SupportsAsyncJob         bool           `json:"supportsAsyncJob"`
	SupportsBatch            bool           `json:"supportsBatch"`
}

type TemplateInstanceListItem struct {
	ID               string         `json:"id"`
	PresetTemplateID string         `json:"presetTemplateId,omitempty"`
	Title            string         `json:"title"`
	Summary          string         `json:"summary"`
	Scenario         string         `json:"scenario"`
	Modality         string         `json:"modality"`
	ExecutorType     string         `json:"executorType"`
	Series           string         `json:"series"`
	CapabilityType   string         `json:"capabilityType"`
	PlatformTags     []string       `json:"platformTags"`
	IndustryTags     []string       `json:"industryTags"`
	SourceType       string         `json:"sourceType"`
	SourceLabel      string         `json:"sourceLabel,omitempty"`
	Status           string         `json:"status"`
	IsFavorite       bool           `json:"isFavorite"`
	SavedAt          string         `json:"savedAt"`
	UpdatedAt        string         `json:"updatedAt"`
	EditableSchema   map[string]any `json:"editableSchema"`
	PromptLayers     map[string]any `json:"promptLayers"`
}

type SeedCatalog struct {
	Catalog  models.TemplateCatalog
	Locales  []models.TemplateCatalogLocale
	Version  models.TemplateCatalogVersion
	Schema   models.TemplateCatalogSchema
	Examples []models.TemplateCatalogExample
}

type SeedBuiltinSummary struct {
	CatalogCount int64
	VersionCount int64
	ExampleCount int64
}

func (r *TemplateCenterRepository) SeedIfEmpty(items []SeedCatalog) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		seedIDs := make([]string, 0, len(items))
		for _, item := range items {
			seedIDs = append(seedIDs, item.Catalog.ID)
		}

		var obsoleteCatalogIDs []string
		if len(seedIDs) > 0 {
			if err := tx.Model(&models.TemplateCatalog{}).
				Where("scope = ? AND managed_source = ? AND id NOT IN ?", "official", "seed_builtin", seedIDs).
				Pluck("id", &obsoleteCatalogIDs).Error; err != nil {
				return err
			}
		}
		if len(obsoleteCatalogIDs) > 0 {
			var obsoleteVersionIDs []string
			if err := tx.Model(&models.TemplateCatalogVersion{}).
				Where("template_catalog_id IN ?", obsoleteCatalogIDs).
				Pluck("id", &obsoleteVersionIDs).Error; err != nil {
				return err
			}
			if len(obsoleteVersionIDs) > 0 {
				if err := tx.Where("template_version_id IN ?", obsoleteVersionIDs).Delete(&models.TemplateCatalogExample{}).Error; err != nil {
					return err
				}
				if err := tx.Where("template_version_id IN ?", obsoleteVersionIDs).Delete(&models.TemplateCatalogSchema{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("template_catalog_id IN ?", obsoleteCatalogIDs).Delete(&models.TemplateFavorite{}).Error; err != nil {
				return err
			}
			if err := tx.Where("template_catalog_id IN ?", obsoleteCatalogIDs).Delete(&models.TemplateUsageEvent{}).Error; err != nil {
				return err
			}
			if err := tx.Where("template_catalog_id IN ?", obsoleteCatalogIDs).Delete(&models.TemplateCatalogLocale{}).Error; err != nil {
				return err
			}
			if err := tx.Where("template_catalog_id IN ?", obsoleteCatalogIDs).Delete(&models.TemplateCatalogVersion{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", obsoleteCatalogIDs).Delete(&models.TemplateCatalog{}).Error; err != nil {
				return err
			}
		}

		for _, item := range items {
			versionID := item.Version.ID
			item.Catalog.CurrentVersionID = versionID
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"slug",
					"external_code",
					"scope",
					"managed_source",
					"modality",
					"executor_type",
					"series",
					"capability_type",
					"interaction_mode",
					"status",
					"current_version_id",
					"default_locale",
					"cover_asset_url",
					"icon_asset_url",
					"platform_tags_json",
					"industry_tags_json",
					"scenario_tags_json",
					"compliance_tags_json",
					"is_featured",
					"recommend_score",
					"sort_order",
					"cost_estimate_min",
					"cost_estimate_max",
					"success_rate_hint",
					"owner_team",
					"created_by",
					"updated_by",
					"published_at",
					"archived_at",
					"updated_at",
				}),
				Where: clause.Where{Exprs: []clause.Expression{
					clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "managed_source"}, Value: "seed_builtin"},
				}},
			}).Create(&item.Catalog).Error; err != nil {
				return err
			}
			var existingCatalog models.TemplateCatalog
			if err := tx.Select("id", "managed_source", "scope", "owner_team", "created_by").
				Where("id = ?", item.Catalog.ID).
				First(&existingCatalog).Error; err != nil {
				return err
			}
			managedSource := existingCatalog.ManagedSource
			if managedSource == "ops_manual" &&
				existingCatalog.Scope == "official" &&
				existingCatalog.OwnerTeam == "agent-ecommerce" &&
				existingCatalog.CreatedBy == "system" {
				if err := tx.Model(&models.TemplateCatalog{}).
					Where("id = ?", item.Catalog.ID).
					Update("managed_source", "seed_builtin").Error; err != nil {
					return err
				}
				managedSource = "seed_builtin"
			}
			if managedSource != "seed_builtin" {
				// Never mutate operator-created official templates even if IDs collide with built-in seeds.
				continue
			}
			if err := tx.Where("template_catalog_id = ?", item.Catalog.ID).Delete(&models.TemplateCatalogLocale{}).Error; err != nil {
				return err
			}
			if len(item.Locales) > 0 {
				for i := range item.Locales {
					item.Locales[i].TemplateCatalogID = item.Catalog.ID
				}
				if err := tx.Create(&item.Locales).Error; err != nil {
					return err
				}
			}
			var existingVersionIDs []string
			if err := tx.Model(&models.TemplateCatalogVersion{}).
				Where("template_catalog_id = ?", item.Catalog.ID).
				Pluck("id", &existingVersionIDs).Error; err != nil {
				return err
			}
			if len(existingVersionIDs) > 0 {
				if err := tx.Where("template_version_id IN ?", existingVersionIDs).Delete(&models.TemplateCatalogExample{}).Error; err != nil {
					return err
				}
				if err := tx.Where("template_version_id IN ?", existingVersionIDs).Delete(&models.TemplateCatalogSchema{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("template_catalog_id = ?", item.Catalog.ID).Delete(&models.TemplateCatalogVersion{}).Error; err != nil {
				return err
			}
			item.Version.TemplateCatalogID = item.Catalog.ID
			if err := tx.Create(&item.Version).Error; err != nil {
				return err
			}
			item.Schema.TemplateVersionID = item.Version.ID
			if err := tx.Create(&item.Schema).Error; err != nil {
				return err
			}
			for i := range item.Examples {
				item.Examples[i].TemplateVersionID = item.Version.ID
			}
			if len(item.Examples) > 0 {
				if err := tx.Create(&item.Examples).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r *TemplateCenterRepository) SeedBuiltinSummary() (*SeedBuiltinSummary, error) {
	summary := &SeedBuiltinSummary{}
	if err := r.db.Model(&models.TemplateCatalog{}).
		Where("scope = ? AND managed_source = ?", "official", "seed_builtin").
		Count(&summary.CatalogCount).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&models.TemplateCatalogVersion{}).
		Joins("JOIN ecommerce_template_catalogs c ON c.id = ecommerce_template_catalog_versions.template_catalog_id").
		Where("c.scope = ? AND c.managed_source = ?", "official", "seed_builtin").
		Count(&summary.VersionCount).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&models.TemplateCatalogExample{}).
		Joins("JOIN ecommerce_template_catalog_versions v ON v.id = ecommerce_template_catalog_examples.template_version_id").
		Joins("JOIN ecommerce_template_catalogs c ON c.id = v.template_catalog_id").
		Where("c.scope = ? AND c.managed_source = ?", "official", "seed_builtin").
		Count(&summary.ExampleCount).Error; err != nil {
		return nil, err
	}
	return summary, nil
}

func (r *TemplateCenterRepository) ListCatalog(scope Scope, filter TemplateCatalogFilter) ([]CatalogListItem, error) {
	locale := normalizedLocale(filter.Locale)
	var catalogs []models.TemplateCatalog
	q := r.applyCatalogFilter(r.db.Model(&models.TemplateCatalog{}), filter)
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	q = applyCatalogSort(q, filter.SortBy)
	if err := q.Find(&catalogs).Error; err != nil {
		return nil, err
	}
	locales, localeMap, err := r.loadLocales(locale, catalogs)
	if err != nil {
		return nil, err
	}
	_ = locales
	favoriteSet, _ := r.favoriteSet(scope, catalogs)
	favoriteCounts, _ := r.favoriteCounts(catalogs)
	useCounts, _ := r.useCounts(catalogs)
	items := make([]CatalogListItem, 0, len(catalogs))
	keyword := strings.ToLower(strings.TrimSpace(filter.Keyword))
	for _, item := range catalogs {
		loc := localeMap[item.ID]
		candidate := toCatalogListItem(item, loc, favoriteSet[item.ID], favoriteCounts[item.ID], useCounts[item.ID])
		if keyword != "" && !matchesKeyword(candidate, keyword) {
			continue
		}
		items = append(items, candidate)
	}
	sortCatalogItems(items, filter.SortBy)
	return items, nil
}

func (r *TemplateCenterRepository) ListFacets(filter TemplateCatalogFilter) (*CatalogFacets, error) {
	locale := normalizedLocale(filter.Locale)
	var catalogs []models.TemplateCatalog
	if err := r.applyCatalogFilter(r.db.Model(&models.TemplateCatalog{}), filter).Find(&catalogs).Error; err != nil {
		return nil, err
	}
	locales, localeMap, err := r.loadLocales(locale, catalogs)
	if err != nil {
		return nil, err
	}
	_ = locales

	facets := &CatalogFacets{
		Platforms:    make([]CatalogFacetBucket, 0),
		Modalities:   make([]CatalogFacetBucket, 0),
		Series:       make([]CatalogFacetBucket, 0),
		Capabilities: make([]CatalogFacetBucket, 0),
	}
	platformCount := map[string]int64{}
	modalityCount := map[string]int64{}
	seriesCount := map[string]int64{}
	capabilityCount := map[string]int64{}

	keyword := strings.ToLower(strings.TrimSpace(filter.Keyword))
	for _, item := range catalogs {
		candidate := toCatalogListItem(item, localeMap[item.ID], false, 0, 0)
		if keyword != "" && !matchesKeyword(candidate, keyword) {
			continue
		}
		for _, tag := range candidate.PlatformTags {
			platformCount[tag]++
		}
		modalityCount[candidate.Modality]++
		seriesCount[candidate.Series]++
		capabilityCount[candidate.CapabilityType]++
	}

	facets.Platforms = sortedFacetBuckets(platformCount)
	facets.Modalities = sortedFacetBuckets(modalityCount)
	facets.Series = sortedFacetBuckets(seriesCount)
	facets.Capabilities = sortedFacetBuckets(capabilityCount)
	return facets, nil
}

func (r *TemplateCenterRepository) GetCatalogDetail(scope Scope, templateID, locale string) (*CatalogDetail, error) {
	var catalog models.TemplateCatalog
	if err := r.db.Where("id = ? AND status = ?", templateID, "published").First(&catalog).Error; err != nil {
		return nil, err
	}
	loc, err := r.getLocale(templateID, normalizedLocale(locale))
	if err != nil {
		return nil, err
	}
	var version models.TemplateCatalogVersion
	if err := r.db.Where("id = ?", catalog.CurrentVersionID).First(&version).Error; err != nil {
		return nil, err
	}
	var schema models.TemplateCatalogSchema
	if err := r.db.Where("template_version_id = ?", version.ID).First(&schema).Error; err != nil {
		return nil, err
	}
	var examples []models.TemplateCatalogExample
	if err := r.db.Where("template_version_id = ?", version.ID).Order("sort_order asc").Find(&examples).Error; err != nil {
		return nil, err
	}
	favoriteSet, _ := r.favoriteSet(scope, []models.TemplateCatalog{catalog})
	favoriteCounts, _ := r.favoriteCounts([]models.TemplateCatalog{catalog})
	useCounts, _ := r.useCounts([]models.TemplateCatalog{catalog})
	item := toCatalogListItem(catalog, loc, favoriteSet[catalog.ID], favoriteCounts[catalog.ID], useCounts[catalog.ID])
	result := &CatalogDetail{
		Catalog: item,
		Locale: CatalogLocaleDTO{
			Description:         loc.Description,
			ScenarioDescription: loc.ScenarioDescription,
			InputDescription:    loc.InputDescription,
			OutputDescription:   loc.OutputDescription,
		},
		Version: CatalogVersionDTO{ID: version.ID, VersionNo: version.VersionNo, VersionLabel: version.VersionLabel, Status: version.Status, SourceAssetRef: version.SourceAssetRef},
		Schema: CatalogSchemaDTO{
			InputSchema:      decodeJSONMap(schema.InputSchemaJSON),
			OutputSchema:     decodeJSONMap(schema.OutputSchemaJSON),
			ExecutionSchema:  decodeJSONMap(schema.ExecutionSchemaJSON),
			PromptLayers:     decodeJSONMap(schema.PromptLayersJSON),
			PolicySchema:     decodeJSONMap(schema.PolicySchemaJSON),
			DefaultVariables: decodeJSONMap(schema.DefaultVariablesJSON),
			ToolBinding:      decodeJSONMap(schema.ToolBindingJSON),
		},
		Examples: toExamples(examples),
	}
	return result, nil
}

func (r *TemplateCenterRepository) ResolveRuntimePromptTemplate(ref string) (*RuntimePromptTemplate, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}

	var catalog models.TemplateCatalog
	err := r.db.
		Where("(id = ? OR external_code = ? OR slug = ?) AND status = ?", ref, ref, ref, "published").
		First(&catalog).Error
	switch {
	case err == nil:
		return r.loadRuntimePromptTemplate(catalog)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}

	var locale models.TemplateCatalogLocale
	err = r.db.Where("name = ?", ref).Order("updated_at desc").First(&locale).Error
	switch {
	case err == nil:
		lookupErr := r.db.Where("id = ? AND status = ?", locale.TemplateCatalogID, "published").First(&catalog).Error
		if lookupErr != nil {
			if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, lookupErr
		}
		return r.loadRuntimePromptTemplate(catalog)
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	default:
		return nil, err
	}
}

func (r *TemplateCenterRepository) loadRuntimePromptTemplate(catalog models.TemplateCatalog) (*RuntimePromptTemplate, error) {
	if strings.TrimSpace(catalog.CurrentVersionID) == "" {
		return nil, nil
	}

	var schema models.TemplateCatalogSchema
	if err := r.db.Where("template_version_id = ?", catalog.CurrentVersionID).First(&schema).Error; err != nil {
		return nil, err
	}
	loc, err := r.getLocale(catalog.ID, "zh")
	if err != nil {
		return nil, err
	}
	toolBinding := decodeJSONMap(schema.ToolBindingJSON)
	return &RuntimePromptTemplate{
		TemplateID:       catalog.ID,
		ExternalCode:     catalog.ExternalCode,
		Slug:             catalog.Slug,
		Name:             loc.Name,
		ToolSlug:         stringValue(toolBinding["toolSlug"]),
		PromptLayers:     decodeJSONMap(schema.PromptLayersJSON),
		DefaultVariables: decodeJSONMap(schema.DefaultVariablesJSON),
	}, nil
}

func (r *TemplateCenterRepository) ListFavorites(scope Scope, locale string) ([]CatalogListItem, error) {
	if scope.UserID == "" || scope.OrgID == "" {
		return []CatalogListItem{}, nil
	}
	var favorites []models.TemplateFavorite
	if err := r.db.Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).Find(&favorites).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(favorites))
	for _, item := range favorites {
		ids = append(ids, item.TemplateCatalogID)
	}
	if len(ids) == 0 {
		return []CatalogListItem{}, nil
	}
	var catalogs []models.TemplateCatalog
	if err := r.db.Where("id IN ?", ids).Order("is_featured desc").Order("recommend_score desc").Find(&catalogs).Error; err != nil {
		return nil, err
	}
	_, localeMap, err := r.loadLocales(normalizedLocale(locale), catalogs)
	if err != nil {
		return nil, err
	}
	favoriteSet, _ := r.favoriteSet(scope, catalogs)
	favoriteCounts, _ := r.favoriteCounts(catalogs)
	useCounts, _ := r.useCounts(catalogs)
	items := make([]CatalogListItem, 0, len(catalogs))
	for _, item := range catalogs {
		items = append(items, toCatalogListItem(item, localeMap[item.ID], favoriteSet[item.ID], favoriteCounts[item.ID], useCounts[item.ID]))
	}
	return items, nil
}

func (r *TemplateCenterRepository) AddFavorite(scope Scope, templateID string) error {
	item := models.TemplateFavorite{
		ID:                buildID("fav"),
		TemplateCatalogID: templateID,
		UserID:            scope.UserID,
		OrganizationID:    scope.OrgID,
		CreatedAt:         time.Now(),
	}
	return r.db.Where("template_catalog_id = ? AND user_id = ? AND organization_id = ?", templateID, scope.UserID, scope.OrgID).FirstOrCreate(&item).Error
}

func (r *TemplateCenterRepository) FavoriteTemplateIDs(scope Scope) ([]string, error) {
	if scope.UserID == "" || scope.OrgID == "" {
		return []string{}, nil
	}
	var ids []string
	if err := r.db.Model(&models.TemplateFavorite{}).
		Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).
		Pluck("template_catalog_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *TemplateCenterRepository) RemoveFavorite(scope Scope, templateID string) error {
	return r.db.Where("template_catalog_id = ? AND user_id = ? AND organization_id = ?", templateID, scope.UserID, scope.OrgID).Delete(&models.TemplateFavorite{}).Error
}

func (r *TemplateCenterRepository) CopyToMyTemplates(scope Scope, templateID string) (*models.TemplateInstance, error) {
	detail, err := r.GetCatalogDetail(scope, templateID, "zh")
	if err != nil {
		return nil, err
	}
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
	if err := r.db.Create(instance).Error; err != nil {
		return nil, err
	}
	return instance, nil
}

func (r *TemplateCenterRepository) CreateTemplateInstance(item *models.TemplateInstance) error {
	return r.db.Create(item).Error
}

func (r *TemplateCenterRepository) BuildUseResponse(scope Scope, templateID string) (*UseTemplateResponse, error) {
	detail, err := r.GetCatalogDetail(scope, templateID, "zh")
	if err != nil {
		return nil, err
	}
	execution := detail.Schema.ExecutionSchema
	toolBinding := detail.Schema.ToolBinding
	resp := &UseTemplateResponse{
		TargetRoute:          stringValue(execution["route"]),
		ExecutorType:         detail.Catalog.ExecutorType,
		ToolSlug:             stringValue(toolBinding["toolSlug"]),
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

func toCatalogListItem(item models.TemplateCatalog, locale models.TemplateCatalogLocale, isFavorited bool, favoriteCount, useCount int64) CatalogListItem {
	return CatalogListItem{
		ID:              item.ID,
		Slug:            item.Slug,
		ExternalCode:    item.ExternalCode,
		Name:            locale.Name,
		Summary:         locale.Summary,
		Modality:        item.Modality,
		ExecutorType:    item.ExecutorType,
		Series:          item.Series,
		CapabilityType:  item.CapabilityType,
		InteractionMode: item.InteractionMode,
		CoverAssetURL:   item.CoverAssetURL,
		PlatformTags:    decodeStringArray(item.PlatformTagsJSON),
		IndustryTags:    decodeStringArray(item.IndustryTagsJSON),
		ScenarioTags:    decodeStringArray(item.ScenarioTagsJSON),
		IsFeatured:      item.IsFeatured,
		RecommendScore:  item.RecommendScore,
		IsFavorited:     isFavorited,
		FavoriteCount:   favoriteCount,
		UseCount:        useCount,
		SuccessRateHint: item.SuccessRateHint,
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
