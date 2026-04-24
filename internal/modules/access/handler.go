package access

import (
	"ecommerce-service/internal/modules/authz"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ authz *authz.Service }

func NewHandler(authzService *authz.Service) *Handler { return &Handler{authz: authzService} }

func (h *Handler) Me(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/access-handler", "ecommerce.access.me")
	defer span.End()

	result, err := h.authz.Resolve(c.GetString("userID"), c.GetString("orgID"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeExternalDependency, "resolve access failed", "ACCESS_RESOLVE_FAILED", "Retry later or sign in again.")
		return
	}
	response.JSONSuccess(c, result)
}
