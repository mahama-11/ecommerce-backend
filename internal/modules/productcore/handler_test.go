package productcore

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
