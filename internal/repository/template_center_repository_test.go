package repository

import (
	"path/filepath"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/storage"

	"gorm.io/gorm"
)

func TestSeedIfEmptyRepairsHistoricalManagedSourceDrift(t *testing.T) {
	db := newTemplateCenterRepositoryTestDB(t)
	repo := NewTemplateCenterRepository(db)
	now := time.Now()

	item := SeedCatalog{
		Catalog: models.TemplateCatalog{
			ID:              "tpl_m1_t01",
			Slug:            "changing-model-m1-t01-template",
			ExternalCode:    "M1-T01",
			Scope:           "official",
			ManagedSource:   "seed_builtin",
			Modality:        "image",
			ExecutorType:    "image_tool",
			Series:          "model_image",
			CapabilityType:  "model_swap",
			InteractionMode: "upload_form",
			Status:          "published",
			DefaultLocale:   "zh",
			IsFeatured:      true,
			RecommendScore:  9899,
			SortOrder:       9899,
			CostEstimateMin: 1,
			CostEstimateMax: 3,
			SuccessRateHint: 92,
			OwnerTeam:       "agent-ecommerce",
			CreatedBy:       "system",
			UpdatedBy:       "system",
			CreatedAt:       now,
			UpdatedAt:       now,
			PublishedAt:     &now,
		},
		Locales: []models.TemplateCatalogLocale{{
			ID:                  "tpl_m1_t01_zh",
			Locale:              "zh",
			Name:                "欧美白人女模特",
			Summary:             "summary",
			Description:         "description",
			ScenarioDescription: "description",
			InputDescription:    "input",
			OutputDescription:   "output",
			CreatedAt:           now,
			UpdatedAt:           now,
		}},
		Version: models.TemplateCatalogVersion{
			ID:             "tpl_m1_t01_v1",
			VersionNo:      1,
			VersionLabel:   "v1",
			Status:         "published",
			IsPublishable:  true,
			IsDefault:      true,
			SourceAssetRef: "docs/#M1-T01",
			CreatedBy:      "system",
			PublishedBy:    "system",
			CreatedAt:      now,
			PublishedAt:    &now,
		},
		Schema: models.TemplateCatalogSchema{
			ID:                   "tpl_m1_t01_v1_schema",
			InputSchemaJSON:      "{}",
			OutputSchemaJSON:     "{}",
			ExecutionSchemaJSON:  "{}",
			PromptLayersJSON:     "{}",
			PolicySchemaJSON:     "{}",
			DefaultVariablesJSON: "{}",
			ToolBindingJSON:      "{}",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		Examples: []models.TemplateCatalogExample{{
			ID:              "m1_t01_example_1",
			ExampleType:     "reference_image",
			Title:           "欧美白人女模特",
			Description:     "example",
			AssetRef:        "infra/examples/Model/ModelSwap/欧美白人女模特.png",
			SourceRef:       "templates/changing-model/M1-T01/example-1",
			StorageKey:      "",
			PreviewAssetURL: "",
			SortOrder:       0,
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
	}

	if err := repo.SeedIfEmpty([]SeedCatalog{item}); err != nil {
		t.Fatalf("initial seed: %v", err)
	}

	if err := db.Model(&models.TemplateCatalog{}).
		Where("id = ?", item.Catalog.ID).
		Update("managed_source", "ops_manual").Error; err != nil {
		t.Fatalf("drift managed_source: %v", err)
	}
	if err := db.Where("template_version_id = ?", item.Version.ID).Delete(&models.TemplateCatalogExample{}).Error; err != nil {
		t.Fatalf("delete examples: %v", err)
	}

	if err := repo.SeedIfEmpty([]SeedCatalog{item}); err != nil {
		t.Fatalf("reseed drifted catalog: %v", err)
	}

	var managedSource string
	if err := db.Model(&models.TemplateCatalog{}).
		Where("id = ?", item.Catalog.ID).
		Pluck("managed_source", &managedSource).Error; err != nil {
		t.Fatalf("query managed_source: %v", err)
	}
	if managedSource != "seed_builtin" {
		t.Fatalf("managed_source = %q, want seed_builtin", managedSource)
	}

	var count int64
	if err := db.Model(&models.TemplateCatalogExample{}).
		Where("template_version_id = ?", item.Version.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count examples: %v", err)
	}
	if count != 1 {
		t.Fatalf("example count = %d, want 1", count)
	}
}

func newTemplateCenterRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	cfg := config.DatabaseConfig{
		Driver:              "sqlite",
		SQLitePath:          filepath.Join(t.TempDir(), "template-center-repository-test.db"),
		TablePrefix:         "ecommerce_",
		AutoMigrateEnabled:  true,
		AllowStartupMigrate: true,
	}

	db, err := storage.InitDB(cfg, "debug")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	return db
}
