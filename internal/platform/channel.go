package platform

import "time"

type ChannelPartner struct {
	ID                    string    `json:"id"`
	Code                  string    `json:"code"`
	Name                  string    `json:"name"`
	PartnerType           string    `json:"partner_type"`
	SettlementSubjectType string    `json:"settlement_subject_type"`
	SettlementSubjectID   string    `json:"settlement_subject_id"`
	Status                string    `json:"status"`
	RiskLevel             string    `json:"risk_level"`
	CountryCode           string    `json:"country_code"`
	DefaultCurrency       string    `json:"default_currency"`
	ContactProfile        string    `json:"contact_profile"`
	Metadata              string    `json:"metadata"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ChannelProgram struct {
	ID                     string    `json:"id"`
	ProductCode            string    `json:"product_code"`
	ProgramCode            string    `json:"program_code"`
	Name                   string    `json:"name"`
	ProgramType            string    `json:"program_type"`
	Status                 string    `json:"status"`
	DefaultSettlementCycle string    `json:"default_settlement_cycle"`
	DefaultCooldownDays    int       `json:"default_cooldown_days"`
	DefaultHoldbackRateBps int64     `json:"default_holdback_rate_bps"`
	Metadata               string    `json:"metadata"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type ChannelBinding struct {
	ID                  string     `json:"id"`
	ProductCode         string     `json:"product_code"`
	OrgID               string     `json:"org_id"`
	ChannelPartnerID    string     `json:"channel_partner_id"`
	ChannelProgramID    string     `json:"channel_program_id"`
	BindingSource       string     `json:"binding_source"`
	SourceCode          string     `json:"source_code"`
	SourceRefID         string     `json:"source_ref_id"`
	BindingScope        string     `json:"binding_scope"`
	Status              string     `json:"status"`
	EffectiveFrom       *time.Time `json:"effective_from,omitempty"`
	EffectiveTo         *time.Time `json:"effective_to,omitempty"`
	LockedUntil         *time.Time `json:"locked_until,omitempty"`
	ReplacedByBindingID string     `json:"replaced_by_binding_id"`
	ReasonCode          string     `json:"reason_code"`
	Evidence            string     `json:"evidence"`
	CreatedBy           string     `json:"created_by"`
	Metadata            string     `json:"metadata"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ChannelCommissionLedger struct {
	ID                     string     `json:"id"`
	LedgerNo               string     `json:"ledger_no"`
	ProductCode            string     `json:"product_code"`
	ChannelPartnerID       string     `json:"channel_partner_id"`
	ChannelProgramID       string     `json:"channel_program_id"`
	BindingID              string     `json:"binding_id"`
	PolicyID               string     `json:"policy_id"`
	PolicyVersionID        string     `json:"policy_version_id"`
	ProfitSnapshotID       string     `json:"profit_snapshot_id"`
	AssignmentLevel        string     `json:"assignment_level"`
	MatchedRuleCode        string     `json:"matched_rule_code"`
	CalculationFormulaCode string     `json:"calculation_formula_code"`
	RoundingMode           string     `json:"rounding_mode"`
	CalculationTraceID     string     `json:"calculation_trace_id"`
	SettlementSubjectType  string     `json:"settlement_subject_type"`
	SettlementSubjectID    string     `json:"settlement_subject_id"`
	SourceEventID          string     `json:"source_event_id"`
	SourceChargeID         string     `json:"source_charge_id"`
	SourceOrderID          string     `json:"source_order_id"`
	BillableItemCode       string     `json:"billable_item_code"`
	AppliesTo              string     `json:"applies_to"`
	Currency               string     `json:"currency"`
	GrossAmount            int64      `json:"gross_amount"`
	DiscountAmount         int64      `json:"discount_amount"`
	PaidAmount             int64      `json:"paid_amount"`
	RefundedAmount         int64      `json:"refunded_amount"`
	NetCollectedAmount     int64      `json:"net_collected_amount"`
	CommissionableAmount   int64      `json:"commissionable_amount"`
	CommissionRateBps      int64      `json:"commission_rate_bps"`
	CommissionAmount       int64      `json:"commission_amount"`
	HoldbackAmount         int64      `json:"holdback_amount"`
	SettleableAmount       int64      `json:"settleable_amount"`
	Status                 string     `json:"status"`
	AvailableAt            *time.Time `json:"available_at,omitempty"`
	EarnedAt               *time.Time `json:"earned_at,omitempty"`
	SettledAt              *time.Time `json:"settled_at,omitempty"`
	ReversedAt             *time.Time `json:"reversed_at,omitempty"`
	ReversalEventID        *string    `json:"reversal_event_id,omitempty"`
	ReversalReasonCode     string     `json:"reversal_reason_code"`
	Dimensions             string     `json:"dimensions"`
	Metadata               string     `json:"metadata"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type ChannelSettlementBatch struct {
	ID                    string     `json:"id"`
	BatchNo               string     `json:"batch_no"`
	ProductCode           string     `json:"product_code"`
	ChannelProgramID      string     `json:"channel_program_id"`
	SettlementCycle       string     `json:"settlement_cycle"`
	PeriodStart           time.Time  `json:"period_start"`
	PeriodEnd             time.Time  `json:"period_end"`
	Currency              string     `json:"currency"`
	Status                string     `json:"status"`
	TotalPartnerCount     int64      `json:"total_partner_count"`
	TotalItemCount        int64      `json:"total_item_count"`
	GrossCommissionAmount int64      `json:"gross_commission_amount"`
	GrossClawbackAmount   int64      `json:"gross_clawback_amount"`
	NetSettleableAmount   int64      `json:"net_settleable_amount"`
	GeneratedAt           *time.Time `json:"generated_at,omitempty"`
	ConfirmedAt           *time.Time `json:"confirmed_at,omitempty"`
	ClosedAt              *time.Time `json:"closed_at,omitempty"`
	Metadata              string     `json:"metadata"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type ChannelSettlementItem struct {
	ID                string    `json:"id"`
	SettlementBatchID string    `json:"settlement_batch_id"`
	ChannelPartnerID  string    `json:"channel_partner_id"`
	Currency          string    `json:"currency"`
	CommissionAmount  int64     `json:"commission_amount"`
	ClawbackAmount    int64     `json:"clawback_amount"`
	AdjustmentAmount  int64     `json:"adjustment_amount"`
	NetAmount         int64     `json:"net_amount"`
	Status            string    `json:"status"`
	StatementSnapshot string    `json:"statement_snapshot"`
	Metadata          string    `json:"metadata"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ChannelSettlementItemDetail struct {
	Item                ChannelSettlementItem `json:"item"`
	CommissionLedgerIDs []string              `json:"commission_ledger_ids"`
	ClawbackLedgerIDs   []string              `json:"clawback_ledger_ids"`
	AdjustmentLedgerIDs []string              `json:"adjustment_ledger_ids"`
}

type ChannelSettlementBatchDetail struct {
	Batch ChannelSettlementBatch        `json:"batch"`
	Items []ChannelSettlementItemDetail `json:"items"`
}

type RecordChannelChargeInput struct {
	EventID                   string `json:"event_id"`
	ProductCode               string `json:"product_code"`
	OrgID                     string `json:"org_id"`
	UserID                    string `json:"user_id,omitempty"`
	PolicyVersionID           string `json:"policy_version_id,omitempty"`
	RegionCode                string `json:"region_code,omitempty"`
	PartnerTier               string `json:"partner_tier,omitempty"`
	BillableItemCode          string `json:"billable_item_code,omitempty"`
	AppliesTo                 string `json:"applies_to"`
	SourceChargeID            string `json:"source_charge_id"`
	SourceOrderID             string `json:"source_order_id,omitempty"`
	Currency                  string `json:"currency,omitempty"`
	GrossAmount               int64  `json:"gross_amount"`
	DiscountAmount            int64  `json:"discount_amount"`
	PaidAmount                int64  `json:"paid_amount"`
	RefundedAmount            int64  `json:"refunded_amount"`
	NetCollectedAmount        int64  `json:"net_collected_amount"`
	PaymentFeeAmount          int64  `json:"payment_fee_amount"`
	TaxAmount                 int64  `json:"tax_amount"`
	ServiceDeliveryCostAmount int64  `json:"service_delivery_cost_amount"`
	InfraVariableCostAmount   int64  `json:"infra_variable_cost_amount"`
	RiskReserveAmount         int64  `json:"risk_reserve_amount"`
	ManualAdjustmentAmount    int64  `json:"manual_adjustment_amount"`
	OccurredAt                string `json:"occurred_at,omitempty"`
	CommissionRecognitionAt   string `json:"commission_recognition_at,omitempty"`
	SnapshotBasis             string `json:"snapshot_basis,omitempty"`
	Dimensions                string `json:"dimensions,omitempty"`
	Metadata                  string `json:"metadata,omitempty"`
}

type RecordChannelRefundInput struct {
	EventID        string `json:"event_id"`
	ProductCode    string `json:"product_code"`
	OrgID          string `json:"org_id,omitempty"`
	SourceChargeID string `json:"source_charge_id"`
	SourceRefundID string `json:"source_refund_id,omitempty"`
	RefundAmount   int64  `json:"refund_amount"`
	RefundType     string `json:"refund_type"`
	OccurredAt     string `json:"occurred_at,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	Metadata       string `json:"metadata,omitempty"`
}

type RecordChannelChargeResult struct {
	Matched    bool                     `json:"matched"`
	Idempotent bool                     `json:"idempotent"`
	Status     string                   `json:"status"`
	Ledger     *ChannelCommissionLedger `json:"ledger,omitempty"`
	BindingID  string                   `json:"binding_id,omitempty"`
	ChannelID  string                   `json:"channel_partner_id,omitempty"`
	PolicyID   string                   `json:"policy_id,omitempty"`
}

type RecordChannelRefundResult struct {
	Matched    bool                     `json:"matched"`
	Idempotent bool                     `json:"idempotent"`
	Action     string                   `json:"action"`
	Ledger     *ChannelCommissionLedger `json:"ledger,omitempty"`
}

func (c *Client) ListChannelPartners(status string) ([]ChannelPartner, error) {
	out, err := doInternalGet[platformItemsResponse[ChannelPartner]](c, withQuery("/incentives/channel-partners", map[string]string{
		"status": status,
	}))
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListChannelPrograms(productCode, status string) ([]ChannelProgram, error) {
	out, err := doInternalGet[platformItemsResponse[ChannelProgram]](c, withQuery("/incentives/channel-programs", map[string]string{
		"product_code": productCode,
		"status":       status,
	}))
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListChannelBindings(productCode, orgID, status string) ([]ChannelBinding, error) {
	out, err := doInternalGet[platformItemsResponse[ChannelBinding]](c, withQuery("/incentives/channel-bindings", map[string]string{
		"product_code": productCode,
		"org_id":       orgID,
		"status":       status,
	}))
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListChannelCommissions(productCode, channelPartnerID, status string) ([]ChannelCommissionLedger, error) {
	out, err := doInternalGet[platformItemsResponse[ChannelCommissionLedger]](c, withQuery("/incentives/channel-commissions", map[string]string{
		"product_code":       productCode,
		"channel_partner_id": channelPartnerID,
		"status":             status,
	}))
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListChannelSettlementBatches(productCode, channelProgramID, status string) ([]ChannelSettlementBatch, error) {
	out, err := doInternalGet[platformItemsResponse[ChannelSettlementBatch]](c, withQuery("/incentives/channel-settlement-batches", map[string]string{
		"product_code":       productCode,
		"channel_program_id": channelProgramID,
		"status":             status,
	}))
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) GetChannelSettlementBatch(batchID string) (*ChannelSettlementBatchDetail, error) {
	return doInternalGet[ChannelSettlementBatchDetail](c, "/incentives/channel-settlement-batches/"+batchID)
}

func (c *Client) RecordChannelCharge(input RecordChannelChargeInput) (*RecordChannelChargeResult, error) {
	return doInternalPost[RecordChannelChargeInput, RecordChannelChargeResult](c, "/incentives/channel-events/charges", input)
}

func (c *Client) RecordChannelRefund(input RecordChannelRefundInput) (*RecordChannelRefundResult, error) {
	return doInternalPost[RecordChannelRefundInput, RecordChannelRefundResult](c, "/incentives/channel-events/refunds", input)
}
