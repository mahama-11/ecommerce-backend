package commercial

import (
	"strings"

	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/observability"
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
	lc := observability.StartGin(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.order.create", "ecommerce.commercial.order.create", "commercial", "order.create", commercialRequestFields(c, observability.Fields{}))
	var req CreateOrderInput
	if err := c.ShouldBindJSON(&req); err != nil {
		lc.Fail(err, "commercial_order_request_invalid", commercialRequestFields(c, observability.Fields{"failure_category": "request_validation"}))
		response.JSONBindError(c, err, "invalid create commercial order request")
		return
	}
	result, err := h.service.CreateOrder(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		lc.Fail(err, "commercial_order_create_failed", commercialCreateOrderFields(c, req, nil, observability.Fields{"failure_category": classifyCommercialOrderFailure(err)}))
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
	lc.Finish(commercialCreateOrderFields(c, req, result, nil))
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
	lc := observability.StartGin(c, "ecommerce-service/commercial-handler", "ecommerce.commercial.payment.confirm", "ecommerce.commercial.payment.confirm", "commercial", "payment.confirm", commercialPaymentFields(c, ConfirmOrderPaymentInput{}, nil, nil))
	var req ConfirmOrderPaymentInput
	if err := c.ShouldBindJSON(&req); err != nil {
		lc.Fail(err, "commercial_payment_request_invalid", commercialPaymentFields(c, req, nil, observability.Fields{"failure_category": "request_validation"}))
		response.JSONBindError(c, err, "invalid confirm order payment request")
		return
	}
	result, err := h.service.ConfirmOrderPayment(c.GetString("userID"), c.GetString("orgID"), c.Param("orderID"), req, commercialRequestFields(c, nil))
	if err != nil {
		lc.Fail(err, "commercial_payment_confirm_failed", commercialPaymentFields(c, req, nil, observability.Fields{"failure_category": classifyCommercialPaymentFailure(err)}))
		writeConfirmOrderPaymentError(c, err)
		return
	}
	lc.Finish(commercialPaymentFields(c, req, result, nil))
	response.JSONSuccess(c, result)
}

func commercialRequestFields(c *gin.Context, extra observability.Fields) observability.Fields {
	fields := observability.Fields{"request_id": c.GetString("requestID"), "trace_id": c.GetString("traceID"), "org_id": c.GetString("orgID"), "user_id": c.GetString("userID")}
	for key, value := range extra {
		fields[key] = value
	}
	return fields
}

func commercialCreateOrderFields(c *gin.Context, req CreateOrderInput, view *OrderView, extra observability.Fields) observability.Fields {
	fields := commercialRequestFields(c, observability.Fields{"sku_code": req.SKUCode, "package_code": req.PackageCode, "quantity": req.Quantity})
	if view != nil && view.Order != nil {
		fields["order_id"] = view.Order.ID
		fields["product_code"] = view.Order.ProductCode
		fields["currency"] = view.Order.Currency
		fields["amount"] = view.Order.TotalAmount
		fields["payment_status"] = view.Order.PaymentStatus
		fields["order_status"] = view.Order.Status
	}
	for key, value := range extra {
		fields[key] = value
	}
	return fields
}

func commercialPaymentFields(c *gin.Context, req ConfirmOrderPaymentInput, view *OrderView, extra observability.Fields) observability.Fields {
	fields := commercialRequestFields(c, observability.Fields{"order_id": c.Param("orderID"), "payment_method": req.PaymentMethod, "provider_code": req.ProviderCode, "payment_asset_code": req.PaymentAssetCode})
	if view != nil && view.Order != nil {
		fields["order_id"] = view.Order.ID
		fields["product_code"] = view.Order.ProductCode
		fields["sku_code"] = view.Order.SKUCode
		fields["package_code"] = view.Order.PackageCode
		fields["currency"] = view.Order.Currency
		fields["amount"] = view.Order.TotalAmount
		fields["payment_status"] = view.Order.PaymentStatus
		fields["fulfillment_status"] = view.Order.FulfillmentStatus
	}
	if view != nil && view.Payment != nil {
		fields["payment_id"] = view.Payment.ID
		fields["payment_status"] = view.Payment.Status
	}
	for key, value := range extra {
		fields[key] = value
	}
	return fields
}

func classifyCommercialOrderFailure(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if platform.IsNotFound(err) || strings.Contains(msg, "not found") {
		return "offering_not_found"
	}
	if strings.Contains(msg, "required") || strings.Contains(msg, "not active") || strings.Contains(msg, "metadata") {
		return "request_validation"
	}
	if strings.Contains(msg, "active subscription already exists") {
		return "order_state_conflict"
	}
	return "order_internal"
}

func classifyCommercialPaymentFailure(err error) string {
	if err == nil {
		return ""
	}
	if platform.ErrorCode(err) == "WALLET_LEDGER_INSUFFICIENT_BALANCE" {
		return "insufficient_balance"
	}
	if platform.IsNotFound(err) {
		return "order_not_found"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "payment") || strings.Contains(msg, "wallet") || strings.Contains(msg, "ledger") {
		return "payment_state"
	}
	if strings.Contains(msg, "quota") || strings.Contains(msg, "fulfillment") {
		return "fulfillment_failed"
	}
	return "payment_internal"
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
