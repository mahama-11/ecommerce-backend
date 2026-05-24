package productcore

import (
	"archive/zip"
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
)

type productEnvelope[T any] struct {
	Code int `json:"code"`
	Data T   `json:"data"`
}

func newProductCoreBillingPlatform(t *testing.T) (*platform.Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/charge-sessions":
			_, _ = w.Write([]byte(`{"code":0,"data":{"id":"cs-test","status":"created"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/internal/v1/runtime/charge-sessions/cs-test":
			_, _ = w.Write([]byte(`{"code":0,"data":{"id":"cs-test","status":"updated"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations":
			_, _ = w.Write([]byte(`{"code":0,"data":{"id":"res-test","status":"reserved","units":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations/res-test/release":
			_, _ = w.Write([]byte(`{"code":0,"data":{"id":"res-test","status":"released","units":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/metering/finalizations":
			_, _ = w.Write([]byte(`{"code":0,"data":{"settlement":{"id":"set-test","event_id":"evt-test","status":"posted"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/storage/assets/content":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("mock-export-binary"))
		default:
			http.NotFound(w, r)
		}
	}))
	client := platform.New(config.PlatformConfig{
		BaseURL:               server.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	})
	return client, server.Close
}

func newProductCoreTestService(t *testing.T, productRepo *repository.ProductCenterRepository, assetRepo *repository.ImageRuntimeRepository) *Service {
	t.Helper()
	client, cleanup := newProductCoreBillingPlatform(t)
	t.Cleanup(cleanup)
	return NewService(productRepo, assetRepo, client)
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

func TestCreateExportPackagePartialSuccessListsAndDownloadsBundle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	records := []any{
		&models.EcomProductSKU{ID: "product-package-ready", OrganizationID: "org-1", SKUCode: "SKU-PKG-READY", Title: "Package Ready", Status: models.ProductStatusListingReady, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusReady, ExportStatus: models.ExportStatusPending, CreatedBy: "user-1", UpdatedBy: "user-1"},
		&models.EcommerceAsset{ID: "asset-package-ready", OrganizationID: "org-1", UserID: "user-1", AssetType: "image", SourceType: "generated", StorageKey: "ecommerce-assets/pkg-ready.png", MimeType: "image/png", FileName: "pkg-ready.png"},
		&models.EcomAssetRelation{ID: "relation-package-ready", OrganizationID: "org-1", AssetID: "asset-package-ready", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: "product-package-ready", RelationType: models.AssetRelationTypePrimary, AssetRole: models.AssetRoleHero, IsPrimary: true},
		&models.EcomListingVersion{ID: "listing-package-ready", OrganizationID: "org-1", ProductID: "product-package-ready", VersionNo: 1, VersionLabel: "Ready v1", Status: models.ListingVersionStatusAdopted, Platform: "amazon", Site: "US", Locale: "en_US", Title: "Package Ready Listing", CreatedBy: "user-1"},
		&models.EcomProductSKU{ID: "product-package-blocked", OrganizationID: "org-1", SKUCode: "SKU-PKG-BLOCKED", Title: "Package Blocked", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusMissing, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending, CreatedBy: "user-1", UpdatedBy: "user-1"},
	}
	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed package record: %v", err)
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
	router.POST("/api/v1/ecommerce/export-packages", handler.CreateExportPackage)
	router.GET("/api/v1/ecommerce/downloads", handler.ListDownloads)
	router.GET("/api/v1/ecommerce/downloads/:download_id/content", handler.DownloadContent)

	body := []byte(`{"platform":"amazon","site":"US","locale":"en_US","format":"csv","items":[{"product_id":"product-package-ready"},{"product_id":"product-package-blocked"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/export-packages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create package status=%d body=%s", resp.Code, resp.Body.String())
	}
	var created productEnvelope[ExportPackageResponse]
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode package response: %v", err)
	}
	if created.Data.Status != models.ExportPackageStatusPartialSucceeded || created.Data.Succeeded != 1 || created.Data.Failed != 1 {
		t.Fatalf("unexpected package response: %+v", created.Data)
	}
	if created.Data.Manifest.Schema != "amazon/us/csv/v1" || len(created.Data.Manifest.Products) != 1 || len(created.Data.Manifest.Blockers) != 1 {
		t.Fatalf("unexpected package manifest: %+v", created.Data.Manifest)
	}

	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads", nil))
	if listResp.Code != http.StatusOK {
		t.Fatalf("list downloads status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var downloads productEnvelope[[]DownloadListItem]
	if err := json.Unmarshal(listResp.Body.Bytes(), &downloads); err != nil {
		t.Fatalf("decode downloads: %v", err)
	}
	var foundPackage, foundChild bool
	for _, item := range downloads.Data {
		if item.ID == created.Data.PackageID && item.SourceType == "export_package" && item.SKUCount == 2 && item.Downloadable {
			foundPackage = true
		}
		if item.TaskID == created.Data.Items[0].TaskID && item.PackageID == created.Data.PackageID && item.SourceType == "product_export" {
			foundChild = true
		}
	}
	if !foundPackage || !foundChild {
		t.Fatalf("downloads missing package or child row: %+v", downloads.Data)
	}

	manifestResp := httptest.NewRecorder()
	router.ServeHTTP(manifestResp, httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads/"+created.Data.PackageID+"/content?file=manifest", nil))
	if manifestResp.Code != http.StatusOK || manifestResp.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("manifest status=%d content-type=%s body=%s", manifestResp.Code, manifestResp.Header().Get("Content-Type"), manifestResp.Body.String())
	}
	var manifest ExportPackageManifest
	if err := json.Unmarshal(manifestResp.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest content: %v", err)
	}
	if manifest.PackageID != created.Data.PackageID || manifest.Schema != "amazon/us/csv/v1" || manifest.Succeeded != 1 {
		t.Fatalf("unexpected manifest content: %+v", manifest)
	}

	bundleResp := httptest.NewRecorder()
	router.ServeHTTP(bundleResp, httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/downloads/"+created.Data.PackageID+"/content", nil))
	if bundleResp.Code != http.StatusOK || bundleResp.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("bundle status=%d content-type=%s", bundleResp.Code, bundleResp.Header().Get("Content-Type"))
	}
	zipReader, err := zip.NewReader(bytes.NewReader(bundleResp.Body.Bytes()), int64(bundleResp.Body.Len()))
	if err != nil {
		t.Fatalf("read bundle zip: %v", err)
	}
	files := map[string]bool{}
	for _, file := range zipReader.File {
		files[file.Name] = true
	}
	if !files["manifest.json"] || !files["listing.csv"] {
		t.Fatalf("bundle missing expected files: %+v", files)
	}
}

func TestDownloadContentDoesNotRedirectToPackageURL(t *testing.T) {
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
	service := newProductCoreTestService(t, productRepo, assetRepo)
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
	if resp.Code == http.StatusTemporaryRedirect {
		t.Fatalf("unexpected redirect to package_url: %s", resp.Header().Get("Location"))
	}
	if resp.Header().Get("Location") != "" {
		t.Fatalf("download content must not expose redirect location: %s", resp.Header().Get("Location"))
	}
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusInternalServerError)
	}
}

func TestBatchListingCreateAndAdopt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	seedBatchListingTestData(t, db)

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

func TestBatchListingPreviewAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newProductCoreTestDB(t)
	seedBatchListingTestData(t, db)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-not-ready",
		OrganizationID: "org-1",
		SKUCode:        "SKU-NOT-READY",
		Title:          "Not Ready",
		Status:         models.ProductStatusDraft,
		AssetStatus:    models.AssetStatusPartial,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
	}).Error; err != nil {
		t.Fatalf("seed not ready product: %v", err)
	}

	service := NewService(repository.NewProductCenterRepository(db), repository.NewImageRuntimeRepository(db), nil)
	result, err := service.BatchCreateListingVersions("org-1", "user-1", BatchCreateListingVersionsInput{
		Preview: true,
		Items: []BatchCreateListingVersionItemInput{
			{SKUCode: "SKU-BATCH-1", VersionLabel: "Preview v1", Title: "Valid Preview Listing", BulletPoints: []string{"Soft cotton", "Machine washable"}, Platform: "amazon", Site: "US", Locale: "en_US"},
			{SKUCode: "SKU-BATCH-2", VersionLabel: "Bad v1", Title: "fake replica product", Platform: "amazon", Site: "US", Locale: "en_US"},
			{SKUCode: "SKU-NOT-READY", VersionLabel: "No Assets", Title: "Needs assets", Platform: "amazon", Site: "US", Locale: "en_US"},
		},
	})
	if err != nil {
		t.Fatalf("batch preview: %v", err)
	}
	if !result.Preview || result.Total != 3 || result.Succeeded != 1 || result.Failed != 2 {
		t.Fatalf("unexpected preview result: %+v", result)
	}
	if !result.Items[0].Success || result.Items[0].Listing == nil || result.Items[0].Listing.ID == "" {
		t.Fatalf("expected first preview item success: %+v", result.Items[0])
	}
	if result.Items[1].Success || result.Items[2].Success {
		t.Fatalf("expected sensitive/not-ready items to fail: %+v", result.Items)
	}
	var count int64
	if err := db.Model(&models.EcomListingVersion{}).Count(&count).Error; err != nil {
		t.Fatalf("count listings after preview: %v", err)
	}
	if count != 0 {
		t.Fatalf("preview should not persist listing versions, got %d", count)
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
	service := newProductCoreTestService(t, productRepo, assetRepo)
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
	if result.Data.ID == "listing-edit-1" || result.Data.VersionNo != 2 || result.Data.VersionLabel != "Edited v2" || result.Data.Title != "Edited Title" {
		t.Fatalf("unexpected immutable edit payload: %+v", result.Data)
	}

	var stored models.EcomListingVersion
	if err := db.Where("id = ?", "listing-edit-1").First(&stored).Error; err != nil {
		t.Fatalf("query stored listing: %v", err)
	}
	if stored.VersionLabel != "Original v1" || stored.Site != "US" || stored.Locale != "en_US" {
		t.Fatalf("historical listing version was mutated: %+v", stored)
	}
	var count int64
	if err := db.Model(&models.EcomListingVersion{}).Where("product_id = ?", "product-batch-1").Count(&count).Error; err != nil {
		t.Fatalf("count listing versions: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected immutable edit to create second version, got %d", count)
	}
}
