package imageruntime

import (
	"bytes"
	"encoding/json"
	"io"
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
)

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

func TestRegisterSourceAssetPropagatesPlatformUploadBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/storage/assets" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":1000,"message":"invalid asset upload request","error_code":"STORAGE_ASSET_PAYLOAD_INVALID","error_hint":"Send a valid data URL or base64-encoded image payload.","error":"http: request body too large"}`))
	}))
	defer platformServer.Close()

	db := newImageRuntimeTestDB(t)
	repo := repository.NewImageRuntimeRepository(db)
	productRepo := repository.NewProductCenterRepository(db)
	if err := db.Create(&models.EcomProductSKU{
		ID:             "product-upload-limit",
		OrganizationID: "org-1",
		SKUCode:        "SKU-UPLOAD-LIMIT",
		Title:          "Upload Limit Product",
		Status:         models.ProductStatusDraft,
		AssetStatus:    models.AssetStatusMissing,
		ListingStatus:  models.ListingStatusMissing,
		ExportStatus:   models.ExportStatusPending,
	}).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	service := NewService(repo, repository.NewCommercialRepository(db), nil, productRepo, nil, platform.New(config.PlatformConfig{
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/assets/source", bytes.NewBufferString(`{"product_id":"product-upload-limit","sku_code":"SKU-UPLOAD-LIMIT","file_name":"source.png","mime_type":"image/png","payload":"data:image/png;base64,Zm9v"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("register source asset status = %d, want %d body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "STORAGE_ASSET_PAYLOAD_INVALID") {
		t.Fatalf("expected upstream storage error code to be propagated, body=%s", resp.Body.String())
	}
}
