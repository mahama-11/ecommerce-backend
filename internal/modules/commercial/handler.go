package commercial

import (
	"strings"

	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/platform"
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

func (h *Handler) GetOfferings(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.offerings.get")
	defer span.End()
	result, err := h.service.Offerings(c.GetString("orgID"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load commercial offerings failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) CreateOrder(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.order.create")
	defer span.End()
	var req CreateOrderInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid create commercial order request")
		return
	}
	result, err := h.service.CreateOrder(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		if platform.IsNotFound(err) {
			response.JSONErrorSemantic(c, response.CodeNotFound, "Commercial package not found", "COMMERCIAL_PACKAGE_NOT_FOUND", "Refresh pricing and try again.")
			return
		}
		if strings.Contains(strings.ToLower(err.Error()), "active subscription already exists") {
			response.JSONErrorSemantic(c, response.CodeConflict, "This subscription is already active", "COMMERCIAL_SUBSCRIPTION_ALREADY_ACTIVE", "Switch to another package or manage it from your account center.")
			return
		}
		moduleutil.WritePlatformError(c, err, "create commercial order failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ListOrders(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.order.list")
	defer span.End()
	result, err := h.service.ListOrders(c.GetString("orgID"), moduleutil.QueryInt(c, "limit", 20), moduleutil.QueryInt(c, "offset", 0))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "list commercial orders failed", "COMMERCIAL_ORDER_LIST_FAILED", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) GetOrder(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.order.get")
	defer span.End()
	result, err := h.service.GetOrder(c.GetString("orgID"), c.Param("orderID"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "load commercial order failed", "COMMERCIAL_ORDER_LOAD_FAILED", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ConfirmOrderPayment(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.order.confirm_payment")
	defer span.End()
	var req ConfirmOrderPaymentInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid confirm order payment request")
		return
	}
	result, err := h.service.ConfirmOrderPayment(c.GetString("userID"), c.GetString("orgID"), c.Param("orderID"), req)
	if err != nil {
		writeConfirmOrderPaymentError(c, err)
		return
	}
	response.JSONSuccess(c, result)
}

func writeConfirmOrderPaymentError(c *gin.Context, err error) {
	switch platform.ErrorCode(err) {
	case "WALLET_LEDGER_INSUFFICIENT_BALANCE":
		response.JSONErrorSemantic(c, response.CodeConflict, "Wallet balance is not enough for this purchase", "COMMERCIAL_ORDER_PAYMENT_INSUFFICIENT_BALANCE", firstNonEmpty(platform.ErrorHint(err), "Recharge your wallet balance before purchasing this package."))
		return
	}
	if platform.IsNotFound(err) {
		response.JSONErrorSemantic(c, response.CodeNotFound, "Commercial order not found", "COMMERCIAL_ORDER_NOT_FOUND", firstNonEmpty(platform.ErrorHint(err), "Refresh the page and try again."))
		return
	}
	moduleutil.WritePlatformError(c, err, "Failed to confirm order payment")
}
