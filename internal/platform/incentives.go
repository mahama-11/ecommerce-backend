package platform

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type platformItemsResponse[T any] struct {
	Items []T `json:"items"`
}

type WalletAccount struct {
	ID                 string    `json:"id"`
	BillingSubjectType string    `json:"billing_subject_type"`
	BillingSubjectID   string    `json:"billing_subject_id"`
	AssetCode          string    `json:"asset_code"`
	AssetType          string    `json:"asset_type"`
	Balance            int64     `json:"balance"`
	Status             string    `json:"status"`
	Metadata           string    `json:"metadata"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type WalletAssetSummary struct {
	AssetCode        string     `json:"asset_code"`
	AssetType        string     `json:"asset_type"`
	LifecycleType    string     `json:"lifecycle_type"`
	AccountBalance   int64      `json:"account_balance"`
	AvailableBalance int64      `json:"available_balance"`
	ExpiringBalance  int64      `json:"expiring_balance"`
	NextExpiresAt    *time.Time `json:"next_expires_at,omitempty"`
}

type AssetDefinition struct {
	AssetCode         string    `json:"asset_code"`
	ProductCode       string    `json:"product_code"`
	AssetType         string    `json:"asset_type"`
	LifecycleType     string    `json:"lifecycle_type"`
	DefaultExpireDays int       `json:"default_expire_days"`
	ResetCycle        string    `json:"reset_cycle"`
	Status            string    `json:"status"`
	Description       string    `json:"description"`
	Metadata          string    `json:"metadata"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type WalletLedger struct {
	ID                 string    `json:"id"`
	WalletAccountID    string    `json:"wallet_account_id"`
	BillingSubjectType string    `json:"billing_subject_type"`
	BillingSubjectID   string    `json:"billing_subject_id"`
	AssetCode          string    `json:"asset_code"`
	Direction          string    `json:"direction"`
	Amount             int64     `json:"amount"`
	Reason             string    `json:"reason"`
	ReferenceType      string    `json:"reference_type"`
	ReferenceID        string    `json:"reference_id"`
	Status             string    `json:"status"`
	Metadata           string    `json:"metadata"`
	CreatedAt          time.Time `json:"created_at"`
}

type RewardLedger struct {
	ID                     string    `json:"id"`
	ProductCode            string    `json:"product_code"`
	CampaignCode           string    `json:"campaign_code"`
	RewardType             string    `json:"reward_type"`
	BeneficiarySubjectType string    `json:"beneficiary_subject_type"`
	BeneficiarySubjectID   string    `json:"beneficiary_subject_id"`
	AssetCode              string    `json:"asset_code"`
	Amount                 int64     `json:"amount"`
	Status                 string    `json:"status"`
	ReferenceType          string    `json:"reference_type"`
	ReferenceID            string    `json:"reference_id"`
	Metadata               string    `json:"metadata"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type CommissionLedger struct {
	ID                     string     `json:"id"`
	ProductCode            string     `json:"product_code"`
	CommissionType         string     `json:"commission_type"`
	BeneficiarySubjectType string     `json:"beneficiary_subject_type"`
	BeneficiarySubjectID   string     `json:"beneficiary_subject_id"`
	SettlementSubjectType  string     `json:"settlement_subject_type"`
	SettlementSubjectID    string     `json:"settlement_subject_id"`
	Currency               string     `json:"currency"`
	Amount                 int64      `json:"amount"`
	Status                 string     `json:"status"`
	ReferenceType          string     `json:"reference_type"`
	ReferenceID            string     `json:"reference_id"`
	RedeemedRewardID       string     `json:"redeemed_reward_id"`
	RedeemedAt             *time.Time `json:"redeemed_at,omitempty"`
	Metadata               string     `json:"metadata"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type ReferralProgram struct {
	ID                    string     `json:"id"`
	ProductCode           string     `json:"product_code"`
	ProgramCode           string     `json:"program_code"`
	Name                  string     `json:"name"`
	Status                string     `json:"status"`
	TriggerType           string     `json:"trigger_type"`
	CommissionPolicy      string     `json:"commission_policy"`
	CommissionCurrency    string     `json:"commission_currency"`
	CommissionFixedAmount int64      `json:"commission_fixed_amount"`
	CommissionRateBps     int64      `json:"commission_rate_bps"`
	SettlementDelayDays   int        `json:"settlement_delay_days"`
	AllowRepeat           bool       `json:"allow_repeat"`
	EffectiveFrom         *time.Time `json:"effective_from,omitempty"`
	EffectiveTo           *time.Time `json:"effective_to,omitempty"`
	Metadata              string     `json:"metadata"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type ReferralCode struct {
	ID                  string    `json:"id"`
	ProgramID           string    `json:"program_id"`
	ProductCode         string    `json:"product_code"`
	Code                string    `json:"code"`
	PromoterSubjectType string    `json:"promoter_subject_type"`
	PromoterSubjectID   string    `json:"promoter_subject_id"`
	Status              string    `json:"status"`
	Metadata            string    `json:"metadata"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ReferralConversion struct {
	ID                    string    `json:"id"`
	ProgramID             string    `json:"program_id"`
	ReferralCodeID        string    `json:"referral_code_id"`
	ProductCode           string    `json:"product_code"`
	TriggerType           string    `json:"trigger_type"`
	PromoterSubjectType   string    `json:"promoter_subject_type"`
	PromoterSubjectID     string    `json:"promoter_subject_id"`
	ReferredSubjectType   string    `json:"referred_subject_type"`
	ReferredSubjectID     string    `json:"referred_subject_id"`
	SettlementSubjectType string    `json:"settlement_subject_type"`
	SettlementSubjectID   string    `json:"settlement_subject_id"`
	ReferenceType         string    `json:"reference_type"`
	ReferenceID           string    `json:"reference_id"`
	CommissionCurrency    string    `json:"commission_currency"`
	CommissionAmount      int64     `json:"commission_amount"`
	CommissionLedgerID    string    `json:"commission_ledger_id"`
	Status                string    `json:"status"`
	Metadata              string    `json:"metadata"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ResolvedReferralCode struct {
	Code                  string         `json:"code"`
	ProductCode           string         `json:"product_code"`
	ProgramID             string         `json:"program_id"`
	ProgramCode           string         `json:"program_code"`
	ProgramName           string         `json:"program_name"`
	TriggerType           string         `json:"trigger_type"`
	CommissionPolicy      string         `json:"commission_policy"`
	CommissionCurrency    string         `json:"commission_currency"`
	CommissionFixedAmount int64          `json:"commission_fixed_amount"`
	CommissionRateBps     int64          `json:"commission_rate_bps"`
	SettlementDelayDays   int            `json:"settlement_delay_days"`
	AllowRepeat           bool           `json:"allow_repeat"`
	RewardPolicyDesc      string         `json:"reward_policy_desc"`
	PromoterSubjectType   string         `json:"promoter_subject_type"`
	PromoterSubjectID     string         `json:"promoter_subject_id"`
	Status                string         `json:"status"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type CreateAssetDefinitionInput struct {
	AssetCode         string `json:"asset_code"`
	ProductCode       string `json:"product_code,omitempty"`
	AssetType         string `json:"asset_type"`
	LifecycleType     string `json:"lifecycle_type"`
	DefaultExpireDays int    `json:"default_expire_days,omitempty"`
	ResetCycle        string `json:"reset_cycle,omitempty"`
	Status            string `json:"status,omitempty"`
	Description       string `json:"description,omitempty"`
	Metadata          string `json:"metadata,omitempty"`
}

type CreateReferralProgramInput struct {
	ProductCode           string `json:"product_code"`
	ProgramCode           string `json:"program_code"`
	Name                  string `json:"name"`
	TriggerType           string `json:"trigger_type"`
	CommissionPolicy      string `json:"commission_policy"`
	CommissionCurrency    string `json:"commission_currency,omitempty"`
	CommissionFixedAmount int64  `json:"commission_fixed_amount,omitempty"`
	CommissionRateBps     int64  `json:"commission_rate_bps,omitempty"`
	SettlementDelayDays   int    `json:"settlement_delay_days,omitempty"`
	AllowRepeat           bool   `json:"allow_repeat"`
	Status                string `json:"status,omitempty"`
	Metadata              string `json:"metadata,omitempty"`
}

type CreateReferralCodeInput struct {
	ProgramCode         string `json:"program_code"`
	Code                string `json:"code,omitempty"`
	PromoterSubjectType string `json:"promoter_subject_type"`
	PromoterSubjectID   string `json:"promoter_subject_id"`
	Status              string `json:"status,omitempty"`
	Metadata            string `json:"metadata,omitempty"`
}

type CreateReferralConversionInput struct {
	ReferralCode          string `json:"referral_code"`
	ProductCode           string `json:"product_code"`
	TriggerType           string `json:"trigger_type"`
	ReferredSubjectType   string `json:"referred_subject_type"`
	ReferredSubjectID     string `json:"referred_subject_id"`
	SettlementSubjectType string `json:"settlement_subject_type,omitempty"`
	SettlementSubjectID   string `json:"settlement_subject_id,omitempty"`
	ReferenceType         string `json:"reference_type"`
	ReferenceID           string `json:"reference_id"`
	CommissionBaseAmount  int64  `json:"commission_base_amount,omitempty"`
	CommissionCurrency    string `json:"commission_currency,omitempty"`
	Metadata              string `json:"metadata,omitempty"`
}

type RedeemCommissionsInput struct {
	ProductCode            string   `json:"product_code"`
	BeneficiarySubjectType string   `json:"beneficiary_subject_type"`
	BeneficiarySubjectID   string   `json:"beneficiary_subject_id"`
	AssetCode              string   `json:"asset_code"`
	CommissionIDs          []string `json:"commission_ids,omitempty"`
	Metadata               string   `json:"metadata,omitempty"`
}

type RedeemCommissionsResult struct {
	RewardLedgerID string             `json:"reward_ledger_id"`
	AssetCode      string             `json:"asset_code"`
	TotalAmount    int64              `json:"total_amount"`
	Commissions    []CommissionLedger `json:"commissions"`
}

func (c *Client) ListWalletAccounts(subjectType, subjectID, productCode string) ([]WalletAccount, error) {
	path := withQuery("/wallet/accounts", map[string]string{
		"billing_subject_type": subjectType,
		"billing_subject_id":   subjectID,
		"product_code":         productCode,
	})
	out, err := doInternalGet[platformItemsResponse[WalletAccount]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListAssetDefinitions(productCode, lifecycleType, status string) ([]AssetDefinition, error) {
	path := withQuery("/wallet/assets", map[string]string{
		"product_code":   productCode,
		"lifecycle_type": lifecycleType,
		"status":         status,
	})
	out, err := doInternalGet[platformItemsResponse[AssetDefinition]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) CreateAssetDefinition(input CreateAssetDefinitionInput) (*AssetDefinition, error) {
	return doInternalPost[CreateAssetDefinitionInput, AssetDefinition](c, "/wallet/assets", input)
}

func (c *Client) ListWalletLedger(walletAccountID, productCode string) ([]WalletLedger, error) {
	path := withQuery("/wallet/ledger", map[string]string{
		"wallet_account_id": walletAccountID,
		"product_code":      productCode,
	})
	out, err := doInternalGet[platformItemsResponse[WalletLedger]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListRewards(productCode, beneficiaryType, beneficiaryID string) ([]RewardLedger, error) {
	path := withQuery("/incentives/rewards", map[string]string{
		"product_code":             productCode,
		"beneficiary_subject_type": beneficiaryType,
		"beneficiary_subject_id":   beneficiaryID,
	})
	out, err := doInternalGet[platformItemsResponse[RewardLedger]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListCommissions(productCode, beneficiaryType, beneficiaryID, status string) ([]CommissionLedger, error) {
	path := withQuery("/incentives/commissions", map[string]string{
		"product_code":             productCode,
		"beneficiary_subject_type": beneficiaryType,
		"beneficiary_subject_id":   beneficiaryID,
		"status":                   status,
	})
	out, err := doInternalGet[platformItemsResponse[CommissionLedger]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) RedeemCommissions(input RedeemCommissionsInput) (*RedeemCommissionsResult, error) {
	return doInternalPost[RedeemCommissionsInput, RedeemCommissionsResult](c, "/incentives/commissions/redeem", input)
}

func (c *Client) ListReferralPrograms(productCode, status string) ([]ReferralProgram, error) {
	path := withQuery("/incentives/referral-programs", map[string]string{
		"product_code": productCode,
		"status":       status,
	})
	out, err := doInternalGet[platformItemsResponse[ReferralProgram]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) CreateReferralProgram(input CreateReferralProgramInput) (*ReferralProgram, error) {
	return doInternalPost[CreateReferralProgramInput, ReferralProgram](c, "/incentives/referral-programs", input)
}

func (c *Client) ListReferralCodes(programID, promoterType, promoterID, status string) ([]ReferralCode, error) {
	path := withQuery("/incentives/referral-codes", map[string]string{
		"program_id":            programID,
		"promoter_subject_type": promoterType,
		"promoter_subject_id":   promoterID,
		"status":                status,
	})
	out, err := doInternalGet[platformItemsResponse[ReferralCode]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) CreateReferralCode(input CreateReferralCodeInput) (*ReferralCode, error) {
	return doInternalPost[CreateReferralCodeInput, ReferralCode](c, "/incentives/referral-codes", input)
}

func (c *Client) ResolveReferralCode(code, productCode string) (*ResolvedReferralCode, error) {
	path := withQuery(fmt.Sprintf("/incentives/referral-codes/%s/resolve", url.PathEscape(strings.TrimSpace(code))), map[string]string{
		"product_code": productCode,
	})
	return doInternalGet[ResolvedReferralCode](c, path)
}

func (c *Client) ListReferralConversions(productCode, promoterType, promoterID, status string) ([]ReferralConversion, error) {
	path := withQuery("/incentives/referral-conversions", map[string]string{
		"product_code":          productCode,
		"promoter_subject_type": promoterType,
		"promoter_subject_id":   promoterID,
		"status":                status,
	})
	out, err := doInternalGet[platformItemsResponse[ReferralConversion]](c, path)
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) CreateReferralConversion(input CreateReferralConversionInput) (*ReferralConversion, error) {
	return doInternalPost[CreateReferralConversionInput, ReferralConversion](c, "/incentives/referral-conversions", input)
}
