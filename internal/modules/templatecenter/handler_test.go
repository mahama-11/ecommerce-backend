package templatecenter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
	"ecommerce-service/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type envelope[T any] struct {
	Code int `json:"code"`
	Data  T  `json:"data"`
}

type favoriteResponse struct {
	TemplateID string `json:"templateId"`
	Favorited  bool   `json:"favorited"`
}

type copyResponse struct {
	TemplateInstanceID string `json:"templateInstanceId"`
	TemplateID         string `json:"templateId"`
}

func TestTemplateCenterHandlerFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTemplateCenterTestDB(t)
	repo := repository.NewTemplateCenterRepository(db)
	service := NewService(repo, nil)
	if err := service.SeedPresetCatalog(); err != nil {
		t.Fatalf("seed preset catalog: %v", err)
	}

	handler := NewHandler(service)
	secret := "platform-dev-secret"
	router := gin.New()

	public := router.Group("/api/v1/ecommerce/template-center")
	public.Use(middleware.OptionalPlatformJWTAuth(secret))
	{
		public.GET("/catalog", handler.ListCatalog)
		public.GET("/catalog/recommendations", handler.Recommendations)
		public.GET("/catalog/:templateId", handler.Detail)
	}

	protected := router.Group("/api/v1/ecommerce/template-center")
	protected.Use(middleware.PlatformJWTAuth(secret))
	{
		protected.GET("/instances", handler.Instances)
		protected.GET("/favorites", handler.Favorites)
		protected.POST("/catalog/:templateId/favorite", handler.AddFavorite)
		protected.DELETE("/catalog/:templateId/favorite", handler.RemoveFavorite)
		protected.POST("/catalog/:templateId/copy", handler.CopyToMyTemplates)
		protected.POST("/catalog/:templateId/use", handler.Use)
	}

	authHeader := "Bearer " + newTemplateCenterTestToken(t, secret, "user_test_001", "org_test_001")

	listResp := performRequest(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/catalog?locale=zh", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("catalog status = %d", listResp.Code)
	}
	var listPayload envelope[[]repository.CatalogListItem]
	decodeResponse(t, listResp, &listPayload)
	if listPayload.Code != 0 || len(listPayload.Data) == 0 {
		t.Fatalf("catalog payload invalid: code=%d len=%d", listPayload.Code, len(listPayload.Data))
	}

	templateID := listPayload.Data[0].ID

	detailResp := performRequest(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/catalog/"+templateID+"?locale=zh", "")
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detailResp.Code)
	}
	var detailPayload envelope[repository.CatalogDetail]
	decodeResponse(t, detailResp, &detailPayload)
	if detailPayload.Data.Catalog.ID != templateID {
		t.Fatalf("unexpected detail template id: %s", detailPayload.Data.Catalog.ID)
	}

	recommendResp := performRequest(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/catalog/recommendations?locale=zh", "")
	if recommendResp.Code != http.StatusOK {
		t.Fatalf("recommendation status = %d", recommendResp.Code)
	}
	var recommendationPayload envelope[[]repository.CatalogListItem]
	decodeResponse(t, recommendResp, &recommendationPayload)
	if len(recommendationPayload.Data) == 0 {
		t.Fatal("recommendations should not be empty")
	}

	addFavoriteResp := performRequestWithAuth(t, router, http.MethodPost, "/api/v1/ecommerce/template-center/catalog/"+templateID+"/favorite", authHeader)
	if addFavoriteResp.Code != http.StatusCreated {
		t.Fatalf("add favorite status = %d", addFavoriteResp.Code)
	}
	var addFavoritePayload envelope[favoriteResponse]
	decodeResponse(t, addFavoriteResp, &addFavoritePayload)
	if !addFavoritePayload.Data.Favorited {
		t.Fatal("favorite response should be true")
	}

	favoritesResp := performRequestWithAuth(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/favorites?locale=zh", authHeader)
	if favoritesResp.Code != http.StatusOK {
		t.Fatalf("favorites status = %d", favoritesResp.Code)
	}
	var favoritesPayload envelope[[]repository.CatalogListItem]
	decodeResponse(t, favoritesResp, &favoritesPayload)
	if len(favoritesPayload.Data) == 0 || favoritesPayload.Data[0].ID != templateID {
		t.Fatalf("favorites payload invalid: %+v", favoritesPayload.Data)
	}

	copyResp := performRequestWithAuth(t, router, http.MethodPost, "/api/v1/ecommerce/template-center/catalog/"+templateID+"/copy", authHeader)
	if copyResp.Code != http.StatusCreated {
		t.Fatalf("copy status = %d", copyResp.Code)
	}
	var copyPayload envelope[copyResponse]
	decodeResponse(t, copyResp, &copyPayload)
	if copyPayload.Data.TemplateInstanceID == "" {
		t.Fatal("templateInstanceId should not be empty")
	}

	instancesResp := performRequestWithAuth(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/instances?locale=zh", authHeader)
	if instancesResp.Code != http.StatusOK {
		t.Fatalf("instances status = %d", instancesResp.Code)
	}
	var instancesPayload envelope[[]repository.TemplateInstanceListItem]
	decodeResponse(t, instancesResp, &instancesPayload)
	if len(instancesPayload.Data) == 0 || instancesPayload.Data[0].PresetTemplateID != templateID {
		t.Fatalf("instances payload invalid: %+v", instancesPayload.Data)
	}

	useResp := performRequestWithAuth(t, router, http.MethodPost, "/api/v1/ecommerce/template-center/catalog/"+templateID+"/use", authHeader)
	if useResp.Code != http.StatusOK {
		t.Fatalf("use status = %d", useResp.Code)
	}
	var usePayload envelope[repository.UseTemplateResponse]
	decodeResponse(t, useResp, &usePayload)
	if usePayload.Data.TargetRoute == "" || usePayload.Data.ExecutorType == "" {
		t.Fatalf("use payload invalid: %+v", usePayload.Data)
	}

	removeFavoriteResp := performRequestWithAuth(t, router, http.MethodDelete, "/api/v1/ecommerce/template-center/catalog/"+templateID+"/favorite", authHeader)
	if removeFavoriteResp.Code != http.StatusOK {
		t.Fatalf("remove favorite status = %d", removeFavoriteResp.Code)
	}
}

func TestSeedPresetCatalogDoesNotOverrideManualOfficialTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTemplateCenterTestDB(t)
	repo := repository.NewTemplateCenterRepository(db)
	service := NewService(repo, nil)

	now := time.Now()
	manual := models.TemplateCatalog{
		ID:                 "tpl_manual_ops_001",
		Slug:               "ops-manual-template",
		ExternalCode:       "OPS-MANUAL-001",
		Scope:              "official",
		ManagedSource:      "ops_manual",
		Modality:           "text",
		ExecutorType:       "chat",
		Series:             "ops_manual",
		CapabilityType:     "ops_manual",
		InteractionMode:    "form_based",
		Status:             "published",
		DefaultLocale:      "zh",
		PlatformTagsJSON:   "[]",
		IndustryTagsJSON:   "[]",
		ScenarioTagsJSON:   "[]",
		ComplianceTagsJSON: "[]",
		OwnerTeam:          "ops-team",
		CreatedBy:          "ops-user",
		UpdatedBy:          "ops-user",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatalf("create manual catalog: %v", err)
	}

	if err := service.SeedPresetCatalog(); err != nil {
		t.Fatalf("seed preset catalog: %v", err)
	}

	var item models.TemplateCatalog
	if err := db.Where("id = ?", manual.ID).First(&item).Error; err != nil {
		t.Fatalf("reload manual catalog: %v", err)
	}
	if item.ManagedSource != "ops_manual" {
		t.Fatalf("managed source changed unexpectedly: %s", item.ManagedSource)
	}
	if item.OwnerTeam != "ops-team" || item.CreatedBy != "ops-user" {
		t.Fatalf("manual catalog fields overwritten: %+v", item)
	}

	var count int64
	if err := db.Model(&models.TemplateCatalog{}).Where("id = ?", manual.ID).Count(&count).Error; err != nil {
		t.Fatalf("count manual catalog: %v", err)
	}
	if count != 1 {
		t.Fatalf("manual catalog count = %d", count)
	}
}

func newTemplateCenterTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	cfg := config.DatabaseConfig{
		Driver:             "sqlite",
		SQLitePath:         filepath.Join(t.TempDir(), "template-center-test.db"),
		TablePrefix:        "ecommerce_",
		AutoMigrateEnabled: true,
		AllowStartupMigrate: true,
	}

	db, err := storage.InitDB(cfg, "debug")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	return db
}

func newTemplateCenterTestToken(t *testing.T, secret, userID, orgID string) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  userID,
		"org_id":   orgID,
		"org_role": "owner",
		"exp":      time.Now().Add(10 * time.Minute).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func performRequest(t *testing.T, router http.Handler, method, path, _ string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func performRequestWithAuth(t *testing.T, router http.Handler, method, path, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", authHeader)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func decodeResponse[T any](t *testing.T, resp *httptest.ResponseRecorder, target *T) {
	t.Helper()
	if err := json.Unmarshal(resp.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, resp.Body.String())
	}
}
