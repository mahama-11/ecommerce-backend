package wallet

import (
	"ecommerce-service/internal/modules/moduleutil"
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

func (h *Handler) Summary(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/wallet-handler", "ecommerce.wallet.summary")
	defer span.End()

	result, err := h.service.Summary(c.GetString("orgID"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load wallet summary failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) History(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/wallet-handler", "ecommerce.wallet.history")
	defer span.End()

	result, err := h.service.History(c.GetString("orgID"), moduleutil.QueryInt(c, "limit", 100))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load wallet history failed")
		return
	}
	response.JSONSuccess(c, result)
}
