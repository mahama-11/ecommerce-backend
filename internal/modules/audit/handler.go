package audit

import (
	"strconv"

	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) History(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/audit-handler", "ecommerce.audit.history")
	defer span.End()

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}

	result, err := h.service.History(
		c.GetString("orgID"),
		c.GetString("userID"),
		c.Query("target_type"),
		c.Query("status"),
		limit,
		offset,
	)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "failed to load audit history", "AUDIT_HISTORY_LOAD_FAILED", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, result)
}
