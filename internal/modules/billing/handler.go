package billing

import (
	"net/http"

	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/metrics"
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
	span := telemetry.StartGinSpan(c, "ecommerce-service/billing-handler", "ecommerce.billing.summary")
	defer span.End()

	result, err := h.service.Summary(c.GetString("orgID"))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "load billing summary failed", "BILLING_SUMMARY_LOAD_FAILED", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ListCharges(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/billing-handler", "ecommerce.billing.charges.list")
	defer span.End()

	result, err := h.service.ListCharges(c.GetString("orgID"), moduleutil.QueryInt(c, "limit", 100), moduleutil.QueryInt(c, "offset", 0))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "list billing charges failed", "BILLING_CHARGES_LIST_FAILED", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) RecordCharge(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/billing-handler", "ecommerce.billing.charge.record")
	defer span.End()

	var req RecordChargeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid record charge request")
		return
	}
	result, err := h.service.RecordCharge(req)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "record billing charge failed", "BILLING_CHARGE_RECORD_FAILED", "Check the payload and retry.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_billing_charge_recorded_total")
	response.JSONSuccessWithStatus(c, http.StatusCreated, result)
}

func (h *Handler) RefundCharge(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/billing-handler", "ecommerce.billing.charge.refund")
	defer span.End()

	var req RefundChargeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid refund charge request")
		return
	}
	result, err := h.service.RefundCharge(c.Param("recordID"), req)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "refund billing charge failed", "BILLING_CHARGE_REFUND_FAILED", "Check the payload and retry.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_billing_charge_refunded_total")
	response.JSONSuccess(c, result)
}

func (h *Handler) ReplayOutbox(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/billing-handler", "ecommerce.billing.outbox.replay")
	defer span.End()

	var req ReplayOutboxInput
	_ = c.ShouldBindJSON(&req)
	result, err := h.service.ReplayOutbox(req.Limit)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeDatabaseError, "replay commercial outbox failed", "COMMERCIAL_OUTBOX_REPLAY_FAILED", "Retry later after checking platform connectivity.")
		return
	}
	response.JSONSuccess(c, result)
}
