package promptcenter

import (
	"net/http"

	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Preview(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/prompt-center-handler", "ecommerce.prompt_center.preview")
	defer span.End()
	var req PreviewPromptInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		response.JSONBindError(c, err, "invalid prompt preview request")
		return
	}
	item, err := h.service.Preview(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		span.RecordError(err)
		moduleutil.WritePlatformError(c, err, "Failed to preview ecommerce prompt")
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) Get(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/prompt-center-handler", "ecommerce.prompt_center.get")
	defer span.End()
	item, err := h.service.Get(c.GetString("orgID"), c.Param("promptId"))
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeNotFound, "Prompt not found", "ECOMMERCE_PROMPT_NOT_FOUND", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, item)
}
