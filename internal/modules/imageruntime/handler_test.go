package imageruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestImageRuntimeInternalCallbacksAndAssetDownload(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/storage/assets/content" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("mock-image"))
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	service := NewService(repo, commercialRepo, nil, productRepo, nil, platform.New(config.PlatformConfig{
		BaseURL:               platformServer.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	}), testImageRuntimeAppConfig())
	handler := NewHandler(service)

	job := &models.EcommerceImageJob{
		ID:             "job-1",
		OrganizationID: "org-1",
		UserID:         "user-1",
		SceneType:      "product_main_image",
		InputMode:      "image_to_image",
		Status:         "queued",
		Stage:          "queued",
		StageMessage:   "queued",
	}
	if err := repo.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}

	router := gin.New()
	internal := router.Group("/internal/v1/ecommerce")
	internal.Use(middleware.RequireInternalService("ecommerce-service-secret"))
	internal.POST("/jobs/:jobID/runtime", handler.InternalUpdateJobRuntime)
	internal.POST("/jobs/:jobID/results", handler.InternalRecordJobResults)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/job-1/runtime", bytes.NewBufferString(`{"status":"processing","stage":"provider_running","stage_message":"running","progress":25,"provider_job_id":"task-1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("runtime callback without secret status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/job-1/runtime", bytes.NewBufferString(`{"status":"processing","stage":"provider_running","stage_message":"running","progress":25,"provider_job_id":"task-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service-Secret", "ecommerce-service-secret")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("runtime callback status = %d, want %d", resp.Code, http.StatusOK)
	}

	updated, err := repo.FindJobByID("job-1")
	if err != nil {
		t.Fatalf("FindJobByID() error = %v", err)
	}
	if updated.Status != "processing" || updated.ProviderJobID != "task-1" || updated.Progress != 25 {
		t.Fatalf("runtime callback did not update job: %+v", updated)
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/job-1/results", bytes.NewBufferString(`{"status":"completed","progress":100,"stage_message":"done","variants":[{"index":0,"status":"ready","is_selected":true,"asset":{"asset_type":"generated","source_type":"generated","storage_key":"ecommerce-assets/generated-1.png","mime_type":"image/png","width":1024,"height":1024}}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service-Secret", "ecommerce-service-secret")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("results callback status = %d, want %d", resp.Code, http.StatusOK)
	}

	completed, err := repo.FindJobByID("job-1")
	if err != nil {
		t.Fatalf("FindJobByID() after results error = %v", err)
	}
	if completed.Status != "completed" || completed.SelectedResultAssetID == "" {
		t.Fatalf("results callback did not complete job: %+v", completed)
	}

	asset, body, headers, err := service.GetAssetContent("org-1", completed.SelectedResultAssetID)
	if err != nil {
		t.Fatalf("GetAssetContent() error = %v", err)
	}
	defer body.Close()
	payload, _ := io.ReadAll(body)
	if asset.StorageKey != "ecommerce-assets/generated-1.png" {
		t.Fatalf("asset storage_key = %s, want ecommerce-assets/generated-1.png", asset.StorageKey)
	}
	if headers.Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %s, want image/png", headers.Get("Content-Type"))
	}
	if string(payload) != "mock-image" {
		t.Fatalf("asset content = %s, want mock-image", string(payload))
	}
}

func TestRegisterSourceAssetAndCreateImageJob(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	var runtimeCreate platform.CreateRuntimeJobInput
	var chargeCreate platform.CreateChargeSessionInput
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/storage/assets":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"storage_key":"ecommerce-assets/source-1.png","mime_type":"image/png","file_size":128}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/charge-sessions":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &chargeCreate)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-session-1","product_code":"ecommerce","business_subject_type":"ecommerce_image_job","business_subject_id":"job-1","billing_subject_type":"organization","billing_subject_id":"org-1","usage_subject_type":"user","usage_subject_id":"user-1","settlement_subject_type":"organization","settlement_subject_id":"org-1","scene_code":"single","input_mode":"image_to_image","idempotency_key":"abc","status":"created","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"reservation-1","resource_type":"ecommerce.image.generate","billing_subject_type":"organization","billing_subject_id":"org-1","billable_item_code":"ecommerce.image.generate","reservation_key":"reserve:job-1","units":1,"status":"reserved","reference_id":"charge-session-1","metadata":"{}"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/internal/v1/runtime/charge-sessions/charge-session-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-session-1","product_code":"ecommerce","business_subject_type":"ecommerce_image_job","business_subject_id":"job-1","billing_subject_type":"organization","billing_subject_id":"org-1","usage_subject_type":"user","usage_subject_id":"user-1","settlement_subject_type":"organization","settlement_subject_id":"org-1","scene_code":"single","input_mode":"image_to_image","idempotency_key":"abc","status":"reserved","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/jobs":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &runtimeCreate)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"runtime-job-1","product_code":"ecommerce","task_type":"image_generation","provider_code":"volcengine","provider_mode":"async","organization_id":"org-1","user_id":"user-1","source_type":"ecommerce_image_job","source_id":"job-1","status":"queued","stage":"queued","stage_message":"Runtime job queued","provider_job_id":"","input_manifest":"{}","route_snapshot":"{}","metadata":"{}"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-1",
		OrganizationID: "org-1",
		SKUCode:        "SKU-001",
		Title:          "Test Product",
		Status:         models.ProductStatusDraft,
		AssetStatus:    models.AssetStatusMissing,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	service := NewService(repo, commercialRepo, nil, productRepo, nil, platform.New(config.PlatformConfig{
		BaseURL:               platformServer.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	}), testImageRuntimeAppConfig())
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.POST("/api/v1/ecommerce/assets/source", handler.RegisterSourceAsset)
	router.GET("/api/v1/ecommerce/image-jobs", handler.ListJobs)
	router.POST("/api/v1/ecommerce/image-jobs", handler.CreateImageJob)
	router.GET("/api/v1/ecommerce/image-jobs/:jobID", handler.GetJob)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/assets/source", bytes.NewBufferString(`{"product_id":"product-1","sku_code":"SKU-001","file_name":"source.png","mime_type":"image/png","payload":"data:image/png;base64,Zm9v","width":1024,"height":1024}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("register source asset status = %d, want %d", resp.Code, http.StatusCreated)
	}
	var sourceAsset models.EcommerceAsset
	if err := db.Where("organization_id = ? AND asset_type = ?", "org-1", "source").First(&sourceAsset).Error; err != nil {
		t.Fatalf("query source asset: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/image-jobs", bytes.NewBufferString(`{"product_id":"product-1","sku_code":"SKU-001","scene_type":"ai_posture","source_asset_id":"`+sourceAsset.ID+`","prompt":"生成多组站姿和行走姿势，适合女装详情页","negative_prompt":"blur","objective":"quality","requested_variants":1,"width":1024,"height":1024}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create image job status = %d, want %d", resp.Code, http.StatusCreated)
	}
	var job models.EcommerceImageJob
	if err := db.Where("organization_id = ? AND scene_type = ?", "org-1", "ai_posture").First(&job).Error; err != nil {
		t.Fatalf("query image job: %v", err)
	}
	if job.RuntimeJobID != "runtime-job-1" || job.SourceAssetID != sourceAsset.ID {
		t.Fatalf("unexpected image job data: %+v", job)
	}
	if runtimeCreate.InputManifest == "" {
		t.Fatalf("runtime create request input_manifest is empty")
	}

	var manifest struct {
		ParamsSnapshot map[string]any `json:"params_snapshot"`
	}
	if err := json.Unmarshal([]byte(runtimeCreate.InputManifest), &manifest); err != nil {
		t.Fatalf("unmarshal runtime input manifest: %v", err)
	}
	finalPrompt, _ := manifest.ParamsSnapshot["prompt"].(string)
	finalNegative, _ := manifest.ParamsSnapshot["negative_prompt"].(string)
	if finalPrompt == "生成多组站姿和行走姿势，适合女装详情页" {
		t.Fatalf("expected backend to expand prompt, got raw prompt only")
	}
	if !strings.Contains(finalPrompt, "[SYSTEM INSTRUCTION]") || !strings.Contains(finalPrompt, "不得改变模特身份") || !strings.Contains(finalPrompt, "生成多组站姿和行走姿势，适合女装详情页") {
		t.Fatalf("unexpected final prompt: %s", finalPrompt)
	}
	if strings.Contains(finalPrompt, "[TEMPLATE STYLE]") {
		t.Fatalf("did not expect template style section without template selection: %s", finalPrompt)
	}
	if !strings.Contains(finalNegative, "watermark") || !strings.Contains(finalNegative, "blur") {
		t.Fatalf("unexpected final negative prompt: %s", finalNegative)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/image-jobs/"+job.ID, nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("get image job status = %d, want %d", resp.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/ecommerce/image-jobs?sceneType=ai_posture&limit=5", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list image jobs status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestCreateImageJobFailsWhenReservationFails(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/storage/assets":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"storage_key":"ecommerce-assets/source-1.png","mime_type":"image/png","file_size":128}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/charge-sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-session-1","product_code":"ecommerce","organization_id":"org-1","billing_subject_type":"organization","billing_subject_id":"org-1","billable_item_code":"ecommerce.image.generate","resource_type":"ecommerce.image.generate","reservation_key":"reserve:job-1","estimated_units":1,"status":"created","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":4000,"message":"reservation failed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-1",
		OrganizationID: "org-1",
		SKUCode:        "SKU-001",
		Title:          "Test Product",
		Status:         models.ProductStatusDraft,
		AssetStatus:    models.AssetStatusMissing,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	service := NewService(repo, commercialRepo, nil, productRepo, nil, platform.New(config.PlatformConfig{
		BaseURL:               platformServer.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	}), testImageRuntimeAppConfig())
	handler := NewHandler(service)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Set("orgID", "org-1")
		c.Next()
	})
	router.POST("/api/v1/ecommerce/assets/source", handler.RegisterSourceAsset)
	router.POST("/api/v1/ecommerce/image-jobs", handler.CreateImageJob)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/assets/source", bytes.NewBufferString(`{"product_id":"product-1","sku_code":"SKU-001","file_name":"source.png","mime_type":"image/png","payload":"data:image/png;base64,Zm9v","width":1024,"height":1024}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("register source asset status = %d, want %d", resp.Code, http.StatusCreated)
	}
	var sourceAsset models.EcommerceAsset
	if err := db.Where("organization_id = ? AND asset_type = ?", "org-1", "source").First(&sourceAsset).Error; err != nil {
		t.Fatalf("query source asset: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/image-jobs", bytes.NewBufferString(`{"product_id":"product-1","sku_code":"SKU-001","scene_type":"ai_posture","source_asset_id":"`+sourceAsset.ID+`","prompt":"test","requested_variants":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code == http.StatusCreated {
		t.Fatalf("expected create image job to fail when reservation fails")
	}
	var count int64
	if err := db.Model(&models.EcommerceImageJob{}).Count(&count).Error; err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no image jobs created when reservation fails, got %d", count)
	}
}

func TestRecordJobResultsPersistsWhenMeteringFails(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/metering/finalizations":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":4000,"message":"invalid finalize request"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/internal/v1/runtime/charge-sessions/charge-session-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-session-1","product_code":"ecommerce","organization_id":"org-1","billing_subject_type":"organization","billing_subject_id":"org-1","billable_item_code":"ecommerce.image.generate","resource_type":"ecommerce.image.generate","reservation_key":"reserve:job-1","estimated_units":1,"status":"execution_completed","metadata":"{}"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-1",
		OrganizationID: "org-1",
		SKUCode:        "SKU-001",
		Title:          "Test Product",
		Status:         models.ProductStatusDraft,
		AssetStatus:    models.AssetStatusMissing,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	service := NewService(repo, commercialRepo, nil, productRepo, nil, platform.New(config.PlatformConfig{
		BaseURL:               platformServer.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	}), testImageRuntimeAppConfig())

	job := &models.EcommerceImageJob{
		ID:             "job-1",
		OrganizationID: "org-1",
		UserID:         "user-1",
		SceneType:      "ai_posture",
		InputMode:      "image_to_image",
		Status:         "processing",
		Stage:          "provider_running",
		Metadata:       `{"charge_session_id":"charge-session-1","reservation_id":"","billable_item_code":"ecommerce.image.generate","usage_units":1,"product_id":"product-1","sku_code":"SKU-001"}`,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}

	input := RecordJobResultsInput{
		Status:       "completed",
		Progress:     100,
		StageMessage: "Image generation completed",
		Variants: []RecordResultVariantInput{{
			Index:      0,
			Status:     "completed",
			IsSelected: true,
			Asset: RecordResultAssetInput{
				AssetType:  "generated",
				SourceType: "generated",
				StorageKey: "result-assets/job-1/0.png",
				MimeType:   "image/png",
				FileName:   "0.png",
				Width:      1024,
				Height:     1024,
			},
		}},
	}
	item, err := service.RecordJobResults(job.ID, input)
	if err != nil {
		t.Fatalf("RecordJobResults: %v", err)
	}
	if item.Status != "completed" {
		t.Fatalf("expected completed status, got %s", item.Status)
	}
	if !strings.Contains(item.Metadata, `"metering_status":"failed"`) {
		t.Fatalf("expected metering failure metadata, got %s", item.Metadata)
	}
	var count int64
	if err := db.Model(&models.EcommerceAsset{}).Where("organization_id = ?", "org-1").Count(&count).Error; err != nil {
		t.Fatalf("count assets: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 generated asset, got %d", count)
	}
	item, err = service.RecordJobResults(job.ID, input)
	if err != nil {
		t.Fatalf("RecordJobResults retry: %v", err)
	}
	if err := db.Model(&models.EcommerceAsset{}).Where("organization_id = ?", "org-1").Count(&count).Error; err != nil {
		t.Fatalf("count assets after retry: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected generated assets to stay idempotent, got %d", count)
	}
	input.Variants[0].Asset.StorageKey = "result-assets/job-1/0-replayed-with-new-storage-key.png"
	item, err = service.RecordJobResults(job.ID, input)
	if err != nil {
		t.Fatalf("RecordJobResults retry with changed storage key: %v", err)
	}
	if err := db.Model(&models.EcommerceAsset{}).Where("organization_id = ?", "org-1").Count(&count).Error; err != nil {
		t.Fatalf("count assets after changed storage key retry: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected job/index result asset to stay idempotent when provider replays changed storage key, got %d", count)
	}
	var asset models.EcommerceAsset
	if err := db.Where("organization_id = ?", "org-1").First(&asset).Error; err != nil {
		t.Fatalf("load generated asset: %v", err)
	}
	if !strings.Contains(asset.Metadata, `"generation_result_key":"job-1:0"`) || !strings.Contains(asset.Metadata, `"generation_task_id":"job-1"`) || !strings.Contains(asset.Metadata, `"result_index":0`) {
		t.Fatalf("expected generated asset lineage metadata, got %s", asset.Metadata)
	}
	var relationCount int64
	if err := db.Model(&models.EcomAssetRelation{}).Where("organization_id = ? AND owner_id = ?", "org-1", "product-1").Count(&relationCount).Error; err != nil {
		t.Fatalf("count asset relations: %v", err)
	}
	if relationCount != 1 {
		t.Fatalf("expected generated result to archive into product assets, got %d", relationCount)
	}
}

func newImageRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.EcommerceImageJob{}, &models.EcommerceAsset{}, &models.EcommercePromptRun{}, &models.EcomProductSKU{}, &models.EcomAssetRelation{}, &models.EcomProductActivity{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func testImageRuntimeAppConfig() config.AppConfig {
	return config.AppConfig{
		ProductCode: "ecommerce",
		ImageRuntime: config.ImageRuntimeConfig{
			GlobalNegativePrompt: "blurry, watermark",
			ScenePromptPolicies: map[string]config.ScenePromptPolicyConfig{
				"ai_posture": {
					ToolSlug:              "ai-posture",
					DisplayName:           "姿势裂变",
					SystemPrompt:          "你是一个专业的AI电商模特姿势裂变系统。任务目标：基于单张模特图生成多张不同姿势或镜头角度的结果图，不得改变模特身份、服装款式、颜色、印花、背景、光线与商业拍摄质感。",
					DefaultNegativePrompt: "duplicated poses, background changes",
				},
			},
		},
	}
}

func TestCreateImageJobWithPromptIDPersistsContractMetadata(t *testing.T) {
	t.Helper()

	var runtimeCreate platform.CreateRuntimeJobInput
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/charge-sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-prompt","product_code":"ecommerce","status":"created","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"reservation-prompt","resource_type":"quota","billing_subject_type":"organization","billing_subject_id":"org-1","billable_item_code":"ecommerce.image.generate","reservation_key":"reserve:prompt","units":1,"status":"reserved","reference_id":"charge-prompt","metadata":"{}"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/internal/v1/runtime/charge-sessions/charge-prompt":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-prompt","product_code":"ecommerce","status":"reserved","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/jobs":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &runtimeCreate)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"runtime-prompt","product_code":"ecommerce","task_type":"image_generation","provider_mode":"async","organization_id":"org-1","user_id":"user-1","source_type":"ecommerce_image_job","source_id":"job-prompt","status":"queued","stage":"queued","metadata":"{}"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	promptRepo := repository.NewPromptCenterRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{ID: "product-1", OrganizationID: "org-1", SKUCode: "SKU-001", Title: "Test Product", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := repo.CreateAsset(&models.EcommerceAsset{ID: "asset-1", OrganizationID: "org-1", UserID: "user-1", AssetType: "source", SourceType: "upload", StorageKey: "source.png", MimeType: "image/png", Metadata: `{"product_id":"product-1","sku_code":"SKU-001"}`}); err != nil {
		t.Fatalf("create asset: %v", err)
	}
	if err := promptRepo.CreatePromptRun(&models.EcommercePromptRun{ID: "prompt-1", OrganizationID: "org-1", UserID: "user-1", ProductID: "product-1", SKUCode: "SKU-001", TemplateID: "tpl-1", TemplateVersionID: "tplv-1", TemplateVersionNo: 1, TemplateCode: "tpl-code", ToolSlug: "ai-posture", SceneType: "ai_posture", Status: "validated", SchemaVersion: "prompt.schema.v1", ContentHash: "sha256:content", SourceMapHash: "sha256:source", SourceAssetBindingsJSON: `[{"slot":"source_image","asset_id":"asset-1"}]`, VariablesJSON: `{}`, InputPayloadJSON: `{}`, CompiledPromptJSON: `{"strategy":"business_layered_prompt_v1","final_prompt":"compiled final prompt","final_negative_prompt":"compiled negative","sections":[]}`, SourceMapJSON: `{}`, ValidationResultJSON: `{"valid":true,"errors":[],"warnings":[]}`}); err != nil {
		t.Fatalf("create prompt run: %v", err)
	}

	service := NewService(repo, commercialRepo, nil, productRepo, nil, platform.New(config.PlatformConfig{BaseURL: platformServer.URL, Timeout: 5 * time.Second, ServiceName: "v-ecommerce-backend", InternalServiceSecret: "platform-internal-secret"}), testImageRuntimeAppConfig()).WithPromptRepository(promptRepo)
	summary, err := service.CreateImageJob("user-1", "org-1", CreateImageJobInput{ProductID: "product-1", SKUCode: "SKU-001", SceneType: "ai_posture", PromptID: "prompt-1", RequestedVariants: 1})
	if err != nil {
		t.Fatalf("CreateImageJob() error = %v", err)
	}
	if summary.PromptID != "prompt-1" {
		t.Fatalf("summary prompt_id = %s", summary.PromptID)
	}
	contract, ok := summary.Metadata["prompt_contract"].(map[string]any)
	if !ok || contract["mode"] != "prompt_id" || contract["prompt_id"] != "prompt-1" || contract["content_hash"] != "sha256:content" {
		t.Fatalf("missing prompt contract metadata: %+v", summary.Metadata)
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(runtimeCreate.InputManifest), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	manifestContract, ok := manifest["prompt_contract"].(map[string]any)
	if !ok || manifestContract["prompt_id"] != "prompt-1" {
		t.Fatalf("missing runtime prompt contract: %+v", manifest)
	}
	params := manifest["params_snapshot"].(map[string]any)
	if params["prompt"] != "compiled final prompt" || params["negative_prompt"] != "compiled negative" {
		t.Fatalf("runtime did not use compiled prompt snapshot: %+v", params)
	}
}

func TestCreateImageJobDoesNotOverwriteRuntimeCallbackRace(t *testing.T) {
	t.Helper()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-race",
		OrganizationID: "org-race",
		SKUCode:        "SKU-RACE",
		Title:          "Race Product",
		Status:         models.ProductStatusDraft,
		AssetStatus:    models.AssetStatusReady,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := repo.CreateAsset(&models.EcommerceAsset{
		ID:             "asset-race-source",
		OrganizationID: "org-race",
		UserID:         "user-race",
		AssetType:      "source",
		SourceType:     "upload",
		StorageKey:     "ecommerce-assets/source-race.png",
		MimeType:       "image/png",
		Width:          64,
		Height:         64,
		FileName:       "source-race.png",
		Metadata:       `{"product_id":"product-race","sku_code":"SKU-RACE"}`,
	}); err != nil {
		t.Fatalf("create source asset: %v", err)
	}

	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/charge-sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-race","product_code":"ecommerce","status":"created","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"reservation-race","resource_type":"quota","billing_subject_type":"organization","billing_subject_id":"org-race","billable_item_code":"ecommerce.image.generate","reservation_key":"reserve:race","units":1,"status":"reserved","reference_id":"charge-race","metadata":"{}"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/internal/v1/runtime/charge-sessions/charge-race":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-race","product_code":"ecommerce","status":"reserved","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/jobs":
			var input platform.CreateRuntimeJobInput
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &input)
			current, err := repo.FindJobByID(input.SourceID)
			if err != nil {
				t.Errorf("FindJobByID during simulated callback: %v", err)
			} else {
				current.Status = "failed"
				current.Stage = "failed"
				current.StageMessage = "provider rejected source image"
				current.LastErrorCode = "PROVIDER_SUBMIT_FAILED"
				current.LastErrorMessage = "provider rejected source image"
				if saveErr := repo.SaveJob(current); saveErr != nil {
					t.Errorf("SaveJob during simulated callback: %v", saveErr)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"runtime-race","product_code":"ecommerce","task_type":"image_generation","provider_code":"volcengine","provider_mode":"async","organization_id":"org-race","user_id":"user-race","source_type":"ecommerce_image_job","source_id":"` + input.SourceID + `","status":"queued","stage":"queued","stage_message":"Runtime job queued","provider_job_id":"","input_manifest":"{}","route_snapshot":"{}","metadata":"{}"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	service := NewService(repo, commercialRepo, nil, productRepo, nil, platform.New(config.PlatformConfig{
		BaseURL:               platformServer.URL,
		Timeout:               5 * time.Second,
		ServiceName:           "v-ecommerce-backend",
		InternalServiceSecret: "platform-internal-secret",
	}), testImageRuntimeAppConfig())

	summary, err := service.CreateImageJob("user-race", "org-race", CreateImageJobInput{
		ProductID:         "product-race",
		SKUCode:           "SKU-RACE",
		SceneType:         "changing-model",
		InputMode:         "image_to_image",
		SourceAssetID:     "asset-race-source",
		Prompt:            "race smoke",
		Objective:         "speed",
		RequestedVariants: 1,
		Width:             512,
		Height:            512,
	})
	if err != nil {
		t.Fatalf("CreateImageJob() error = %v", err)
	}
	if summary.Status != "failed" || summary.Stage != "failed" {
		t.Fatalf("summary status overwritten by create flow: %+v", summary)
	}
	stored, err := repo.FindJobByID(summary.JobID)
	if err != nil {
		t.Fatalf("FindJobByID() error = %v", err)
	}
	if stored.Status != "failed" || stored.Stage != "failed" {
		t.Fatalf("stored status overwritten by create flow: %+v", stored)
	}
	if stored.RuntimeJobID != "runtime-race" {
		t.Fatalf("runtime binding missing after race: %+v", stored)
	}
	if stored.LastErrorCode != "PROVIDER_SUBMIT_FAILED" {
		t.Fatalf("error code lost after race: %+v", stored)
	}
}
