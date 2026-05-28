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
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"ecommerce-service/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type envelope[T any] struct {
	Code int `json:"code"`
	Data T   `json:"data"`
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
	service := NewService(repo, nil, nil)
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
	service := NewService(repo, nil, nil)

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

func TestTemplateCenterHandlerFlowWithPlatformProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTemplateCenterTestDB(t)
	repo := repository.NewTemplateCenterRepository(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope := func(data any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":      0,
				"message":   "ok",
				"timestamp": time.Now().Unix(),
				"data":      data,
			})
		}
		switch r.URL.Path {
		case "/internal/v1/template-ops/catalog":
			writeEnvelope(map[string]any{
				"items": []map[string]any{
					{
						"template_ref":    "ecommerce:tpl_platform_001",
						"product_code":    "ecommerce",
						"template_id":     "tpl_platform_001",
						"slug":            "changing-model-m1-t01-template",
						"name":            "Platform Ecommerce Template",
						"summary":         "Managed by platform",
						"status":          "active",
						"cover_asset_url": "",
						"cover_asset_id":  "",
						"recommend_score": 95,
						"tags":            []string{"fashion"},
						"platforms":       []string{"amazon"},
						"series":          "model_image",
						"capability_type": "model_swap",
						"modality":        "image",
						"scope":           "official",
						"managed_source":  "platform_projection",
						"raw": map[string]any{
							"external_code":     "M1-T01",
							"executor_type":     "image",
							"interaction_mode":  "form_based",
							"industry_tags":     []string{"fashion"},
							"scenario_tags":     []string{"model"},
							"featured":          true,
							"success_rate_hint": 93,
						},
					},
				},
				"total": 1,
			})
		case "/internal/v1/template-ops/catalog/ecommerce:tpl_platform_001":
			writeEnvelope(map[string]any{
				"item": map[string]any{
					"template_ref":    "ecommerce:tpl_platform_001",
					"product_code":    "ecommerce",
					"template_id":     "tpl_platform_001",
					"slug":            "changing-model-m1-t01-template",
					"name":            "Platform Ecommerce Template",
					"summary":         "Managed by platform",
					"status":          "active",
					"cover_asset_url": "",
					"cover_asset_id":  "",
					"recommend_score": 95,
					"tags":            []string{"fashion"},
					"platforms":       []string{"amazon"},
					"series":          "model_image",
					"capability_type": "model_swap",
					"modality":        "image",
					"scope":           "official",
					"managed_source":  "platform_projection",
					"raw": map[string]any{
						"external_code": "M1-T01",
					},
				},
				"product": "ecommerce",
				"detail_raw": map[string]any{
					"summary":      "Managed by platform",
					"inputSchema":  map[string]any{"image": true},
					"outputSchema": map[string]any{"images": true},
					"executionSchema": map[string]any{
						"route":            "/api/v1/ecommerce/image-runtime/jobs",
						"supportsAsyncJob": true,
						"supportsBatch":    true,
					},
					"promptLayers":     map[string]any{"system": "prompt"},
					"defaultVariables": map[string]any{"locale": "zh"},
					"toolBinding":      map[string]any{"toolSlug": "changing-model"},
					"examples": []any{
						map[string]any{
							"id":              "example-1",
							"exampleType":     "preview",
							"title":           "Preview One",
							"sourceRef":       "templates/changing-model/M1-T01/example-1",
							"storageKey":      "ecommerce/template-examples/example-1.png",
							"previewAssetUrl": "/api/v1/ecommerce/template-center/assets/preview?storage_key=ecommerce%2Ftemplate-examples%2Fexample-1.png",
						},
					},
				},
			})
		case "/internal/v1/storage/assets/resolve":
			writeEnvelope(map[string]any{
				"items": []map[string]any{
					{
						"id":           "asset_1",
						"product_code": "ecommerce",
						"category":     "template-examples",
						"source_type":  "template_example",
						"source_ref":   "templates/changing-model/M1-T01/example-1",
						"storage_key":  "ecommerce/template-examples/example-1.png",
						"file_name":    "example-1.png",
						"mime_type":    "image/png",
						"checksum":     "abc",
						"status":       "active",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService(repo, nil, platform.New(config.PlatformConfig{
		BaseURL:               server.URL,
		Timeout:               2 * time.Second,
		InternalServiceSecret: "platform-dev-secret",
		ServiceName:           "ecommerce-test",
	}))
	if err := service.SeedPresetCatalog(); err != nil {
		t.Fatalf("seed preset catalog before platform preference test: %v", err)
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

	authHeader := "Bearer " + newTemplateCenterTestToken(t, secret, "user_platform_001", "org_platform_001")

	listResp := performRequest(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/catalog?locale=zh", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("platform catalog status = %d body=%s", listResp.Code, listResp.Body.String())
	}
	var listPayload envelope[[]repository.CatalogListItem]
	decodeResponse(t, listResp, &listPayload)
	if len(listPayload.Data) != 1 || listPayload.Data[0].ID != "tpl_platform_001" {
		t.Fatalf("platform catalog payload invalid: %+v", listPayload.Data)
	}

	detailResp := performRequest(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/catalog/tpl_platform_001?locale=zh", "")
	if detailResp.Code != http.StatusOK {
		t.Fatalf("platform detail status = %d body=%s", detailResp.Code, detailResp.Body.String())
	}
	var detailPayload envelope[repository.CatalogDetail]
	decodeResponse(t, detailResp, &detailPayload)
	if len(detailPayload.Data.Examples) != 1 || detailPayload.Data.Schema.ToolBinding["toolSlug"] != "changing-model" {
		t.Fatalf("platform detail payload invalid: %+v", detailPayload.Data)
	}

	recommendResp := performRequest(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/catalog/recommendations?locale=zh", "")
	if recommendResp.Code != http.StatusOK {
		t.Fatalf("platform recommendations status = %d body=%s", recommendResp.Code, recommendResp.Body.String())
	}

	addFavoriteResp := performRequestWithAuth(t, router, http.MethodPost, "/api/v1/ecommerce/template-center/catalog/tpl_platform_001/favorite", authHeader)
	if addFavoriteResp.Code != http.StatusCreated {
		t.Fatalf("platform add favorite status = %d body=%s", addFavoriteResp.Code, addFavoriteResp.Body.String())
	}

	favoritesResp := performRequestWithAuth(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/favorites?locale=zh", authHeader)
	if favoritesResp.Code != http.StatusOK {
		t.Fatalf("platform favorites status = %d body=%s", favoritesResp.Code, favoritesResp.Body.String())
	}
	var favoritesPayload envelope[[]repository.CatalogListItem]
	decodeResponse(t, favoritesResp, &favoritesPayload)
	if len(favoritesPayload.Data) != 1 || !favoritesPayload.Data[0].IsFavorited {
		t.Fatalf("platform favorites payload invalid: %+v", favoritesPayload.Data)
	}

	copyResp := performRequestWithAuth(t, router, http.MethodPost, "/api/v1/ecommerce/template-center/catalog/tpl_platform_001/copy", authHeader)
	if copyResp.Code != http.StatusCreated {
		t.Fatalf("platform copy status = %d body=%s", copyResp.Code, copyResp.Body.String())
	}

	instancesResp := performRequestWithAuth(t, router, http.MethodGet, "/api/v1/ecommerce/template-center/instances?locale=zh", authHeader)
	if instancesResp.Code != http.StatusOK {
		t.Fatalf("platform instances status = %d body=%s", instancesResp.Code, instancesResp.Body.String())
	}

	useResp := performRequestWithAuth(t, router, http.MethodPost, "/api/v1/ecommerce/template-center/catalog/tpl_platform_001/use", authHeader)
	if useResp.Code != http.StatusOK {
		t.Fatalf("platform use status = %d body=%s", useResp.Code, useResp.Body.String())
	}
	var usePayload envelope[repository.UseTemplateResponse]
	decodeResponse(t, useResp, &usePayload)
	if usePayload.Data.TargetRoute != "/api/v1/ecommerce/image-runtime/jobs" || usePayload.Data.ToolSlug != "changing-model" {
		t.Fatalf("platform use payload invalid: %+v", usePayload.Data)
	}
}

func TestPlatformProjectionEmptyOrNotFoundFallsBackToLocalFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTemplateCenterTestDB(t)
	repo := repository.NewTemplateCenterRepository(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/v1/template-ops/catalog":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": map[string]any{"items": []any{}, "total": 0}})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "message": "not found", "error_code": "not_found"})
		}
	}))
	defer server.Close()

	service := NewService(repo, nil, platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: 2 * time.Second, InternalServiceSecret: "platform-dev-secret", ServiceName: "ecommerce-test"}))
	if err := service.SeedPresetCatalog(); err != nil {
		t.Fatalf("seed preset catalog: %v", err)
	}
	scope := repository.Scope{UserID: "user_fallback_001", OrgID: "org_fallback_001"}
	items, err := service.ListCatalog(scope, repository.TemplateCatalogFilter{Locale: "zh", Limit: 3})
	if err != nil {
		t.Fatalf("fallback list: %v", err)
	}
	if len(items) == 0 || items[0].ID == "" {
		t.Fatalf("fallback list returned no local templates: %+v", items)
	}
	templateID := items[0].ID
	detail, err := service.Detail(scope, templateID, "zh")
	if err != nil {
		t.Fatalf("fallback detail: %v", err)
	}
	if detail.Catalog.ID != templateID || detail.Version.ID == "" {
		t.Fatalf("fallback detail invalid: %+v", detail)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ecommerce/template-center/catalog/"+templateID+"/favorite", nil)
	if err := service.AddFavorite(c, scope, templateID); err != nil {
		t.Fatalf("fallback favorite: %v", err)
	}
	favorites, err := service.Favorites(scope, "zh")
	if err != nil {
		t.Fatalf("fallback favorites: %v", err)
	}
	if len(favorites) != 1 || favorites[0].ID != templateID || !favorites[0].IsFavorited {
		t.Fatalf("fallback favorites invalid: %+v", favorites)
	}
	instance, err := service.CopyToMyTemplates(c, scope, templateID)
	if err != nil {
		t.Fatalf("fallback copy: %v", err)
	}
	if instance.PresetTemplateID != templateID {
		t.Fatalf("fallback copy invalid: %+v", instance)
	}
	use, err := service.Use(c, scope, templateID)
	if err != nil {
		t.Fatalf("fallback use: %v", err)
	}
	if use.TargetRoute == "" || use.ToolSlug == "" {
		t.Fatalf("fallback use invalid: %+v", use)
	}
}

func TestPlatformProjectionUnavailableFallsBackToLocalDetail(t *testing.T) {
	db := newTemplateCenterTestDB(t)
	repo := repository.NewTemplateCenterRepository(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 502, "message": "platform unavailable", "error_code": "platform_bad_gateway"})
	}))
	defer server.Close()
	service := NewService(repo, nil, platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: 2 * time.Second, InternalServiceSecret: "platform-dev-secret", ServiceName: "ecommerce-test"}))
	if err := service.SeedPresetCatalog(); err != nil {
		t.Fatalf("seed preset catalog: %v", err)
	}
	scope := repository.Scope{UserID: "user_err", OrgID: "org_err"}
	items, err := service.ListCatalog(scope, repository.TemplateCatalogFilter{Locale: "zh", Limit: 1})
	if err != nil {
		t.Fatalf("list should fall back on platform unavailable: %v", err)
	}
	if len(items) != 1 || items[0].ID == "" {
		t.Fatalf("fallback list invalid: %+v", items)
	}
	detail, err := service.Detail(scope, items[0].ID, "zh")
	if err != nil {
		t.Fatalf("detail should fall back on platform unavailable: %v", err)
	}
	if detail.Catalog.ID != items[0].ID || detail.Version.ID == "" {
		t.Fatalf("fallback detail invalid: %+v", detail)
	}
}

func TestTemplateCatalogContextFilterUsesInputModeAndToolApplicability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTemplateCenterTestDB(t)
	repo := repository.NewTemplateCenterRepository(db)
	now := time.Now()
	catalogs := []models.TemplateCatalog{
		{ID: "tpl_ctx_multi", Slug: "ctx-multi", Scope: "official", ManagedSource: "ops_manual", Modality: "image", ExecutorType: "image_tool", Series: "ctx", CapabilityType: "generation", InteractionMode: "form_based", Status: "published", CurrentVersionID: "ver_ctx_multi", DefaultLocale: "zh", PlatformTagsJSON: `["amazon"]`, IndustryTagsJSON: `["fashion"]`, ScenarioTagsJSON: `["hero"]`, ComplianceTagsJSON: `[]`, RecommendScore: 20, CreatedAt: now, UpdatedAt: now},
		{ID: "tpl_ctx_text", Slug: "ctx-text", Scope: "official", ManagedSource: "ops_manual", Modality: "image", ExecutorType: "image_tool", Series: "ctx", CapabilityType: "generation", InteractionMode: "form_based", Status: "published", CurrentVersionID: "ver_ctx_text", DefaultLocale: "zh", PlatformTagsJSON: `["amazon"]`, IndustryTagsJSON: `["fashion"]`, ScenarioTagsJSON: `["hero"]`, ComplianceTagsJSON: `[]`, RecommendScore: 10, CreatedAt: now, UpdatedAt: now},
	}
	for _, catalog := range catalogs {
		if err := db.Create(&catalog).Error; err != nil {
			t.Fatalf("create catalog %s: %v", catalog.ID, err)
		}
		if err := db.Create(&models.TemplateCatalogLocale{ID: "loc_" + catalog.ID, TemplateCatalogID: catalog.ID, Locale: "zh", Name: catalog.ID, Summary: catalog.ID, Description: catalog.ID}).Error; err != nil {
			t.Fatalf("create locale: %v", err)
		}
		if err := db.Create(&models.TemplateCatalogVersion{ID: catalog.CurrentVersionID, TemplateCatalogID: catalog.ID, VersionNo: 1, VersionLabel: "v1", Status: "published", IsPublishable: true, IsDefault: true}).Error; err != nil {
			t.Fatalf("create version: %v", err)
		}
	}
	if err := db.Create(&models.TemplateCatalogSchema{ID: "schema_multi", TemplateVersionID: "ver_ctx_multi", InputSchemaJSON: `{"required_assets":[{"slot":"front","role":"reference","label":"Front","required":true,"constraints":{"mime_type":"image/png"}}]}`, OutputSchemaJSON: `{}`, ExecutionSchemaJSON: `{"route":"/api/v1/ecommerce/image-jobs"}`, PromptLayersJSON: `{}`, PolicySchemaJSON: `{"applicability":{"input_modes":["multi_image"],"product_categories":["apparel"],"provider_capabilities":["multi_image"]}}`, DefaultVariablesJSON: `{}`, ToolBindingJSON: `{"toolSlug":"changing-model"}`}).Error; err != nil {
		t.Fatalf("create multi schema: %v", err)
	}
	if err := db.Create(&models.TemplateCatalogSchema{ID: "schema_text", TemplateVersionID: "ver_ctx_text", InputSchemaJSON: `{}`, OutputSchemaJSON: `{}`, ExecutionSchemaJSON: `{"route":"/api/v1/ecommerce/image-jobs"}`, PromptLayersJSON: `{}`, PolicySchemaJSON: `{"applicability":{"input_modes":["text_to_image"],"product_categories":["apparel"],"provider_capabilities":["text_to_image"]}}`, DefaultVariablesJSON: `{}`, ToolBindingJSON: `{"toolSlug":"text-image"}`}).Error; err != nil {
		t.Fatalf("create text schema: %v", err)
	}

	items, err := repo.ListCatalog(repository.Scope{}, repository.TemplateCatalogFilter{Locale: "zh", ToolSlug: "changing-model", InputMode: "multi_image", ProductCategory: "apparel", ProviderCapability: "multi_image"})
	if err != nil {
		t.Fatalf("ListCatalog context filter: %v", err)
	}
	if len(items) != 1 || items[0].ID != "tpl_ctx_multi" || len(items[0].InputModes) != 1 {
		t.Fatalf("context filter returned unexpected items: %+v", items)
	}
	detail, err := repo.GetCatalogDetail(repository.Scope{}, "tpl_ctx_multi", "zh")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.Schema.RequiredAssets) != 1 || detail.Schema.RequiredAssets[0].Slot != "front" {
		t.Fatalf("required assets not projected: %+v", detail.Schema.RequiredAssets)
	}
}

func newTemplateCenterTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	cfg := config.DatabaseConfig{
		Driver:              "sqlite",
		SQLitePath:          filepath.Join(t.TempDir(), "template-center-test.db"),
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

func TestPlatformProjectionDerivesApplicabilityAndKeepsLegacyInputMode(t *testing.T) {
	items := mapPlatformCatalogListItems([]platform.PlatformTemplateCatalogItem{
		{TemplateID: "tpl_policy", Slug: "policy-template", Name: "Policy", Platforms: []string{"amazon"}, Raw: map[string]any{
			"executionSchema": map[string]any{"route": "/api/v1/ecommerce/image-jobs"},
			"policySchema": map[string]any{"applicability": map[string]any{
				"input_modes":              []any{"multi_image"},
				"product_categories":       []any{"apparel"},
				"provider_capabilities":    []any{"multi_image"},
				"product_category_exclude": []any{"jewelry"},
			}},
		}},
		{TemplateID: "tpl_legacy", Slug: "legacy-template", Name: "Legacy", Platforms: []string{"amazon"}, Raw: map[string]any{
			"executionSchema": map[string]any{"route": "/api/v1/ecommerce/image-jobs"},
		}},
		{TemplateID: "tpl_tool", Slug: "tool-template", Name: "Tool", Platforms: []string{"amazon"}, Raw: map[string]any{
			"toolBinding": map[string]any{"applicability": map[string]any{"inputModes": []any{"text_to_image"}, "providerCapabilities": []any{"text_to_image"}}},
		}},
	})
	if len(items[0].InputModes) != 1 || items[0].InputModes[0] != "multi_image" || len(items[0].ProviderCapabilities) != 1 || items[0].ProviderCapabilities[0] != "multi_image" {
		t.Fatalf("policy applicability was not projected: %+v", items[0])
	}
	if !matchesPlatformCatalogFilter(items[1], repository.TemplateCatalogFilter{InputMode: "text_to_image"}) {
		t.Fatalf("platform legacy catalog without declared input modes should remain visible")
	}
	if matchesPlatformCatalogFilter(items[0], repository.TemplateCatalogFilter{InputMode: "text_to_image"}) {
		t.Fatalf("platform catalog with declared modes should reject absent requested mode")
	}
	detail := mapPlatformCatalogDetail(&platform.PlatformTemplateCatalogDetail{Item: platform.PlatformTemplateCatalogItem{TemplateID: "tpl_detail", Slug: "detail-template", Name: "Detail", Raw: map[string]any{"toolBinding": map[string]any{"applicability": map[string]any{"inputModes": []any{"text_to_image"}, "providerCapabilities": []any{"text_to_image"}}}}}})
	if len(detail.Catalog.InputModes) != 1 || detail.Catalog.InputModes[0] != "text_to_image" || detail.Schema.Applicability["inputModes"] == nil {
		t.Fatalf("detail applicability was not projected from tool binding: %+v", detail)
	}
}
