package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"ecommerce-service/pkg/logger"

	"gorm.io/gorm"
)

const (
	outboxEventChannelCharge = "channel_charge_report"
	outboxEventChannelRefund = "channel_refund_report"
)

type Service struct {
	platform    *platform.Client
	repo        *repository.CommercialRepository
	productCode string
}

type ChannelChargeInput struct {
	PolicyVersionID           string `json:"policy_version_id,omitempty"`
	RegionCode                string `json:"region_code,omitempty"`
	PartnerTier               string `json:"partner_tier,omitempty"`
	SourceOrderID             string `json:"source_order_id,omitempty"`
	AppliesTo                 string `json:"applies_to,omitempty"`
	PaymentFeeAmount          int64  `json:"payment_fee_amount,omitempty"`
	TaxAmount                 int64  `json:"tax_amount,omitempty"`
	ServiceDeliveryCostAmount int64  `json:"service_delivery_cost_amount,omitempty"`
	InfraVariableCostAmount   int64  `json:"infra_variable_cost_amount,omitempty"`
	RiskReserveAmount         int64  `json:"risk_reserve_amount,omitempty"`
	ManualAdjustmentAmount    int64  `json:"manual_adjustment_amount,omitempty"`
	CommissionRecognitionAt   string `json:"commission_recognition_at,omitempty"`
	SnapshotBasis             string `json:"snapshot_basis,omitempty"`
	Dimensions                string `json:"dimensions,omitempty"`
	Metadata                  string `json:"metadata,omitempty"`
}

type RecordChargeInput struct {
	OrganizationID   string              `json:"organization_id" binding:"required"`
	UserID           string              `json:"user_id,omitempty"`
	EventID          string              `json:"event_id" binding:"required"`
	BusinessType     string              `json:"business_type" binding:"required"`
	SceneCode        string              `json:"scene_code,omitempty"`
	SourceType       string              `json:"source_type,omitempty"`
	SourceID         string              `json:"source_id,omitempty"`
	BillableItemCode string              `json:"billable_item_code,omitempty"`
	ChargeMode       string              `json:"charge_mode,omitempty"`
	ChargeSessionID  string              `json:"charge_session_id,omitempty"`
	SettlementID     string              `json:"settlement_id,omitempty"`
	Currency         string              `json:"currency,omitempty"`
	GrossAmount      int64               `json:"gross_amount,omitempty"`
	DiscountAmount   int64               `json:"discount_amount,omitempty"`
	NetAmount        int64               `json:"net_amount,omitempty"`
	QuotaConsumed    int64               `json:"quota_consumed,omitempty"`
	CreditsConsumed  int64               `json:"credits_consumed,omitempty"`
	WalletAssetCode  string              `json:"wallet_asset_code,omitempty"`
	WalletDebited    int64               `json:"wallet_debited,omitempty"`
	BillingAmount    int64               `json:"billing_amount,omitempty"`
	RewardAmount     int64               `json:"reward_amount,omitempty"`
	CommissionAmount int64               `json:"commission_amount,omitempty"`
	Status           string              `json:"status,omitempty"`
	OccurredAt       string              `json:"occurred_at,omitempty"`
	RouteSnapshot    string              `json:"route_snapshot,omitempty"`
	Metadata         string              `json:"metadata,omitempty"`
	ChannelCharge    *ChannelChargeInput `json:"channel_charge,omitempty"`
}

type RefundChargeInput struct {
	RefundEventID string `json:"refund_event_id" binding:"required"`
	RefundAmount  int64  `json:"refund_amount" binding:"required"`
	RefundType    string `json:"refund_type" binding:"required"`
	OccurredAt    string `json:"occurred_at,omitempty"`
	ReasonCode    string `json:"reason_code,omitempty"`
	Metadata      string `json:"metadata,omitempty"`
}

type ReplayOutboxInput struct {
	Limit int `json:"limit"`
}

type ReplayOutboxResult struct {
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type BillingSummary struct {
	ChargeCount          int   `json:"charge_count"`
	SettledCount         int   `json:"settled_count"`
	RefundedCount        int   `json:"refunded_count"`
	TotalNetAmount       int64 `json:"total_net_amount"`
	TotalWalletDebited   int64 `json:"total_wallet_debited"`
	TotalCreditsConsumed int64 `json:"total_credits_consumed"`
	ChannelPendingCount  int   `json:"channel_pending_count"`
	ChannelFailedCount   int   `json:"channel_failed_count"`
}

func NewService(platformClient *platform.Client, repo *repository.CommercialRepository, appCfg config.AppConfig) *Service {
	return &Service{
		platform:    platformClient,
		repo:        repo,
		productCode: defaultString(appCfg.ProductCode, "ecommerce"),
	}
}

func (s *Service) ListCharges(orgID string, limit, offset int) ([]models.BillingChargeRecord, error) {
	return s.repo.ListBillingChargeRecords(orgID, limit, offset)
}

func (s *Service) Summary(orgID string) (*BillingSummary, error) {
	items, err := s.repo.ListBillingChargeRecords(orgID, 0, 0)
	if err != nil {
		return nil, err
	}
	out := &BillingSummary{ChargeCount: len(items)}
	for _, item := range items {
		out.TotalNetAmount += item.NetAmount
		out.TotalWalletDebited += item.WalletDebited
		out.TotalCreditsConsumed += item.CreditsConsumed
		switch item.Status {
		case "refunded":
			out.RefundedCount++
		default:
			out.SettledCount++
		}
		switch item.ChannelStatus {
		case "pending":
			out.ChannelPendingCount++
		case "failed":
			out.ChannelFailedCount++
		}
	}
	return out, nil
}

func (s *Service) RecordCharge(input RecordChargeInput) (*models.BillingChargeRecord, error) {
	existing, err := s.repo.GetBillingChargeByEventID(input.EventID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	occurredAt := parseTimeOrNow(input.OccurredAt)
	record := &models.BillingChargeRecord{
		ProductCode:      s.productCode,
		OrganizationID:   input.OrganizationID,
		UserID:           input.UserID,
		EventID:          input.EventID,
		BusinessType:     input.BusinessType,
		SceneCode:        input.SceneCode,
		SourceType:       input.SourceType,
		SourceID:         input.SourceID,
		BillableItemCode: input.BillableItemCode,
		ChargeMode:       input.ChargeMode,
		ChargeSessionID:  input.ChargeSessionID,
		SettlementID:     input.SettlementID,
		Currency:         input.Currency,
		GrossAmount:      input.GrossAmount,
		DiscountAmount:   input.DiscountAmount,
		NetAmount:        input.NetAmount,
		QuotaConsumed:    input.QuotaConsumed,
		CreditsConsumed:  input.CreditsConsumed,
		WalletAssetCode:  input.WalletAssetCode,
		WalletDebited:    input.WalletDebited,
		BillingAmount:    input.BillingAmount,
		RewardAmount:     input.RewardAmount,
		CommissionAmount: input.CommissionAmount,
		Status:           defaultString(input.Status, "settled"),
		OccurredAt:       occurredAt,
		RouteSnapshot:    input.RouteSnapshot,
		MetadataJSON:     input.Metadata,
		ChannelStatus:    "skipped",
	}
	if input.ChannelCharge != nil {
		record.ChannelStatus = "pending"
	}
	if err := s.repo.CreateBillingChargeRecord(record); err != nil {
		return nil, err
	}
	if input.ChannelCharge != nil {
		s.syncChannelCharge(record, input.ChannelCharge)
	}
	return record, nil
}

func (s *Service) RefundCharge(recordID string, input RefundChargeInput) (*models.BillingChargeRecord, error) {
	record, err := s.repo.GetBillingChargeRecord(recordID)
	if err != nil {
		return nil, err
	}
	refundAt := parseTimeOrNow(input.OccurredAt)
	record.Status = "refunded"
	record.RefundedAt = &refundAt
	record.UpdatedAt = time.Now()
	if err := s.repo.UpdateBillingChargeRecord(record); err != nil {
		return nil, err
	}
	s.syncChannelRefund(record, input)
	return record, nil
}

func (s *Service) ReplayOutbox(limit int) (*ReplayOutboxResult, error) {
	items, err := s.repo.ListReplayableOutbox(limit, time.Now())
	if err != nil {
		return nil, err
	}
	result := &ReplayOutboxResult{Processed: len(items)}
	for i := range items {
		err := s.replayOutboxEvent(&items[i])
		if err != nil {
			result.Failed++
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

func (s *Service) syncChannelCharge(record *models.BillingChargeRecord, input *ChannelChargeInput) {
	payload := channelChargePayload{
		RecordID: record.ID,
		Input: platform.RecordChannelChargeInput{
			EventID:                   record.EventID,
			ProductCode:               s.productCode,
			OrgID:                     record.OrganizationID,
			UserID:                    record.UserID,
			PolicyVersionID:           input.PolicyVersionID,
			RegionCode:                input.RegionCode,
			PartnerTier:               input.PartnerTier,
			BillableItemCode:          record.BillableItemCode,
			AppliesTo:                 defaultString(input.AppliesTo, record.BusinessType),
			SourceChargeID:            record.ID,
			SourceOrderID:             input.SourceOrderID,
			Currency:                  record.Currency,
			GrossAmount:               record.GrossAmount,
			DiscountAmount:            record.DiscountAmount,
			PaidAmount:                record.NetAmount,
			RefundedAmount:            0,
			NetCollectedAmount:        record.NetAmount,
			PaymentFeeAmount:          input.PaymentFeeAmount,
			TaxAmount:                 input.TaxAmount,
			ServiceDeliveryCostAmount: input.ServiceDeliveryCostAmount,
			InfraVariableCostAmount:   input.InfraVariableCostAmount,
			RiskReserveAmount:         input.RiskReserveAmount,
			ManualAdjustmentAmount:    input.ManualAdjustmentAmount,
			OccurredAt:                record.OccurredAt.UTC().Format(time.RFC3339),
			CommissionRecognitionAt:   input.CommissionRecognitionAt,
			SnapshotBasis:             input.SnapshotBasis,
			Dimensions:                input.Dimensions,
			Metadata:                  input.Metadata,
		},
	}
	if err := s.tryChannelCharge(record, payload.Input); err != nil {
		record.ChannelStatus = "failed"
		record.ChannelError = err.Error()
		_ = s.repo.UpdateBillingChargeRecord(record)
		s.enqueueOutbox(record, outboxEventChannelCharge, payload)
	}
}

func (s *Service) syncChannelRefund(record *models.BillingChargeRecord, input RefundChargeInput) {
	payload := channelRefundPayload{
		RecordID: record.ID,
		Input: platform.RecordChannelRefundInput{
			EventID:        input.RefundEventID,
			ProductCode:    s.productCode,
			OrgID:          record.OrganizationID,
			SourceChargeID: record.ID,
			SourceRefundID: input.RefundEventID,
			RefundAmount:   input.RefundAmount,
			RefundType:     input.RefundType,
			OccurredAt:     input.OccurredAt,
			ReasonCode:     input.ReasonCode,
			Metadata:       input.Metadata,
		},
	}
	if err := s.tryChannelRefund(record, payload.Input); err != nil {
		record.ChannelStatus = "failed"
		record.ChannelError = err.Error()
		_ = s.repo.UpdateBillingChargeRecord(record)
		s.enqueueOutbox(record, outboxEventChannelRefund, payload)
	}
}

func (s *Service) tryChannelCharge(record *models.BillingChargeRecord, input platform.RecordChannelChargeInput) error {
	result, err := s.platform.RecordChannelCharge(input)
	if err != nil {
		return err
	}
	record.ChannelStatus = "succeeded"
	record.ChannelError = ""
	if result != nil && result.Ledger != nil {
		record.ChannelLedgerID = result.Ledger.ID
	}
	return s.repo.UpdateBillingChargeRecord(record)
}

func (s *Service) tryChannelRefund(record *models.BillingChargeRecord, input platform.RecordChannelRefundInput) error {
	result, err := s.platform.RecordChannelRefund(input)
	if err != nil {
		return err
	}
	record.ChannelStatus = "succeeded"
	record.ChannelError = ""
	if result != nil && result.Ledger != nil {
		record.ChannelLedgerID = result.Ledger.ID
	}
	return s.repo.UpdateBillingChargeRecord(record)
}

func (s *Service) enqueueOutbox(record *models.BillingChargeRecord, eventType string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		logger.With("record_id", record.ID, "event_type", eventType, "error", err).Error("billing.outbox_payload_encode_failed")
		return
	}
	item := &models.CommercialEventOutbox{
		ProductCode:    s.productCode,
		OrganizationID: record.OrganizationID,
		UserID:         record.UserID,
		EventType:      eventType,
		AggregateType:  "billing_charge_record",
		AggregateID:    record.ID,
		Status:         "pending",
		PayloadJSON:    string(body),
		AvailableAt:    time.Now(),
	}
	if err := s.repo.CreateOutboxEvent(item); err != nil {
		logger.With("record_id", record.ID, "event_type", eventType, "error", err).Error("billing.outbox_create_failed")
	}
}

func (s *Service) replayOutboxEvent(item *models.CommercialEventOutbox) error {
	item.AttemptCount++
	item.Status = "processing"
	if err := s.repo.UpdateOutboxEvent(item); err != nil {
		return err
	}
	record, err := s.repo.GetBillingChargeRecord(item.AggregateID)
	if err != nil {
		item.Status = "failed"
		item.LastError = err.Error()
		_ = s.repo.UpdateOutboxEvent(item)
		return err
	}
	switch item.EventType {
	case outboxEventChannelCharge:
		var payload channelChargePayload
		if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
			return s.failOutbox(item, err)
		}
		if err := s.tryChannelCharge(record, payload.Input); err != nil {
			return s.failOutbox(item, err)
		}
	case outboxEventChannelRefund:
		var payload channelRefundPayload
		if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
			return s.failOutbox(item, err)
		}
		if err := s.tryChannelRefund(record, payload.Input); err != nil {
			return s.failOutbox(item, err)
		}
	default:
		return s.failOutbox(item, fmt.Errorf("unknown outbox event type: %s", item.EventType))
	}
	now := time.Now()
	item.Status = "succeeded"
	item.LastError = ""
	item.ProcessedAt = &now
	return s.repo.UpdateOutboxEvent(item)
}

func (s *Service) failOutbox(item *models.CommercialEventOutbox, err error) error {
	item.Status = "failed"
	item.LastError = err.Error()
	item.AvailableAt = time.Now().Add(1 * time.Minute)
	_ = s.repo.UpdateOutboxEvent(item)
	return err
}

type channelChargePayload struct {
	RecordID string                            `json:"record_id"`
	Input    platform.RecordChannelChargeInput `json:"input"`
}

type channelRefundPayload struct {
	RecordID string                            `json:"record_id"`
	Input    platform.RecordChannelRefundInput `json:"input"`
}

func parseTimeOrNow(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Now()
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	return time.Now()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
