package templatecenter

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
)

//go:embed generated_seed_definitions.json
var generatedSeedDefinitionsRaw []byte

type generatedSeedLocale struct {
	Name              string `json:"name"`
	Summary           string `json:"summary"`
	Description       string `json:"description"`
	InputDescription  string `json:"inputDescription"`
	OutputDescription string `json:"outputDescription"`
}

type generatedSeedExample struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ExampleType     string `json:"exampleType"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	AssetRef        string `json:"assetRef"`
	SourceRef       string `json:"sourceRef"`
	StorageKey      string `json:"storageKey"`
	AssetID         string `json:"assetId"`
	MimeType        string `json:"mimeType"`
	Checksum        string `json:"checksum"`
	InputAssetURL   string `json:"inputAssetUrl"`
	OutputAssetURL  string `json:"outputAssetUrl"`
	Preview         string `json:"preview"`
	PreviewAssetURL string `json:"previewAssetUrl"`
	VideoPosterURL  string `json:"videoPosterUrl"`
}

type generatedSeedDefinition struct {
	ID               string                 `json:"id"`
	ExternalCode     string                 `json:"externalCode"`
	Slug             string                 `json:"slug"`
	Modality         string                 `json:"modality"`
	ExecutorType     string                 `json:"executorType"`
	Series           string                 `json:"series"`
	CapabilityType   string                 `json:"capabilityType"`
	InteractionMode  string                 `json:"interactionMode"`
	Featured         bool                   `json:"featured"`
	RecommendScore   int                    `json:"recommendScore"`
	SourceAssetRef   string                 `json:"sourceAssetRef"`
	PlatformTags     []string               `json:"platformTags"`
	IndustryTags     []string               `json:"industryTags"`
	ScenarioTags     []string               `json:"scenarioTags"`
	ExecutionSchema  map[string]any         `json:"executionSchema"`
	ToolBinding      map[string]any         `json:"toolBinding"`
	InputSchema      map[string]any         `json:"inputSchema"`
	OutputSchema     map[string]any         `json:"outputSchema"`
	PromptLayers     map[string]any         `json:"promptLayers"`
	DefaultVariables map[string]any         `json:"defaultVariables"`
	Examples         []generatedSeedExample `json:"examples"`
	LocaleZH         generatedSeedLocale    `json:"localeZH"`
	LocaleEN         generatedSeedLocale    `json:"localeEN"`
}

func seedCatalogs() []repository.SeedCatalog {
	var definitions []generatedSeedDefinition
	if err := json.Unmarshal(generatedSeedDefinitionsRaw, &definitions); err != nil {
		panic(fmt.Sprintf("unmarshal generated template center seed definitions: %v", err))
	}

	now := time.Now()
	items := make([]repository.SeedCatalog, 0, len(definitions))
	for _, def := range definitions {
		items = append(items, buildSeedCatalog(now, def))
	}
	return items
}

func buildSeedCatalog(now time.Time, def generatedSeedDefinition) repository.SeedCatalog {
	versionID := def.ID + "_v1"
	catalog := models.TemplateCatalog{
		ID:                 def.ID,
		Slug:               def.Slug,
		ExternalCode:       def.ExternalCode,
		Scope:              "official",
		ManagedSource:      "seed_builtin",
		Modality:           def.Modality,
		ExecutorType:       def.ExecutorType,
		Series:             def.Series,
		CapabilityType:     def.CapabilityType,
		InteractionMode:    def.InteractionMode,
		Status:             "published",
		CurrentVersionID:   versionID,
		DefaultLocale:      "zh",
		CoverAssetURL:      firstPreview(def.Examples),
		PlatformTagsJSON:   mustJSON(def.PlatformTags),
		IndustryTagsJSON:   mustJSON(def.IndustryTags),
		ScenarioTagsJSON:   mustJSON(def.ScenarioTags),
		ComplianceTagsJSON: mustJSON([]string{}),
		IsFeatured:         def.Featured,
		RecommendScore:     def.RecommendScore,
		SortOrder:          def.RecommendScore,
		CostEstimateMin:    1,
		CostEstimateMax:    3,
		SuccessRateHint:    92,
		OwnerTeam:          "agent-ecommerce",
		CreatedBy:          "system",
		UpdatedBy:          "system",
		CreatedAt:          now,
		UpdatedAt:          now,
		PublishedAt:        &now,
	}
	version := models.TemplateCatalogVersion{
		ID:             versionID,
		VersionNo:      1,
		VersionLabel:   "v1",
		Status:         "published",
		SourceAssetRef: def.SourceAssetRef,
		IsPublishable:  true,
		IsDefault:      true,
		CreatedBy:      "system",
		PublishedBy:    "system",
		CreatedAt:      now,
		PublishedAt:    &now,
	}
	schema := models.TemplateCatalogSchema{
		ID:                   versionID + "_schema",
		TemplateVersionID:    versionID,
		InputSchemaJSON:      mustJSON(def.InputSchema),
		OutputSchemaJSON:     mustJSON(def.OutputSchema),
		ExecutionSchemaJSON:  mustJSON(def.ExecutionSchema),
		PromptLayersJSON:     mustJSON(def.PromptLayers),
		PolicySchemaJSON:     "{}",
		DefaultVariablesJSON: mustJSON(def.DefaultVariables),
		ToolBindingJSON:      mustJSON(def.ToolBinding),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	locales := []models.TemplateCatalogLocale{
		{
			ID:                  def.ID + "_zh",
			TemplateCatalogID:   def.ID,
			Locale:              "zh",
			Name:                def.LocaleZH.Name,
			Summary:             def.LocaleZH.Summary,
			Description:         def.LocaleZH.Description,
			ScenarioDescription: def.LocaleZH.Summary,
			InputDescription:    def.LocaleZH.InputDescription,
			OutputDescription:   def.LocaleZH.OutputDescription,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		{
			ID:                  def.ID + "_en",
			TemplateCatalogID:   def.ID,
			Locale:              "en",
			Name:                def.LocaleEN.Name,
			Summary:             def.LocaleEN.Summary,
			Description:         def.LocaleEN.Description,
			ScenarioDescription: def.LocaleEN.Summary,
			InputDescription:    def.LocaleEN.InputDescription,
			OutputDescription:   def.LocaleEN.OutputDescription,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}
	examples := make([]models.TemplateCatalogExample, 0, len(def.Examples))
	for idx, item := range def.Examples {
		examples = append(examples, models.TemplateCatalogExample{
			ID:                firstNonEmpty(item.ID, fmt.Sprintf("%s_ex_%d", def.ID, idx+1)),
			TemplateVersionID: versionID,
			ExampleType:       firstNonEmpty(item.ExampleType, item.Type),
			Title:             item.Title,
			Description:       item.Description,
			AssetRef:          item.AssetRef,
			SourceRef:         item.SourceRef,
			StorageKey:        item.StorageKey,
			AssetID:           item.AssetID,
			MimeType:          item.MimeType,
			Checksum:          item.Checksum,
			InputAssetURL:     item.InputAssetURL,
			OutputAssetURL:    item.OutputAssetURL,
			PreviewAssetURL:   firstNonEmpty(item.PreviewAssetURL, item.Preview),
			VideoPosterURL:    item.VideoPosterURL,
			SortOrder:         idx,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
	}

	return repository.SeedCatalog{
		Catalog:  catalog,
		Locales:  locales,
		Version:  version,
		Schema:   schema,
		Examples: examples,
	}
}

func mustJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal generated template center seed field: %v", err))
	}
	return string(payload)
}

func firstPreview(items []generatedSeedExample) string {
	if len(items) == 0 {
		return ""
	}
	return firstNonEmpty(items[0].PreviewAssetURL, items[0].Preview)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
