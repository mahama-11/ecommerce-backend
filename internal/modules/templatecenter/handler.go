package templatecenter

import (
	"io"
	"net/http"

	"ecommerce-service/internal/repository"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func scopeFromContext(c *gin.Context) repository.Scope {
	return repository.Scope{UserID: c.GetString("userID"), OrgID: c.GetString("orgID")}
}

func (h *Handler) ListCatalog(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.catalog.list")
	defer span.End()
	items, err := h.service.ListCatalog(scopeFromContext(c), repository.TemplateCatalogFilter{Locale: c.DefaultQuery("locale", "zh"), Keyword: c.Query("keyword"), Modality: c.Query("modality"), Series: c.Query("series"), Capability: c.Query("capability"), Platform: c.Query("platform"), SortBy: c.DefaultQuery("sortBy", "recommended")})
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to load template catalog", "TEMPLATE_CATALOG_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) Facets(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.catalog.facets")
	defer span.End()
	items, err := h.service.Facets(repository.TemplateCatalogFilter{Locale: c.DefaultQuery("locale", "zh"), Keyword: c.Query("keyword"), Modality: c.Query("modality"), Series: c.Query("series"), Capability: c.Query("capability"), Platform: c.Query("platform")})
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to load template facets", "TEMPLATE_FACETS_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) Recommendations(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.catalog.recommendations")
	defer span.End()
	items, err := h.service.Recommendations(scopeFromContext(c), c.DefaultQuery("locale", "zh"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to load recommendations", "TEMPLATE_RECOMMENDATION_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) Detail(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.catalog.detail")
	defer span.End()
	item, err := h.service.Detail(scopeFromContext(c), c.Param("templateId"), c.DefaultQuery("locale", "zh"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeNotFound, "template not found", "TEMPLATE_NOT_FOUND", "Check the template id and try again.")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) PreviewAsset(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.asset.preview")
	defer span.End()
	storageKey := c.Query("storage_key")
	if storageKey == "" {
		response.JSONErrorSemantic(c, response.CodeMissingParameter, "storage_key is required", "TEMPLATE_ASSET_STORAGE_KEY_REQUIRED", "Provide a storage_key and try again.")
		return
	}
	body, headers, err := h.service.DownloadExampleAsset(storageKey)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeNotFound, "template asset not found", "TEMPLATE_ASSET_NOT_FOUND", "Check the storage key and try again.")
		return
	}
	defer body.Close()
	for key, value := range headers {
		if value != "" {
			c.Header(key, value)
		}
	}
	if c.GetHeader("Cache-Control") == "" {
		c.Header("Cache-Control", "public, max-age=300")
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}

func (h *Handler) Favorites(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.favorite.list")
	defer span.End()
	items, err := h.service.Favorites(scopeFromContext(c), c.DefaultQuery("locale", "zh"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to load favorites", "TEMPLATE_FAVORITES_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) Instances(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.instance.list")
	defer span.End()
	items, err := h.service.Instances(scopeFromContext(c), c.DefaultQuery("locale", "zh"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to load template instances", "TEMPLATE_INSTANCES_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) AddFavorite(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.favorite.add")
	defer span.End()
	if err := h.service.AddFavorite(c, scopeFromContext(c), c.Param("templateId")); err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to favorite template", "TEMPLATE_FAVORITE_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, gin.H{"templateId": c.Param("templateId"), "favorited": true})
}

func (h *Handler) RemoveFavorite(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.favorite.remove")
	defer span.End()
	if err := h.service.RemoveFavorite(c, scopeFromContext(c), c.Param("templateId")); err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to unfavorite template", "TEMPLATE_UNFAVORITE_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, gin.H{"templateId": c.Param("templateId"), "favorited": false})
}

func (h *Handler) CopyToMyTemplates(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.copy_to_my_templates")
	defer span.End()
	instance, err := h.service.CopyToMyTemplates(c, scopeFromContext(c), c.Param("templateId"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to copy template", "TEMPLATE_COPY_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, gin.H{"templateInstanceId": instance.ID, "templateId": c.Param("templateId")})
}

func (h *Handler) Use(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/template-center-handler", "ecommerce.template_center.use")
	defer span.End()
	result, err := h.service.Use(c, scopeFromContext(c), c.Param("templateId"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to resolve template use route", "TEMPLATE_USE_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, result)
}
