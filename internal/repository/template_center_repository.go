package repository

import (
	"encoding/json"
	"errors"
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
	Locale             string
	Keyword            string
	Modality           string
	Series             string
	Capability         string
	Platform           string
	ToolSlug           string
	InputMode          string
	ProductCategory    string
	Industry           string
	Scenario           string
	ProviderCapability string
	SortBy             string
	FeaturedOnly       bool
	Limit              int
	Offset             int
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
	ID                   string         `json:"id"`
	Slug                 string         `json:"slug"`
	ToolSlug             string         `json:"toolSlug,omitempty"`
	TargetRoute          string         `json:"targetRoute,omitempty"`
	ExternalCode         string         `json:"externalCode,omitempty"`
	Name                 string         `json:"name"`
	Summary              string         `json:"summary"`
	Modality             string         `json:"modality"`
	ExecutorType         string         `json:"executorType"`
	Series               string         `json:"series"`
	CapabilityType       string         `json:"capabilityType"`
	InteractionMode      string         `json:"interactionMode"`
	CoverAssetURL        string         `json:"coverAssetUrl,omitempty"`
	PlatformTags         []string       `json:"platformTags"`
	IndustryTags         []string       `json:"industryTags"`
	ScenarioTags         []string       `json:"scenarioTags"`
	InputModes           []string       `json:"inputModes,omitempty"`
	ProductCategories    []string       `json:"productCategories,omitempty"`
	ProviderCapabilities []string       `json:"providerCapabilities,omitempty"`
	Applicability        map[string]any `json:"applicability,omitempty"`
	IsFeatured           bool           `json:"isFeatured"`
	RecommendScore       int            `json:"recommendScore"`
	IsFavorited          bool           `json:"isFavorited"`
	FavoriteCount        int64          `json:"favoriteCount"`
	UseCount             int64          `json:"useCount"`
	SuccessRateHint      float64        `json:"successRateHint"`
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
	TargetRoute      string         `json:"targetRoute,omitempty"`
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
	InputSchema      map[string]any             `json:"inputSchema"`
	OutputSchema     map[string]any             `json:"outputSchema"`
	ExecutionSchema  map[string]any             `json:"executionSchema"`
	PromptLayers     map[string]any             `json:"promptLayers"`
	PolicySchema     map[string]any             `json:"policySchema,omitempty"`
	DefaultVariables map[string]any             `json:"defaultVariables"`
	ToolBinding      map[string]any             `json:"toolBinding"`
	RequiredAssets   []TemplateRequiredAssetDTO `json:"requiredAssets,omitempty"`
	Applicability    map[string]any             `json:"applicability,omitempty"`
}

type TemplateRequiredAssetDTO struct {
	Slot        string         `json:"slot"`
	Role        string         `json:"role,omitempty"`
	Label       string         `json:"label,omitempty"`
	Required    bool           `json:"required"`
	Constraints map[string]any `json:"constraints,omitempty"`
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
	TargetRoute              string                     `json:"targetRoute"`
	ExecutorType             string                     `json:"executorType"`
	ToolSlug                 string                     `json:"toolSlug,omitempty"`
	InputMode                string                     `json:"inputMode,omitempty"`
	RequiredAssets           []TemplateRequiredAssetDTO `json:"requiredAssets,omitempty"`
	Applicability            map[string]any             `json:"applicability,omitempty"`
	PrefilledInputSchema     map[string]any             `json:"prefilledInputSchema"`
	PreloadedTemplatePayload map[string]any             `json:"preloadedTemplatePayload"`
	SupportsAsyncJob         bool                       `json:"supportsAsyncJob"`
	SupportsBatch            bool                       `json:"supportsBatch"`
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
	schemaBindings, _ := r.loadSchemaBindings(catalogs)
	items := make([]CatalogListItem, 0, len(catalogs))
	keyword := strings.ToLower(strings.TrimSpace(filter.Keyword))
	for _, item := range catalogs {
		loc := localeMap[item.ID]
		candidate := toCatalogListItem(item, loc, favoriteSet[item.ID], favoriteCounts[item.ID], useCounts[item.ID], schemaBindings[item.ID])
		if !matchesCatalogContextFilter(candidate, filter) {
			continue
		}
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
	schemaBindings, _ := r.loadSchemaBindings(catalogs)

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
		candidate := toCatalogListItem(item, localeMap[item.ID], false, 0, 0, schemaBindings[item.ID])
		if !matchesCatalogContextFilter(candidate, filter) {
			continue
		}
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
	item := toCatalogListItem(catalog, loc, favoriteSet[catalog.ID], favoriteCounts[catalog.ID], useCounts[catalog.ID], schemaBindingFromSchema(schema, catalog))
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
			RequiredAssets:   requiredAssetsFromSchema(schema),
			Applicability:    applicabilityFromSchema(schema, catalog),
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
	binding := schemaBindingFromSchema(schema, catalog)
	return &RuntimePromptTemplate{
		TemplateID:       catalog.ID,
		ExternalCode:     catalog.ExternalCode,
		Slug:             catalog.Slug,
		Name:             loc.Name,
		ToolSlug:         binding.ToolSlug,
		TargetRoute:      binding.TargetRoute,
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
	schemaBindings, _ := r.loadSchemaBindings(catalogs)
	items := make([]CatalogListItem, 0, len(catalogs))
	for _, item := range catalogs {
		items = append(items, toCatalogListItem(item, localeMap[item.ID], favoriteSet[item.ID], favoriteCounts[item.ID], useCounts[item.ID], schemaBindings[item.ID]))
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
