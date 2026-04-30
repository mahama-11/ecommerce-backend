package wallet

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
)

type Service struct {
	platform    *platform.Client
	repo        *repository.CommercialRepository
	productCode string
}

type Summary struct {
	platform.WalletSummary
	PrimaryAssetCode string        `json:"primary_asset_code"`
	Quota            *QuotaSummary `json:"quota,omitempty"`
}

type QuotaSummary struct {
	BillableItemCode string `json:"billable_item_code"`
	Granted          int64  `json:"granted"`
	Consumed         int64  `json:"consumed"`
	Reserved         int64  `json:"reserved"`
	Remaining        int64  `json:"remaining"`
}

const ecommerceQuotaBillableItemCode = "ecommerce.image.generate"

type HistoryEntry struct {
	ID               string         `json:"id"`
	Category         string         `json:"category"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	Direction        string         `json:"direction"`
	Amount           int64          `json:"amount"`
	AssetCode        string         `json:"asset_code,omitempty"`
	Currency         string         `json:"currency,omitempty"`
	Status           string         `json:"status"`
	OccurredAt       string         `json:"occurred_at"`
	ReferenceType    string         `json:"reference_type,omitempty"`
	ReferenceID      string         `json:"reference_id,omitempty"`
	BillableItemCode string         `json:"billable_item_code,omitempty"`
	ChargeMode       string         `json:"charge_mode,omitempty"`
	QuotaConsumed    int64          `json:"quota_consumed,omitempty"`
	CreditsConsumed  int64          `json:"credits_consumed,omitempty"`
	WalletDebited    int64          `json:"wallet_debited,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type HistoryResult struct {
	Items []HistoryEntry `json:"items"`
}

func NewService(platformClient *platform.Client, repo *repository.CommercialRepository, appCfg config.AppConfig) *Service {
	return &Service{
		platform:    platformClient,
		repo:        repo,
		productCode: defaultString(appCfg.ProductCode, "ecommerce"),
	}
}

func (s *Service) Summary(orgID string) (*Summary, error) {
	item, err := s.platform.GetWalletSummary("organization", orgID, s.productCode)
	if err != nil {
		return nil, err
	}
	quota, quotaErr := s.platform.GetQuotaBalance("organization", orgID, ecommerceQuotaBillableItemCode)
	if quotaErr != nil {
		return nil, quotaErr
	}
	quotaSummary := &QuotaSummary{
		BillableItemCode: quota.BillableItemCode,
		Granted:          quota.Granted,
		Consumed:         quota.Consumed,
		Reserved:         quota.Reserved,
		Remaining:        quota.Available,
	}
	if item == nil {
		return &Summary{PrimaryAssetCode: s.productCode + "_CREDIT", Quota: quotaSummary}, nil
	}
	primaryAsset := ""
	if len(item.Assets) > 0 {
		primaryAsset = item.Assets[0].AssetCode
	}
	return &Summary{WalletSummary: *item, PrimaryAssetCode: primaryAsset, Quota: quotaSummary}, nil
}

func (s *Service) History(orgID string, limit int) (*HistoryResult, error) {
	entries := make([]HistoryEntry, 0)
	rewards, err := s.platform.ListRewards(s.productCode, "organization", orgID)
	if err != nil {
		return nil, err
	}
	for _, item := range rewards {
		entries = append(entries, mapReward(item))
	}
	commissions, err := s.platform.ListCommissions(s.productCode, "organization", orgID, "")
	if err != nil {
		return nil, err
	}
	for _, item := range commissions {
		entries = append(entries, mapCommission(item))
	}
	accounts, err := s.platform.ListWalletAccounts("organization", orgID, s.productCode)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		ledgers, ledgerErr := s.platform.ListWalletLedger(account.ID, s.productCode)
		if ledgerErr != nil {
			return nil, ledgerErr
		}
		for _, item := range ledgers {
			if entry, ok := mapLedger(item); ok {
				entries = append(entries, entry)
			}
		}
	}
	if s.repo != nil {
		charges, err := s.repo.ListBillingChargeRecords(orgID, limitOrDefault(limit), 0)
		if err != nil {
			return nil, err
		}
		for _, item := range charges {
			entries = append(entries, mapCharge(item))
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].OccurredAt > entries[j].OccurredAt })
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return &HistoryResult{Items: entries}, nil
}

func mapReward(item platform.RewardLedger) HistoryEntry {
	category := "reward"
	title := "Promotion credits issued"
	if item.RewardType == "commission_redeem" {
		category = "redeem"
		title = "Commission redeemed to credits"
	}
	return HistoryEntry{
		ID:            item.ID,
		Category:      category,
		Title:         title,
		Direction:     "credit",
		Amount:        item.Amount,
		AssetCode:     item.AssetCode,
		Status:        item.Status,
		OccurredAt:    item.CreatedAt.UTC().Format(time.RFC3339),
		ReferenceType: item.ReferenceType,
		ReferenceID:   item.ReferenceID,
		Metadata:      decodeMap(item.Metadata),
	}
}

func mapCommission(item platform.CommissionLedger) HistoryEntry {
	title := "Promotion commission earned"
	switch item.Status {
	case "redeemed":
		title = "Promotion commission redeemed"
	case "reversed":
		title = "Promotion commission reversed"
	}
	return HistoryEntry{
		ID:            item.ID,
		Category:      "commission",
		Title:         title,
		Direction:     "info",
		Amount:        item.Amount,
		Currency:      item.Currency,
		Status:        item.Status,
		OccurredAt:    item.CreatedAt.UTC().Format(time.RFC3339),
		ReferenceType: item.ReferenceType,
		ReferenceID:   item.ReferenceID,
		Metadata:      decodeMap(item.Metadata),
	}
}

func mapLedger(item platform.WalletLedger) (HistoryEntry, bool) {
	if item.AssetCode == "ECOMMERCE_MONTHLY_ALLOWANCE" {
		return HistoryEntry{}, false
	}
	switch item.Reason {
	case "reward_issue", "metering_settlement":
		return HistoryEntry{}, false
	case "asset_expire":
		return HistoryEntry{
			ID:            item.ID,
			Category:      "expiration",
			Title:         "Credits expired",
			Direction:     "debit",
			Amount:        item.Amount,
			AssetCode:     item.AssetCode,
			Status:        item.Status,
			OccurredAt:    item.CreatedAt.UTC().Format(time.RFC3339),
			ReferenceType: item.ReferenceType,
			ReferenceID:   item.ReferenceID,
			Metadata:      decodeMap(item.Metadata),
		}, true
	default:
		title := "Wallet adjustment"
		category := "wallet_adjustment"
		if item.Direction == "credit" {
			title = "Credits recharge"
			category = "recharge"
		}
		return HistoryEntry{
			ID:            item.ID,
			Category:      category,
			Title:         title,
			Direction:     normalizeDirection(item.Direction),
			Amount:        item.Amount,
			AssetCode:     item.AssetCode,
			Status:        item.Status,
			OccurredAt:    item.CreatedAt.UTC().Format(time.RFC3339),
			ReferenceType: item.ReferenceType,
			ReferenceID:   item.ReferenceID,
			Metadata:      decodeMap(item.Metadata),
		}, true
	}
}

func mapCharge(item models.BillingChargeRecord) HistoryEntry {
	title := "Product charge settled"
	category := "charge"
	if item.Status == "refunded" {
		title = "Product charge refunded"
		category = "refund"
	}
	metadata := decodeMap(item.MetadataJSON)
	quotaConsumed := resolveQuotaConsumed(item, metadata)
	return HistoryEntry{
		ID:               item.ID,
		Category:         category,
		Title:            title,
		Direction:        "debit",
		Amount:           resolveChargeAmount(item, quotaConsumed),
		AssetCode:        chargeAssetCode(item, quotaConsumed),
		Currency:         item.Currency,
		Status:           item.Status,
		OccurredAt:       item.OccurredAt.UTC().Format(time.RFC3339),
		ReferenceType:    item.SourceType,
		ReferenceID:      item.SourceID,
		BillableItemCode: item.BillableItemCode,
		ChargeMode:       item.ChargeMode,
		QuotaConsumed:    quotaConsumed,
		CreditsConsumed:  item.CreditsConsumed,
		WalletDebited:    item.WalletDebited,
		Metadata:         metadata,
	}
}

func chargeAssetCode(item models.BillingChargeRecord, quotaConsumed int64) string {
	if strings.TrimSpace(item.WalletAssetCode) != "" {
		return item.WalletAssetCode
	}
	if quotaConsumed > 0 {
		return firstNonEmpty(item.BillableItemCode, ecommerceQuotaBillableItemCode)
	}
	return item.WalletAssetCode
}

func resolveQuotaConsumed(item models.BillingChargeRecord, metadata map[string]any) int64 {
	if item.QuotaConsumed > 0 {
		return item.QuotaConsumed
	}
	if usageUnits, ok := metadata["usage_units"]; ok {
		switch typed := usageUnits.(type) {
		case float64:
			return int64(typed)
		case int:
			return int64(typed)
		case int64:
			return typed
		}
	}
	return 0
}

func resolveChargeAmount(item models.BillingChargeRecord, quotaConsumed int64) int64 {
	if quotaConsumed > 0 && item.NetAmount == 0 && item.WalletDebited == 0 {
		return quotaConsumed
	}
	return item.NetAmount
}

func decodeMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func normalizeDirection(value string) string {
	if strings.TrimSpace(value) == "credit" {
		return "credit"
	}
	return "debit"
}

func limitOrDefault(limit int) int {
	if limit > 0 {
		return limit
	}
	return 100
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
