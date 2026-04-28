package commission

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

func (h *Handler) Overview(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.overview")
	defer span.End()

	result, err := h.service.Overview(c.GetString("orgID"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load commission overview failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ListReferralCommissions(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.referrals.list")
	defer span.End()

	result, err := h.service.ListCommissions(c.GetString("orgID"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list referral commissions failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) Redeem(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.redeem")
	defer span.End()

	var req RedeemInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid redeem commission request")
		return
	}
	result, err := h.service.Redeem(c.GetString("orgID"), req)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "redeem commission failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_commission_redeemed_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{
			Action:        "commission.redeem",
			TargetType:    "commission",
			TargetID:      result.RewardLedgerID,
			Status:        "success",
			Details:       "commission redeemed to wallet asset",
			AfterSnapshot: result,
		})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, result)
}

func (h *Handler) ChannelOverview(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.channel_overview")
	defer span.End()

	result, err := h.service.ChannelOverview(c.GetString("orgID"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load channel commission overview failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ChannelBindings(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.channel_bindings")
	defer span.End()

	result, err := h.service.CurrentBindings(c.GetString("orgID"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "load channel bindings failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ChannelCommissions(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.channel_commissions")
	defer span.End()

	result, err := h.service.ListChannelCommissions(c.GetString("orgID"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list channel commissions failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) ChannelSettlements(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/commission-handler", "ecommerce.commission.channel_settlements")
	defer span.End()

	result, err := h.service.ListChannelSettlements(c.GetString("orgID"), c.Query("status"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list channel settlements failed")
		return
	}
	response.JSONSuccess(c, result)
}
