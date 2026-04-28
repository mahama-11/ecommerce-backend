package promotion

import (
	"net/http"

	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/metrics"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
	audit   *auditmodule.Service
}

func NewHandler(service *Service, auditService *auditmodule.Service) *Handler {
	return &Handler{service: service, audit: auditService}
}

func (h *Handler) ResolveCode(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.code.resolve")
	defer span.End()

	result, err := h.service.ResolveCode(c.Param("code"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "resolve promotion code failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ListPrograms(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.programs.list")
	defer span.End()

	result, err := h.service.ListPrograms(c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list promotion programs failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) Overview(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.overview")
	defer span.End()

	result, err := h.service.Overview(c.GetString("orgID"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load promotion overview failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ListCodes(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.codes.list")
	defer span.End()

	result, err := h.service.ListCodes(c.GetString("orgID"), c.Query("program_code"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list promotion codes failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) CreateCode(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.codes.create")
	defer span.End()

	var req CreateCodeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid create promotion code request")
		return
	}
	result, err := h.service.CreateCode(c.GetString("orgID"), req)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "create promotion code failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_promotion_code_created_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{
			Action:        "promotion.code.create",
			TargetType:    "promotion_code",
			TargetID:      result.ID,
			Status:        "success",
			Details:       "promotion code created",
			AfterSnapshot: result,
		})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, result)
}

func (h *Handler) EnsureCode(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.codes.ensure")
	defer span.End()

	var req CreateCodeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid ensure promotion code request")
		return
	}
	result, err := h.service.EnsureCode(c.GetString("orgID"), req)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "ensure promotion code failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_promotion_code_ensured_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{
			Action:        "promotion.code.ensure",
			TargetType:    "promotion_code",
			TargetID:      result.ID,
			Status:        "success",
			Details:       "promotion code ensured",
			AfterSnapshot: result,
		})
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ListConversions(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/promotion-handler", "ecommerce.promotion.conversions.list")
	defer span.End()

	result, err := h.service.ListConversions(c.GetString("orgID"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list promotion conversions failed")
		return
	}
	response.JSONSuccess(c, result)
}
