package productcore

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateProductAssetRelation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-asset-1",
		OrganizationID: "org-1",
		SKUCode:        "SKU-ASSET-1",
		Title:          "Asset Product",
		Status:         models.ProductStatusAssetsReady,
		AssetStatus:    models.AssetStatusReady,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
		CreatedBy:      "user-1",
		UpdatedBy:      "user-1",
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	assets := []models.EcommerceAsset{
		{
			ID:             "asset-primary-old",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			FileName:       "old-primary.png",
			MimeType:       "image/png",
		},
		{
			ID:             "asset-secondary",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			FileName:       "secondary.png",
			MimeType:       "image/png",
		},
	}
	for _, asset := range assets {
		item := asset
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create asset: %v", err)
		}
	}
	relations := []models.EcomAssetRelation{
		{
			ID:             "relation-primary-old",
			OrganizationID: "org-1",
			AssetID:        "asset-primary-old",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-asset-1",
			RelationType:   models.AssetRelationTypeResult,
			AssetRole:      models.AssetRoleHero,
			IsPrimary:      true,
			SortOrder:      0,
		},
		{
			ID:             "relation-secondary",
			OrganizationID: "org-1",
			AssetID:        "asset-secondary",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-asset-1",
			RelationType:   models.AssetRelationTypeResult,
			AssetRole:      models.AssetRoleDetailShot,
			IsPrimary:      false,
			SortOrder:      1,
		},
	}
	for _, relation := range relations {
		item := relation
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create relation: %v", err)
		}
	}

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := newProductCoreTestService(t, productRepo, assetRepo)
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.PATCH("/api/v1/ecommerce/products/:product_id/assets/:asset_relation_id", handler.UpdateProductAsset)

	updateBody := map[string]any{
		"asset_role": "model_shot",
		"is_primary": true,
		"sort_order": 5,
	}
	payload, err := json.Marshal(updateBody)
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ecommerce/products/product-asset-1/assets/relation-secondary", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("update product asset status = %d, want %d body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	var result productEnvelope[models.EcomAssetRelation]
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode update product asset response: %v", err)
	}
	if !result.Data.IsPrimary || result.Data.AssetRole != models.AssetRoleModelShot || result.Data.SortOrder != 5 {
		t.Fatalf("unexpected update payload: %+v", result.Data)
	}

	var updated models.EcomAssetRelation
	if err := db.Where("id = ?", "relation-secondary").First(&updated).Error; err != nil {
		t.Fatalf("query updated relation: %v", err)
	}
	if !updated.IsPrimary || updated.AssetRole != models.AssetRoleModelShot {
		t.Fatalf("relation-secondary not updated: %+v", updated)
	}

	var oldPrimary models.EcomAssetRelation
	if err := db.Where("id = ?", "relation-primary-old").First(&oldPrimary).Error; err != nil {
		t.Fatalf("query old primary relation: %v", err)
	}
	if oldPrimary.IsPrimary {
		t.Fatalf("expected old primary relation to be cleared: %+v", oldPrimary)
	}
}

func TestCreateExportTaskSnapshotsAssetsAndListing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-export-snapshot",
		OrganizationID: "org-1",
		SKUCode:        "SKU-EXPORT-1",
		Title:          "Snapshot Product",
		Status:         models.ProductStatusListingReady,
		AssetStatus:    models.AssetStatusReady,
		ListingStatus:  models.ListingStatusReady,
		ExportStatus:   models.ExportStatusPending,
		CreatedBy:      "user-1",
		UpdatedBy:      "user-1",
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	records := []any{
		&models.EcommerceAsset{
			ID:             "asset-export-hero",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			FileName:       "hero.png",
			MimeType:       "image/png",
		},
		&models.EcommerceAsset{
			ID:             "asset-export-detail",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			FileName:       "detail.png",
			MimeType:       "image/png",
		},
		&models.EcomAssetRelation{
			ID:             "relation-export-hero",
			OrganizationID: "org-1",
			AssetID:        "asset-export-hero",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-export-snapshot",
			RelationType:   models.AssetRelationTypePrimary,
			AssetRole:      models.AssetRoleHero,
			IsPrimary:      true,
			SortOrder:      1,
		},
		&models.EcomAssetRelation{
			ID:             "relation-export-detail",
			OrganizationID: "org-1",
			AssetID:        "asset-export-detail",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-export-snapshot",
			RelationType:   models.AssetRelationTypeResult,
			AssetRole:      models.AssetRoleDetailShot,
			IsPrimary:      false,
			SortOrder:      2,
		},
		&models.EcomListingVersion{
			ID:             "listing-export-adopted",
			OrganizationID: "org-1",
			ProductID:      "product-export-snapshot",
			VersionNo:      3,
			VersionLabel:   "Amazon adopted v3",
			Status:         models.ListingVersionStatusAdopted,
			Title:          "Snapshot Listing",
			Platform:       "amazon",
			Site:           "US",
			Locale:         "en_US",
			CreatedBy:      "user-1",
		},
	}
	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed snapshot record: %v", err)
		}
	}

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := newProductCoreTestService(t, productRepo, assetRepo)
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.POST("/api/v1/ecommerce/products/:product_id/export-tasks", handler.CreateExportTask)
	router.GET("/api/v1/ecommerce/downloads", handler.ListDownloads)

	body := map[string]any{
		"platform": "amazon",
		"site":     "US",
		"locale":   "en_US",
		"format":   "zip",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal export body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/products/product-export-snapshot/export-tasks", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create export task status = %d, want %d body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	var created productEnvelope[models.EcomExportTask]
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create export response: %v", err)
	}
	if created.Data.AssetCount != 2 || created.Data.PrimaryAssetRole != models.AssetRoleHero {
		t.Fatalf("unexpected export snapshot metadata: %+v", created.Data)
	}
	if created.Data.ListingVersionID != "listing-export-adopted" || created.Data.ListingVersionLabel != "Amazon adopted v3" {
		t.Fatalf("unexpected export listing snapshot: %+v", created.Data)
	}
	if created.Data.AssetManifest == "" {
		t.Fatalf("expected asset manifest to be captured")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list downloads status = %d, want %d", listResp.Code, http.StatusOK)
	}

	var downloads productEnvelope[[]DownloadListItem]
	if err := json.Unmarshal(listResp.Body.Bytes(), &downloads); err != nil {
		t.Fatalf("decode downloads payload: %v", err)
	}
	if len(downloads.Data) != 1 {
		t.Fatalf("expected 1 download item, got %d", len(downloads.Data))
	}
	item := downloads.Data[0]
	if item.ID != created.Data.ID || item.TaskID != created.Data.ID {
		t.Fatalf("download projection did not expose created task id: created=%s item=%+v", created.Data.ID, item)
	}
	if item.ContentURL != "/api/v1/ecommerce/downloads/"+created.Data.ID+"/content" {
		t.Fatalf("unexpected download content_url: %+v", item)
	}
	if item.Package.FileName == "" || item.Package.ContentType == "" || !item.Package.ManifestAvailable {
		t.Fatalf("unexpected download package metadata: %+v", item.Package)
	}
	if item.AssetCount != 2 || item.ListingVersionLabel != "Amazon adopted v3" {
		t.Fatalf("unexpected download list snapshot: %+v", item)
	}
	if len(item.Assets) != 2 {
		t.Fatalf("expected 2 manifest assets, got %d", len(item.Assets))
	}
}

func TestCreateExportTaskWithSelectedAssetSubset(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-export-subset",
		OrganizationID: "org-1",
		SKUCode:        "SKU-EXPORT-SUBSET",
		Title:          "Subset Product",
		Status:         models.ProductStatusListingReady,
		AssetStatus:    models.AssetStatusReady,
		ListingStatus:  models.ListingStatusReady,
		ExportStatus:   models.ExportStatusPending,
		CreatedBy:      "user-1",
		UpdatedBy:      "user-1",
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	records := []any{
		&models.EcommerceAsset{
			ID:             "asset-subset-1",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			FileName:       "subset-1.png",
			MimeType:       "image/png",
		},
		&models.EcommerceAsset{
			ID:             "asset-subset-2",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			FileName:       "subset-2.png",
			MimeType:       "image/png",
		},
		&models.EcomAssetRelation{
			ID:             "relation-subset-1",
			OrganizationID: "org-1",
			AssetID:        "asset-subset-1",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-export-subset",
			RelationType:   models.AssetRelationTypePrimary,
			AssetRole:      models.AssetRoleHero,
			IsPrimary:      true,
			SortOrder:      1,
		},
		&models.EcomAssetRelation{
			ID:             "relation-subset-2",
			OrganizationID: "org-1",
			AssetID:        "asset-subset-2",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-export-subset",
			RelationType:   models.AssetRelationTypeResult,
			AssetRole:      models.AssetRoleDetailShot,
			IsPrimary:      false,
			SortOrder:      2,
		},
		&models.EcomListingVersion{
			ID:             "listing-subset-adopted",
			OrganizationID: "org-1",
			ProductID:      "product-export-subset",
			VersionNo:      2,
			VersionLabel:   "Subset adopted v2",
			Status:         models.ListingVersionStatusAdopted,
			Title:          "Subset Listing",
			Platform:       "amazon",
			Site:           "US",
			Locale:         "en_US",
			CreatedBy:      "user-1",
		},
	}
	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed subset record: %v", err)
		}
	}

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := newProductCoreTestService(t, productRepo, assetRepo)
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.POST("/api/v1/ecommerce/products/:product_id/export-tasks", handler.CreateExportTask)

	body := map[string]any{
		"platform": "amazon",
		"site":     "US",
		"locale":   "en_US",
		"format":   "zip",
		"asset_relation_ids": []string{
			"relation-subset-2",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal export subset body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/products/product-export-subset/export-tasks", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create export subset status = %d, want %d body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	var created productEnvelope[models.EcomExportTask]
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create export subset response: %v", err)
	}
	if created.Data.AssetCount != 1 {
		t.Fatalf("expected subset export asset count 1, got %+v", created.Data)
	}
	var manifest []DownloadAssetItem
	if err := json.Unmarshal([]byte(created.Data.AssetManifest), &manifest); err != nil {
		t.Fatalf("decode subset manifest: %v", err)
	}
	if len(manifest) != 1 || manifest[0].RelationID != "relation-subset-2" {
		t.Fatalf("unexpected subset manifest: %+v", manifest)
	}
}

func TestAssetLibraryListStatsAndGovernance(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	records := []any{
		&models.EcomProductSKU{ID: "product-lib-1", OrganizationID: "org-1", SKUCode: "SKU-LIB-1", Title: "Library One", Status: models.ProductStatusAssetsReady, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending},
		&models.EcomProductSKU{ID: "product-lib-2", OrganizationID: "org-1", SKUCode: "SKU-LIB-2", Title: "Library Two", Status: models.ProductStatusAssetsReady, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending},
		&models.EcomProductSKU{ID: "product-other-org", OrganizationID: "org-2", SKUCode: "SKU-OTHER", Title: "Other Org", Status: models.ProductStatusAssetsReady, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending},
		&models.EcommerceAsset{ID: "asset-lib-1", OrganizationID: "org-1", UserID: "user-1", AssetType: "image", SourceType: "generated", StorageKey: "assets/lib-1.png", MimeType: "image/png", FileName: "lib-1.png", Metadata: `{"prompt_id":"prompt-1","job_id":"job-1","template_id":"tmpl-1","storage_key":"assets/lib-1.png"}`},
		&models.EcommerceAsset{ID: "asset-lib-2", OrganizationID: "org-1", UserID: "user-1", AssetType: "image", SourceType: "upload", StorageKey: "assets/lib-2.png", MimeType: "image/png", FileName: "lib-2.png"},
		&models.EcommerceAsset{ID: "asset-orphan", OrganizationID: "org-1", UserID: "user-1", AssetType: "image", SourceType: "generated", StorageKey: "assets/orphan.png", MimeType: "image/png", FileName: "orphan.png"},
		&models.EcommerceAsset{ID: "asset-other-org", OrganizationID: "org-2", UserID: "user-2", AssetType: "image", SourceType: "generated", StorageKey: "assets/other.png", MimeType: "image/png", FileName: "other.png"},
		&models.EcomAssetRelation{ID: "rel-lib-1", OrganizationID: "org-1", AssetID: "asset-lib-1", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: "product-lib-1", RelationType: models.AssetRelationTypeResult, AssetRole: models.AssetRoleHero, IsPrimary: true, SortOrder: 1, Visibility: "library", Metadata: `{"tags":["old"]}`},
		&models.EcomAssetRelation{ID: "rel-lib-2", OrganizationID: "org-1", AssetID: "asset-lib-2", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: "product-lib-2", RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleDetailShot, IsPrimary: false, SortOrder: 2, Visibility: "library"},
		&models.EcomAssetRelation{ID: "rel-other-org", OrganizationID: "org-2", AssetID: "asset-other-org", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: "product-other-org", RelationType: models.AssetRelationTypeResult, AssetRole: models.AssetRoleHero, IsPrimary: true, Visibility: "library"},
	}
	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed asset library record: %v", err)
		}
	}

	service := NewService(repository.NewProductCenterRepository(db), repository.NewImageRuntimeRepository(db), nil)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.GET("/api/v1/ecommerce/assets/library", handler.ListAssetLibrary)
	router.GET("/api/v1/ecommerce/assets/library/stats", handler.AssetLibraryStats)
	router.PATCH("/api/v1/ecommerce/assets/library/batch-governance", handler.BatchUpdateAssetLibraryGovernance)
	router.GET("/api/v1/ecommerce/assets/library/:relationId/lineage", handler.GetAssetLibraryLineage)
	router.PATCH("/api/v1/ecommerce/assets/library/:relationId/governance", handler.UpdateAssetLibraryGovernance)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/assets/library?source_type=generated&asset_role=hero", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list asset library status = %d, want %d", listResp.Code, http.StatusOK)
	}
	var listPayload productEnvelope[AssetLibraryListResponse]
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode asset library list: %v", err)
	}
	if listPayload.Code != 0 || listPayload.Data.Total != 1 || len(listPayload.Data.Items) != 1 {
		t.Fatalf("unexpected asset library list payload: %+v", listPayload)
	}
	item := listPayload.Data.Items[0]
	if item.ProductID != "product-lib-1" || item.SKUCode != "SKU-LIB-1" || item.RelationID != "rel-lib-1" || item.Asset.ID != "asset-lib-1" {
		t.Fatalf("unexpected asset library item: %+v", item)
	}
	if item.Lineage.PromptID != "prompt-1" || item.Lineage.JobID != "job-1" || item.Lineage.TemplateID != "tmpl-1" {
		t.Fatalf("unexpected lineage: %+v", item.Lineage)
	}
	if item.Asset.ContentURL != "/api/v1/ecommerce/assets/asset-lib-1/content" {
		t.Fatalf("asset response must use content proxy, got %+v", item.Asset)
	}
	if strings.Contains(item.Asset.Metadata, "storage_key") || strings.Contains(item.Asset.Metadata, "assets/lib-1.png") {
		t.Fatalf("asset metadata leaked storage key: %s", item.Asset.Metadata)
	}

	lineageReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/assets/library/rel-lib-1/lineage", nil)
	lineageResp := httptest.NewRecorder()
	router.ServeHTTP(lineageResp, lineageReq)
	if lineageResp.Code != http.StatusOK {
		t.Fatalf("lineage status = %d, want %d", lineageResp.Code, http.StatusOK)
	}
	var lineagePayload productEnvelope[AssetLibraryLineageResponse]
	if err := json.Unmarshal(lineageResp.Body.Bytes(), &lineagePayload); err != nil {
		t.Fatalf("decode lineage response: %v", err)
	}
	if lineagePayload.Data.AssetID != "asset-lib-1" || lineagePayload.Data.ProductID != "product-lib-1" || lineagePayload.Data.Lineage.PromptID != "prompt-1" {
		t.Fatalf("unexpected lineage response: %+v", lineagePayload.Data)
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/assets/library/stats?group_by=source_type", nil)
	statsResp := httptest.NewRecorder()
	router.ServeHTTP(statsResp, statsReq)
	if statsResp.Code != http.StatusOK {
		t.Fatalf("stats status = %d, want %d", statsResp.Code, http.StatusOK)
	}
	var statsPayload productEnvelope[AssetLibraryStatsResponse]
	if err := json.Unmarshal(statsResp.Body.Bytes(), &statsPayload); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if len(statsPayload.Data.Groups) != 2 {
		t.Fatalf("expected 2 source_type groups, got %+v", statsPayload.Data.Groups)
	}

	patchBody := []byte(`{"asset_role":"model_shot","is_primary":false,"sort_order":7,"tags":["approved","marketplace"],"visibility":"archived","status":"approved"}`)
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/ecommerce/assets/library/rel-lib-1/governance", bytes.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp := httptest.NewRecorder()
	router.ServeHTTP(patchResp, patchReq)
	if patchResp.Code != http.StatusOK {
		t.Fatalf("governance status = %d, want %d body=%s", patchResp.Code, http.StatusOK, patchResp.Body.String())
	}
	var patchPayload productEnvelope[AssetLibraryItem]
	if err := json.Unmarshal(patchResp.Body.Bytes(), &patchPayload); err != nil {
		t.Fatalf("decode governance response: %v", err)
	}
	if patchPayload.Data.Governance.AssetRole != models.AssetRoleModelShot || patchPayload.Data.Governance.Visibility != "archived" || patchPayload.Data.Governance.Status != "approved" || patchPayload.Data.Governance.SortOrder != 7 {
		t.Fatalf("unexpected governance response: %+v", patchPayload.Data.Governance)
	}
	if len(patchPayload.Data.Governance.Tags) != 2 || patchPayload.Data.Governance.Tags[0] != "approved" {
		t.Fatalf("unexpected governance tags: %+v", patchPayload.Data.Governance.Tags)
	}

	batchBody := []byte(`{"relation_ids":["rel-lib-1","rel-lib-2"],"patch":{"visibility":"shared","tags":["batch"]}}`)
	batchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/ecommerce/assets/library/batch-governance", bytes.NewReader(batchBody))
	batchReq.Header.Set("Content-Type", "application/json")
	batchResp := httptest.NewRecorder()
	router.ServeHTTP(batchResp, batchReq)
	if batchResp.Code != http.StatusOK {
		t.Fatalf("batch governance status = %d, want %d body=%s", batchResp.Code, http.StatusOK, batchResp.Body.String())
	}
	var batchPayload productEnvelope[BatchAssetGovernanceResponse]
	if err := json.Unmarshal(batchResp.Body.Bytes(), &batchPayload); err != nil {
		t.Fatalf("decode batch governance response: %v", err)
	}
	if batchPayload.Data.Success != 2 || batchPayload.Data.Failed != 0 || len(batchPayload.Data.Items) != 2 {
		t.Fatalf("unexpected batch governance response: %+v", batchPayload.Data)
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/assets/library?visibility=shared&tag=batch", nil)
	filterResp := httptest.NewRecorder()
	router.ServeHTTP(filterResp, filterReq)
	if filterResp.Code != http.StatusOK {
		t.Fatalf("filter status = %d, want %d", filterResp.Code, http.StatusOK)
	}
	var filterPayload productEnvelope[AssetLibraryListResponse]
	if err := json.Unmarshal(filterResp.Body.Bytes(), &filterPayload); err != nil {
		t.Fatalf("decode filter response: %v", err)
	}
	if filterPayload.Data.Total != 2 {
		t.Fatalf("expected batch filtered total=2, got %+v", filterPayload.Data)
	}
}

func newProductCoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.EcomProductSKU{},
		&models.EcomAssetRelation{},
		&models.EcomListingVersion{},
		&models.EcomExportTask{},
		&models.EcomExportPackage{},
		&models.EcomProductActivity{},
		&models.EcommerceAsset{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func seedDownloadTestData(t *testing.T, db *gorm.DB) {
	t.Helper()

	records := []any{
		&models.EcomProductSKU{
			ID:             "product-1",
			OrganizationID: "org-1",
			SKUCode:        "SKU-001",
			Title:          "Test Product",
			Status:         models.ProductStatusExportReady,
			AssetStatus:    models.AssetStatusReady,
			ListingStatus:  models.ListingStatusReady,
			ExportStatus:   models.ExportStatusReady,
			CreatedBy:      "user-1",
			UpdatedBy:      "user-1",
		},
		&models.EcommerceAsset{
			ID:             "asset-1",
			OrganizationID: "org-1",
			UserID:         "user-1",
			AssetType:      "image",
			SourceType:     "generated",
			StorageKey:     "ecommerce-assets/source-1.zip",
			MimeType:       "image/png",
			FileName:       "hero.png",
		},
		&models.EcomAssetRelation{
			ID:             "relation-1",
			OrganizationID: "org-1",
			AssetID:        "asset-1",
			OwnerType:      models.AssetRelationOwnerTypeProduct,
			OwnerID:        "product-1",
			RelationType:   models.AssetRelationTypePrimary,
			AssetRole:      models.AssetRoleHero,
			IsPrimary:      true,
			SortOrder:      1,
		},
		&models.EcomExportTask{
			ID:             "export-1",
			OrganizationID: "org-1",
			ProductID:      "product-1",
			Status:         models.ExportTaskStatusSucceeded,
			Platform:       "amazon",
			Site:           "US",
			Locale:         "en_US",
			Format:         "zip",
			StorageKey:     "exports/export-1.zip",
			FileSize:       "2.4MB",
			CreatedBy:      "user-1",
		},
	}

	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
}
