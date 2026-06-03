package wallet

import (
	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/observability"
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
	lc := observability.StartGin(c, "ecommerce-service/wallet-handler", "ecommerce.wallet.summary", "ecommerce.wallet.summary", "wallet", "summary", walletRequestFields(c, nil))

	result, err := h.service.Summary(c.GetString("orgID"))
	if err != nil {
		lc.Fail(err, "wallet_summary_failed", walletRequestFields(c, observability.Fields{"failure_category": "wallet_summary"}))
		moduleutil.WritePlatformError(c, err, "load wallet summary failed")
		return
	}
	lc.Finish(walletSummaryFields(c, result))
	response.JSONSuccess(c, result)
}

func (h *Handler) History(c *gin.Context) {
	limit := moduleutil.QueryInt(c, "limit", 100)
	lc := observability.StartGin(c, "ecommerce-service/wallet-handler", "ecommerce.wallet.history", "ecommerce.wallet.history", "wallet", "history", walletRequestFields(c, observability.Fields{"limit": limit}))

	result, err := h.service.History(c.GetString("orgID"), limit)
	if err != nil {
		lc.Fail(err, "wallet_history_failed", walletRequestFields(c, observability.Fields{"limit": limit, "failure_category": "wallet_history"}))
		moduleutil.WritePlatformError(c, err, "load wallet history failed")
		return
	}
	lc.Finish(walletHistoryFields(c, limit, result))
	response.JSONSuccess(c, result)
}

func walletRequestFields(c *gin.Context, extra observability.Fields) observability.Fields {
	fields := observability.Fields{"request_id": c.GetString("requestID"), "trace_id": c.GetString("traceID"), "org_id": c.GetString("orgID"), "user_id": c.GetString("userID")}
	for key, value := range extra {
		fields[key] = value
	}
	return fields
}

func walletSummaryFields(c *gin.Context, result *Summary) observability.Fields {
	fields := walletRequestFields(c, observability.Fields{"account_present": false, "asset_count": 0})
	if result != nil {
		fields["account_present"] = result.BillingSubjectID != ""
		fields["product_code"] = result.ProductCode
		fields["asset_count"] = len(result.Assets)
		fields["primary_asset_code"] = result.PrimaryAssetCode
		if result.Quota != nil {
			fields["billable_item_code"] = result.Quota.BillableItemCode
			fields["quota_remaining"] = result.Quota.Remaining
		}
	}
	return fields
}

func walletHistoryFields(c *gin.Context, limit int, result *HistoryResult) observability.Fields {
	fields := walletRequestFields(c, observability.Fields{"limit": limit, "count": 0})
	if result != nil {
		fields["count"] = len(result.Items)
	}
	return fields
}
