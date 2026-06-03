package billinggate

import (
	"strings"

	"ecommerce-service/internal/observability"
)

func billingGateFields(ctx *Context, input BeginInput) observability.Fields {
	fields := observability.Fields{
		"product_code":       firstNonEmpty(input.ProductCode, valueOrEmptyContext(ctx, func(c *Context) string { return c.ProductCode })),
		"org_id":             firstNonEmpty(input.OrganizationID, valueOrEmptyContext(ctx, func(c *Context) string { return c.OrganizationID })),
		"user_id":            firstNonEmpty(input.UserID, valueOrEmptyContext(ctx, func(c *Context) string { return c.UserID })),
		"source_type":        firstNonEmpty(input.SourceType, valueOrEmptyContext(ctx, func(c *Context) string { return c.SourceType })),
		"source_id":          firstNonEmpty(input.SourceID, valueOrEmptyContext(ctx, func(c *Context) string { return c.SourceID })),
		"action":             firstNonEmpty(input.Action, valueOrEmptyContext(ctx, func(c *Context) string { return c.Action })),
		"billable_item_code": firstNonEmpty(input.BillableItemCode, valueOrEmptyContext(ctx, func(c *Context) string { return c.BillableItemCode })),
		"resource_type":      firstNonEmpty(input.ResourceType, valueOrEmptyContext(ctx, func(c *Context) string { return c.ResourceType })),
		"charge_session_id":  valueOrEmptyContext(ctx, func(c *Context) string { return c.ChargeSessionID }),
		"reservation_id":     valueOrEmptyContext(ctx, func(c *Context) string { return c.ReservationID }),
		"reservation_key":    valueOrEmptyContext(ctx, func(c *Context) string { return c.ReservationKey }),
		"usage_units":        positiveOrDefault(input.UsageUnits, valueOrDefaultContext(ctx, func(c *Context) int64 { return c.UsageUnits }, 1)),
	}
	return fields
}

func billingGateFailureCategory(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate_card") || strings.Contains(msg, "rate card"):
		return "rate_card"
	case strings.Contains(msg, "billable_item") || strings.Contains(msg, "billable item"):
		return "billable_item"
	case strings.Contains(msg, "wallet"):
		return "wallet_post"
	case strings.Contains(msg, "quota") || strings.Contains(msg, "credit") || strings.Contains(msg, "balance") || strings.Contains(msg, "reservation"):
		return "quota_or_billing"
	case strings.Contains(msg, "settlement") || strings.Contains(msg, "finalize"):
		return "settlement"
	default:
		return "quota_or_billing"
	}
}

func valueOrEmptyContext(ctx *Context, getter func(*Context) string) string {
	if ctx == nil {
		return ""
	}
	return getter(ctx)
}

func valueOrDefaultContext(ctx *Context, getter func(*Context) int64, fallback int64) int64 {
	if ctx == nil {
		return fallback
	}
	if value := getter(ctx); value > 0 {
		return value
	}
	return fallback
}
