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
	visualworkflowmodule "ecommerce-service/internal/modules/visualworkflow"
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

func TestInternalCallbackMultiplexerRoutesVisualWorkflowAndPreservesLegacyImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newImageRuntimeTestDB(t)
	if err := db.AutoMigrate(&models.EcommerceVisualWorkflowSession{}, &models.EcommerceVisualSourceReference{}, &models.EcommerceVisualDeconstructionJob{}, &models.EcommerceVisualDeconstructionElement{}); err != nil {
		t.Fatalf("migrate visual workflow: %v", err)
	}
	imageRepo := repository.NewImageRuntimeRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	imageService := NewService(imageRepo, repository.NewCommercialRepository(db), nil, productRepo, nil, nil, testImageRuntimeAppConfig())
	visualService := visualworkflowmodule.NewService(repository.NewVisualWorkflowRepository(db), productRepo, imageRepo)
	handler := NewHandler(imageService).WithVisualWorkflowService(visualService)
	if err := imageRepo.CreateJob(&models.EcommerceImageJob{ID: "job-legacy", OrganizationID: "org-1", UserID: "user-1", SceneType: "ai_posture", InputMode: "image_to_image", Status: "queued", Stage: "queued"}); err != nil {
		t.Fatalf("create legacy image job: %v", err)
	}
	if err := db.Create(&models.EcommerceVisualWorkflowSession{ID: "vws-route", OrganizationID: "org-1", UserID: "user-1", ProductID: "prod-route", SKUCode: "SKU-ROUTE", CurrentStage: models.VisualWorkflowStageDeconstruction, Status: models.VisualWorkflowStatusProcessing, ReadinessJSON: `{}`, IntentSpecJSON: `{}`, PromptPlanJSON: `{}`, GenerationVersionsJSON: `[]`, Metadata: `{}`}).Error; err != nil {
		t.Fatalf("create visual session: %v", err)
	}
	if err := db.Create(&models.EcommerceVisualDeconstructionJob{ID: "visual-route-no-prefix", OrganizationID: "org-1", UserID: "user-1", SessionID: "vws-route", ProductID: "prod-route", SKUCode: "SKU-ROUTE", Status: models.VisualDeconstructionStatusQueued, Stage: "queued", InputManifestJSON: `{}`, OutputManifestJSON: `{}`, Metadata: `{}`}).Error; err != nil {
		t.Fatalf("create visual job: %v", err)
	}
	router := gin.New()
	router.POST("/internal/v1/ecommerce/jobs/:jobID/runtime", handler.InternalUpdateJobRuntime)
	router.POST("/internal/v1/ecommerce/jobs/:jobID/results", handler.InternalRecordJobResults)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/visual-route-no-prefix/runtime", bytes.NewBufferString(`{"status":"running","progress":42,"stage":"analyzing","runtime_job_id":"runtime-visual"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("visual runtime route status=%d body=%s", resp.Code, resp.Body.String())
	}
	var visualJob models.EcommerceVisualDeconstructionJob
	if err := db.First(&visualJob, "id = ?", "visual-route-no-prefix").Error; err != nil || visualJob.RuntimeJobID != "runtime-visual" || visualJob.Progress != 42 {
		t.Fatalf("visual route did not update visual job: job=%+v err=%v", visualJob, err)
	}
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/job-legacy/runtime", bytes.NewBufferString(`{"status":"processing","progress":13,"provider_job_id":"provider-legacy"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("legacy runtime route status=%d body=%s", resp.Code, resp.Body.String())
	}

	if err := db.Create(&models.EcommerceVisualWorkflowSession{ID: "vws-gen-route", OrganizationID: "org-1", UserID: "user-1", ProductID: "prod-gen-route", SKUCode: "SKU-GEN-ROUTE", CurrentStage: models.VisualWorkflowStageGeneration, Status: models.VisualWorkflowStatusProcessing, ReadinessJSON: `{}`, IntentSpecJSON: `{}`, PromptPlanJSON: `{}`, GenerationVersionsJSON: `[{"version_id":"gv-route","status":"processing","stage":"running","created_at":"2026-05-14T00:00:00Z","updated_at":"2026-05-14T00:00:00Z"}]`, Metadata: `{}`}).Error; err != nil {
		t.Fatalf("create visual generation session: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/gv-route/runtime?source_type=visual_generation", bytes.NewBufferString(`{"status":"processing","progress":55,"stage":"provider_running","runtime_job_id":"runtime-generation"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("visual generation runtime route status=%d body=%s", resp.Code, resp.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/ecommerce/jobs/gv-route/results?source_type=visual_generation", bytes.NewBufferString(`{"status":"completed","progress":100,"stage":"completed","variants":[{"status":"completed","is_selected":true,"asset":{"storage_key":"generated/gv-route.png","mime_type":"image/png","file_name":"gv-route.png"}}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "storage_key") {
		t.Fatalf("visual generation result route status=%d body=%s", resp.Code, resp.Body.String())
	}
	var generatedAsset models.EcommerceAsset
	if err := db.Where("organization_id = ? AND storage_key = ?", "org-1", "generated/gv-route.png").First(&generatedAsset).Error; err != nil {
		t.Fatalf("visual generation result did not create asset: %v", err)
	}

	legacy, err := imageRepo.FindJobByID("job-legacy")
	if err != nil || legacy.ProviderJobID != "provider-legacy" || legacy.Progress != 13 {
		t.Fatalf("legacy route did not preserve image callback: job=%+v err=%v", legacy, err)
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

func TestRecordJobResultsFailsClosedWhenMeteringFinalizeFails(t *testing.T) {
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
	if item.Status != "failed" {
		t.Fatalf("expected failed status when metering finalization fails, got %s", item.Status)
	}
	if item.Stage != "metering_failed" {
		t.Fatalf("expected metering_failed stage, got %s", item.Stage)
	}
	if item.LastErrorCode != "METERING_FINALIZATION_FAILED" {
		t.Fatalf("expected metering finalization error code, got %s", item.LastErrorCode)
	}
	if !strings.Contains(item.LastErrorMessage, "invalid finalize request") {
		t.Fatalf("expected sanitized metering failure message, got %s", item.LastErrorMessage)
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

	if err := promptRepo.CreatePromptRun(&models.EcommercePromptRun{ID: "prompt-text", OrganizationID: "org-1", UserID: "user-1", ProductID: "product-1", SKUCode: "SKU-001", TemplateID: "tpl-text", TemplateVersionID: "tplv-text", TemplateVersionNo: 1, TemplateCode: "tpl-text-code", ToolSlug: "text-image", SceneType: "ai_posture", Status: "validated", SchemaVersion: "prompt.schema.v1", ContentHash: "sha256:text", SourceMapHash: "sha256:none", SourceAssetBindingsJSON: `[]`, VariablesJSON: `{}`, InputPayloadJSON: `{}`, CompiledPromptJSON: `{"strategy":"business_layered_prompt_v1","final_prompt":"text only compiled prompt","final_negative_prompt":"text only negative","sections":[]}`, SourceMapJSON: `{}`, ValidationResultJSON: `{"valid":true,"errors":[],"warnings":[]}`}); err != nil {
		t.Fatalf("create text prompt run: %v", err)
	}
	textSummary, err := service.CreateImageJob("user-1", "org-1", CreateImageJobInput{ProductID: "product-1", SKUCode: "SKU-001", SceneType: "ai_posture", InputMode: "text_to_image", PromptID: "prompt-text", RequestedVariants: 1})
	if err != nil {
		t.Fatalf("CreateImageJob(text prompt without source bindings) error = %v", err)
	}
	if textSummary.SourceAssetID != "" {
		t.Fatalf("text_to_image prompt without bindings should not synthesize source asset, got %q", textSummary.SourceAssetID)
	}
	var textManifest map[string]any
	if err := json.Unmarshal([]byte(runtimeCreate.InputManifest), &textManifest); err != nil {
		t.Fatalf("unmarshal text manifest: %v", err)
	}
	textParams := textManifest["params_snapshot"].(map[string]any)
	if textParams["prompt"] != "text only compiled prompt" {
		t.Fatalf("text prompt did not use compiled prompt: %+v", textParams)
	}
}

func TestCreateImageJobSupportsTextAndMultiImageSourceAssetsContract(t *testing.T) {
	t.Helper()

	var runtimeCreates []platform.CreateRuntimeJobInput
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/charge-sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-contract","product_code":"ecommerce","status":"created","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/reservations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"reservation-contract","resource_type":"quota","billing_subject_type":"organization","billing_subject_id":"org-1","billable_item_code":"ecommerce.image.generate","reservation_key":"reserve:contract","units":1,"status":"reserved","reference_id":"charge-contract","metadata":"{}"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/internal/v1/runtime/charge-sessions/charge-contract":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"charge-contract","product_code":"ecommerce","status":"reserved","metadata":"{}"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/runtime/jobs":
			var input platform.CreateRuntimeJobInput
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &input)
			runtimeCreates = append(runtimeCreates, input)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":"runtime-contract","product_code":"ecommerce","task_type":"image_generation","provider_mode":"async","organization_id":"org-1","user_id":"user-1","source_type":"ecommerce_image_job","source_id":"job-contract","status":"queued","stage":"queued","metadata":"{}"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{ID: "product-1", OrganizationID: "org-1", SKUCode: "SKU-001", Title: "Test Product", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	for _, asset := range []models.EcommerceAsset{
		{ID: "asset-front", OrganizationID: "org-1", UserID: "user-1", AssetType: "source", SourceType: "upload", StorageKey: "front.png", MimeType: "image/png", Metadata: `{"product_id":"product-1","sku_code":"SKU-001"}`},
		{ID: "asset-side", OrganizationID: "org-1", UserID: "user-1", AssetType: "source", SourceType: "upload", StorageKey: "side.png", MimeType: "image/png", Metadata: `{"product_id":"product-1","sku_code":"SKU-001"}`},
	} {
		if err := repo.CreateAsset(&asset); err != nil {
			t.Fatalf("create asset %s: %v", asset.ID, err)
		}
	}
	service := NewService(repo, repository.NewCommercialRepository(db), nil, productRepo, nil, platform.New(config.PlatformConfig{BaseURL: platformServer.URL, Timeout: 5 * time.Second, ServiceName: "v-ecommerce-backend", InternalServiceSecret: "platform-internal-secret"}), testImageRuntimeAppConfig())

	textJob, err := service.CreateImageJob("user-1", "org-1", CreateImageJobInput{ProductID: "product-1", SKUCode: "SKU-001", SceneType: "ai_posture", InputMode: "text_to_image", Prompt: "new product hero"})
	if err != nil {
		t.Fatalf("text_to_image CreateImageJob() error = %v", err)
	}
	if textJob.SourceAssetID != "" {
		t.Fatalf("text_to_image should not require source asset, got %s", textJob.SourceAssetID)
	}

	_, err = service.CreateImageJob("user-1", "org-1", CreateImageJobInput{ProductID: "product-1", SKUCode: "SKU-001", SceneType: "ai_posture", InputMode: "multi_image", Prompt: "combine references", SourceAssets: []ImageJobSourceAssetInput{{Slot: "front", Role: "reference", AssetID: "asset-front", Required: true, Label: "Front"}, {Slot: "side", Role: "reference", AssetID: "asset-side", Required: true, Label: "Side"}}})
	if err != nil {
		t.Fatalf("multi_image CreateImageJob() error = %v", err)
	}
	if len(runtimeCreates) != 2 {
		t.Fatalf("runtime create count = %d, want 2", len(runtimeCreates))
	}
	var multiManifest map[string]any
	if err := json.Unmarshal([]byte(runtimeCreates[1].InputManifest), &multiManifest); err != nil {
		t.Fatalf("unmarshal multi manifest: %v", err)
	}
	ids, _ := multiManifest["source_asset_ids"].([]any)
	slotMap, _ := multiManifest["source_asset_map"].(map[string]any)
	routeHint, _ := multiManifest["route_hint"].(map[string]any)
	if multiManifest["input_mode"] != "multi_image" || len(ids) != 2 || slotMap["front"] == nil || slotMap["side"] == nil || routeHint["runtime_route_family"] != "generate/multi-image" {
		t.Fatalf("multi manifest missing source slot map or route hint: %+v", multiManifest)
	}
}
