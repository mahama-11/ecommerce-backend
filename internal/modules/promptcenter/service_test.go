package promptcenter

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPreviewSuccess(t *testing.T) {
	db := newPromptCenterTestDB(t)
	seedPromptCenterFixtures(t, db)
	service := newPromptCenterTestService(db)

	resp, err := service.Preview("user-1", "org-1", PreviewPromptInput{
		ProductID:    "product-1",
		SKUCode:      "SKU-001",
		TemplateID:   "tpl-1",
		ToolSlug:     "ai-posture",
		SceneType:    "ai_posture",
		Variables:    map[string]any{"prompt": "生成适合女装详情页的自然站姿", "negative_prompt": "blur"},
		SourceAssets: []SourceAssetBinding{{Slot: "source_image", AssetID: "asset-1"}},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if resp.PromptID == "" || !strings.HasPrefix(resp.ContentHash, "sha256:") || resp.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected response identity/hash: %+v", resp)
	}
	if resp.TemplateID != "tpl-1" || resp.TemplateVersionID != "tplv-1" || resp.TemplateCode != "ai-posture-template" {
		t.Fatalf("unexpected template contract: %+v", resp)
	}
	if !resp.Validation.Valid || len(resp.Validation.Errors) != 0 {
		t.Fatalf("expected valid response: %+v", resp.Validation)
	}
	if !strings.Contains(resp.Compiled.FinalPrompt, "[SYSTEM INSTRUCTION]") || !strings.Contains(resp.Compiled.FinalPrompt, "生成适合女装详情页的自然站姿") {
		t.Fatalf("compiled prompt missing expected sections: %s", resp.Compiled.FinalPrompt)
	}
	if _, ok := resp.SourceMap["source_assets"].(map[string]any); !ok {
		t.Fatalf("source map missing source_assets: %+v", resp.SourceMap)
	}

	stored, err := repository.NewPromptCenterRepository(db).FindPromptRunByID("org-1", resp.PromptID)
	if err != nil {
		t.Fatalf("stored prompt run not found: %v", err)
	}
	if stored.ContentHash != resp.ContentHash || stored.Status != "validated" {
		t.Fatalf("unexpected stored prompt run: %+v", stored)
	}
}

func TestPreviewSourceAssetProductMismatchValidation(t *testing.T) {
	db := newPromptCenterTestDB(t)
	seedPromptCenterFixtures(t, db)
	service := newPromptCenterTestService(db)

	resp, err := service.Preview("user-1", "org-1", PreviewPromptInput{
		ProductID:    "product-1",
		SKUCode:      "SKU-001",
		TemplateID:   "tpl-1",
		ToolSlug:     "ai-posture",
		SceneType:    "ai_posture",
		Variables:    map[string]any{"prompt": "test"},
		SourceAssets: []SourceAssetBinding{{Slot: "source_image", AssetID: "asset-other-product"}},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if resp.Validation.Valid || resp.Status != "draft" {
		t.Fatalf("expected invalid draft validation, got status=%s validation=%+v", resp.Status, resp.Validation)
	}
	found := false
	for _, item := range resp.Validation.Errors {
		if item.Code == "SOURCE_ASSET_PRODUCT_MISMATCH" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SOURCE_ASSET_PRODUCT_MISMATCH error, got %+v", resp.Validation.Errors)
	}
}

func newPromptCenterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.EcommercePromptRun{}, &models.EcommerceAsset{}, &models.EcomProductSKU{}, &models.TemplateCatalog{}, &models.TemplateCatalogLocale{}, &models.TemplateCatalogVersion{}, &models.TemplateCatalogSchema{}, &models.TemplateCatalogExample{}, &models.TemplateFavorite{}, &models.TemplateUsageEvent{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func newPromptCenterTestService(db *gorm.DB) *Service {
	return NewService(repository.NewPromptCenterRepository(db), repository.NewTemplateCenterRepository(db), repository.NewImageRuntimeRepository(db), repository.NewProductCenterRepository(db), config.AppConfig{
		ProductCode: "ecommerce",
		ImageRuntime: config.ImageRuntimeConfig{GlobalNegativePrompt: "blurry, watermark", ScenePromptPolicies: map[string]config.ScenePromptPolicyConfig{
			"ai_posture": {ToolSlug: "ai-posture", DisplayName: "姿势裂变", SystemPrompt: "保持模特身份和服装细节", DefaultNegativePrompt: "identity drift"},
		}},
	})
}

func seedPromptCenterFixtures(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	items := []any{
		&models.EcomProductSKU{ID: "product-1", OrganizationID: "org-1", SKUCode: "SKU-001", Title: "Product 1", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending},
		&models.EcomProductSKU{ID: "product-2", OrganizationID: "org-1", SKUCode: "SKU-002", Title: "Product 2", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending},
		&models.EcommerceAsset{ID: "asset-1", OrganizationID: "org-1", UserID: "user-1", AssetType: "source", SourceType: "upload", StorageKey: "sources/1.png", MimeType: "image/png", Metadata: `{"product_id":"product-1","sku_code":"SKU-001"}`},
		&models.EcommerceAsset{ID: "asset-other-product", OrganizationID: "org-1", UserID: "user-1", AssetType: "source", SourceType: "upload", StorageKey: "sources/2.png", MimeType: "image/png", Metadata: `{"product_id":"product-2","sku_code":"SKU-002"}`},
		&models.TemplateCatalog{ID: "tpl-1", Slug: "tpl-1", ExternalCode: "ai-posture-template", Scope: "official", ManagedSource: "seed_builtin", Modality: "image", ExecutorType: "image_generation", Series: "model", CapabilityType: "ai_posture", InteractionMode: "image_to_image", Status: "published", CurrentVersionID: "tplv-1", DefaultLocale: "zh", PlatformTagsJSON: `[]`, IndustryTagsJSON: `[]`, ScenarioTagsJSON: `[]`, ComplianceTagsJSON: `[]`, PublishedAt: &now},
		&models.TemplateCatalogLocale{ID: "tpl-loc-1", TemplateCatalogID: "tpl-1", Locale: "zh", Name: "姿势模板", Summary: "summary", Description: "description"},
		&models.TemplateCatalogVersion{ID: "tplv-1", TemplateCatalogID: "tpl-1", VersionNo: 1, VersionLabel: "v1", Status: "published", IsPublishable: true, IsDefault: true, PublishedAt: &now},
		&models.TemplateCatalogSchema{ID: "tpls-1", TemplateVersionID: "tplv-1", InputSchemaJSON: `{}`, OutputSchemaJSON: `{}`, ExecutionSchemaJSON: `{}`, PromptLayersJSON: `{"l1":{"content":"模板系统提示：保持主体一致"},"l2":{"content":"电商棚拍光线"}}`, DefaultVariablesJSON: `{}`, ToolBindingJSON: `{"toolSlug":"ai-posture","targetRoute":"/tools/ai-posture"}`},
	}
	for _, item := range items {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("seed fixture %T: %v", item, err)
		}
	}
}
