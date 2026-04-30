package productcore

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type productEnvelope[T any] struct {
	Code int `json:"code"`
	Data T   `json:"data"`
}

func TestDownloadCenterListsManifestAndStreamsContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/storage/assets/content" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("mock-export-binary"))
	}))
	defer platformServer.Close()

	db := newProductCoreTestDB(t)
	seedDownloadTestData(t, db)

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := NewService(productRepo, assetRepo, platform.New(config.PlatformConfig{
		BaseURL:               platformServer.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	}))
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.GET("/api/v1/ecommerce/downloads", handler.ListDownloads)
	router.GET("/api/v1/ecommerce/downloads/:download_id/content", handler.DownloadContent)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list downloads status = %d, want %d", listResp.Code, http.StatusOK)
	}

	var payload productEnvelope[[]DownloadListItem]
	if err := json.Unmarshal(listResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list downloads: %v", err)
	}
	if payload.Code != 0 || len(payload.Data) != 1 {
		t.Fatalf("unexpected downloads payload: code=%d len=%d", payload.Code, len(payload.Data))
	}
	item := payload.Data[0]
	if item.ProductID != "product-1" || item.AssetCount != 1 || !item.Downloadable {
		t.Fatalf("unexpected download item: %+v", item)
	}
	if len(item.Assets) != 1 {
		t.Fatalf("expected 1 manifest asset, got %d", len(item.Assets))
	}
	if item.Assets[0].AssetID != "asset-1" || item.Assets[0].ContentURL != "/api/v1/ecommerce/assets/asset-1/content" {
		t.Fatalf("unexpected manifest asset: %+v", item.Assets[0])
	}
	var storedTask models.EcomExportTask
	if err := db.Where("id = ?", "export-1").First(&storedTask).Error; err != nil {
		t.Fatalf("query stored export task: %v", err)
	}
	if storedTask.AssetCount != 1 || storedTask.AssetManifest == "" || storedTask.PrimaryAssetRole != models.AssetRoleHero {
		t.Fatalf("expected legacy export task to be backfilled with snapshot, got %+v", storedTask)
	}

	contentReq := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads/export-1/content", nil)
	contentResp := httptest.NewRecorder()
	router.ServeHTTP(contentResp, contentReq)
	if contentResp.Code != http.StatusOK {
		t.Fatalf("download content status = %d, want %d", contentResp.Code, http.StatusOK)
	}
	if contentResp.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("download content type = %s", contentResp.Header().Get("Content-Type"))
	}
	if got := contentResp.Header().Get("Content-Disposition"); got == "" {
		t.Fatal("expected content disposition header")
	}
	if contentResp.Body.String() != "mock-export-binary" {
		t.Fatalf("download body = %q", contentResp.Body.String())
	}
}

func TestDownloadContentRedirectsToPackageURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-redirect",
		OrganizationID: "org-1",
		SKUCode:        "SKU-REDIRECT",
		Title:          "Redirect Product",
		Status:         models.ProductStatusExportReady,
		AssetStatus:    models.AssetStatusReady,
		ListingStatus:  models.ListingStatusReady,
		ExportStatus:   models.ExportStatusReady,
		CreatedBy:      "user-1",
		UpdatedBy:      "user-1",
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := db.Create(&models.EcomExportTask{
		ID:             "export-redirect",
		OrganizationID: "org-1",
		ProductID:      "product-redirect",
		Status:         models.ExportTaskStatusSucceeded,
		Platform:       "amazon",
		Site:           "US",
		Locale:         "en_US",
		Format:         "zip",
		PackageURL:     "https://example.com/export-redirect.zip",
		CreatedBy:      "user-1",
	}).Error; err != nil {
		t.Fatalf("create export task: %v", err)
	}

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := NewService(productRepo, assetRepo, nil)
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.GET("/api/v1/ecommerce/downloads/:download_id/content", handler.DownloadContent)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads/export-redirect/content", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusTemporaryRedirect {
		t.Fatalf("redirect status = %d, want %d", resp.Code, http.StatusTemporaryRedirect)
	}
	if location := resp.Header().Get("Location"); location != "https://example.com/export-redirect.zip" {
		t.Fatalf("redirect location = %s", location)
	}
}

func TestBatchListingCreateAndAdopt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	seedBatchListingTestData(t, db)

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := NewService(productRepo, assetRepo, nil)
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.POST("/api/v1/ecommerce/products/listing-versions/batch", handler.BatchCreateListingVersions)
	router.POST("/api/v1/ecommerce/products/listing-versions/batch-adopt", handler.BatchAdoptListingVersions)

	createBody := map[string]any{
		"items": []map[string]any{
			{
				"product_id":    "product-batch-1",
				"version_label": "Batch v1",
				"title":         "Batch Listing One",
				"description":   "First batch listing",
				"platform":      "amazon",
				"site":          "US",
				"locale":        "en_US",
			},
			{
				"product_id":    "missing-product",
				"version_label": "Batch v1",
				"title":         "Broken Listing",
				"platform":      "amazon",
				"site":          "US",
				"locale":        "en_US",
			},
		},
	}
	createPayload, err := json.Marshal(createBody)
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/products/listing-versions/batch", bytes.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("batch create status = %d, want %d", createResp.Code, http.StatusOK)
	}

	var createResult productEnvelope[BatchListingMutationResult]
	if decodeErr := json.Unmarshal(createResp.Body.Bytes(), &createResult); decodeErr != nil {
		t.Fatalf("decode batch create response: %v", decodeErr)
	}
	if createResult.Data.Total != 2 || createResult.Data.Succeeded != 1 || createResult.Data.Failed != 1 {
		t.Fatalf("unexpected create summary: %+v", createResult.Data)
	}
	if !createResult.Data.Items[0].Success || createResult.Data.Items[0].Listing == nil {
		t.Fatalf("expected first batch create item to succeed: %+v", createResult.Data.Items[0])
	}
	if createResult.Data.Items[1].Success {
		t.Fatalf("expected second batch create item to fail: %+v", createResult.Data.Items[1])
	}

	var product models.EcomProductSKU
	if queryErr := db.Where("id = ?", "product-batch-1").First(&product).Error; queryErr != nil {
		t.Fatalf("query product-batch-1: %v", queryErr)
	}
	if product.ListingStatus != models.ListingStatusPartial {
		t.Fatalf("listing status after create = %s, want %s", product.ListingStatus, models.ListingStatusPartial)
	}

	adoptBody := map[string]any{
		"items": []map[string]any{
			{
				"product_id": "product-batch-1",
				"version_id": createResult.Data.Items[0].VersionID,
			},
			{
				"product_id": "product-batch-2",
				"version_id": "missing-version",
			},
		},
	}
	adoptPayload, err := json.Marshal(adoptBody)
	if err != nil {
		t.Fatalf("marshal adopt body: %v", err)
	}

	adoptReq := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/products/listing-versions/batch-adopt", bytes.NewReader(adoptPayload))
	adoptReq.Header.Set("Content-Type", "application/json")
	adoptResp := httptest.NewRecorder()
	router.ServeHTTP(adoptResp, adoptReq)
	if adoptResp.Code != http.StatusOK {
		t.Fatalf("batch adopt status = %d, want %d", adoptResp.Code, http.StatusOK)
	}

	var adoptResult productEnvelope[BatchListingMutationResult]
	if err := json.Unmarshal(adoptResp.Body.Bytes(), &adoptResult); err != nil {
		t.Fatalf("decode batch adopt response: %v", err)
	}
	if adoptResult.Data.Total != 2 || adoptResult.Data.Succeeded != 1 || adoptResult.Data.Failed != 1 {
		t.Fatalf("unexpected adopt summary: %+v", adoptResult.Data)
	}
	if !adoptResult.Data.Items[0].Success {
		t.Fatalf("expected first adopt item to succeed: %+v", adoptResult.Data.Items[0])
	}
	if adoptResult.Data.Items[1].Success {
		t.Fatalf("expected second adopt item to fail: %+v", adoptResult.Data.Items[1])
	}

	if err := db.Where("id = ?", "product-batch-1").First(&product).Error; err != nil {
		t.Fatalf("query adopted product: %v", err)
	}
	if product.ListingStatus != models.ListingStatusReady {
		t.Fatalf("listing status after adopt = %s, want %s", product.ListingStatus, models.ListingStatusReady)
	}
}

func TestUpdateListingVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	seedBatchListingTestData(t, db)
	if err := db.Create(&models.EcomListingVersion{
		ID:             "listing-edit-1",
		OrganizationID: "org-1",
		ProductID:      "product-batch-1",
		VersionNo:      1,
		VersionLabel:   "Original v1",
		Status:         models.ListingVersionStatusDraft,
		Title:          "Original Title",
		Description:    "Original Description",
		Platform:       "amazon",
		Site:           "US",
		Locale:         "en_US",
		CreatedBy:      "user-1",
	}).Error; err != nil {
		t.Fatalf("create listing version: %v", err)
	}

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := NewService(productRepo, assetRepo, nil)
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.PATCH("/api/v1/ecommerce/products/:product_id/listing-versions/:version_id", handler.UpdateListingVersion)

	updateBody := map[string]any{
		"version_label": "Edited v2",
		"title":         "Edited Title",
		"description":   "Edited Description",
		"platform":      "amazon",
		"site":          "UK",
		"locale":        "en_GB",
	}
	payload, err := json.Marshal(updateBody)
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ecommerce/products/product-batch-1/listing-versions/listing-edit-1", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("update listing status = %d, want %d", resp.Code, http.StatusOK)
	}

	var result productEnvelope[models.EcomListingVersion]
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode update listing response: %v", err)
	}
	if result.Data.VersionLabel != "Edited v2" || result.Data.Title != "Edited Title" {
		t.Fatalf("unexpected update payload: %+v", result.Data)
	}

	var stored models.EcomListingVersion
	if err := db.Where("id = ?", "listing-edit-1").First(&stored).Error; err != nil {
		t.Fatalf("query stored listing: %v", err)
	}
	if stored.VersionLabel != "Edited v2" || stored.Site != "UK" || stored.Locale != "en_GB" {
		t.Fatalf("listing version not updated in db: %+v", stored)
	}
}

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
	service := NewService(productRepo, assetRepo, nil)
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
	service := NewService(productRepo, assetRepo, nil)
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
	}
	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed subset record: %v", err)
		}
	}

	productRepo := repository.NewProductCenterRepository(db)
	assetRepo := repository.NewImageRuntimeRepository(db)
	service := NewService(productRepo, assetRepo, nil)
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

func seedBatchListingTestData(t *testing.T, db *gorm.DB) {
	t.Helper()

	records := []any{
		&models.EcomProductSKU{
			ID:             "product-batch-1",
			OrganizationID: "org-1",
			SKUCode:        "SKU-BATCH-1",
			Title:          "Batch Product One",
			Status:         models.ProductStatusDraft,
			AssetStatus:    models.AssetStatusReady,
			ListingStatus:  models.ListingStatusMissing,
			ExportStatus:   models.ExportStatusPending,
			CreatedBy:      "user-1",
			UpdatedBy:      "user-1",
		},
		&models.EcomProductSKU{
			ID:             "product-batch-2",
			OrganizationID: "org-1",
			SKUCode:        "SKU-BATCH-2",
			Title:          "Batch Product Two",
			Status:         models.ProductStatusDraft,
			AssetStatus:    models.AssetStatusReady,
			ListingStatus:  models.ListingStatusMissing,
			ExportStatus:   models.ExportStatusPending,
			CreatedBy:      "user-1",
			UpdatedBy:      "user-1",
		},
	}

	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed batch listing record: %v", err)
		}
	}
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
