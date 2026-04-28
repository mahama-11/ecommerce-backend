package models

import "time"

type PromotionAttributionAttempt struct {
	ID                   string    `gorm:"type:varchar(64);primaryKey"`
	ProductCode          string    `gorm:"type:varchar(64);index:idx_promotion_attempt_scope_created,priority:1;not null"`
	OrganizationID       string    `gorm:"type:varchar(64);index:idx_promotion_attempt_scope_created,priority:2;not null"`
	UserID               string    `gorm:"type:varchar(64);index:idx_promotion_attempt_scope_created,priority:3"`
	PromotionCode        string    `gorm:"type:varchar(128);not null"`
	TriggerType          string    `gorm:"type:varchar(64);not null"`
	ReferenceType        string    `gorm:"type:varchar(64);not null"`
	ReferenceID          string    `gorm:"type:varchar(128);not null"`
	Status               string    `gorm:"type:varchar(32);index;not null"`
	PlatformConversionID string    `gorm:"type:varchar(64)"`
	ErrorCode            string    `gorm:"type:varchar(64)"`
	ErrorMessage         string    `gorm:"type:text"`
	MetadataJSON         string    `gorm:"type:text"`
	CreatedAt            time.Time `gorm:"index:idx_promotion_attempt_scope_created,priority:4,sort:desc;autoCreateTime"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime"`
}

type BillingChargeRecord struct {
	ID               string    `gorm:"type:varchar(64);primaryKey"`
	ProductCode      string    `gorm:"type:varchar(64);index:idx_billing_charge_scope_occurred,priority:1;not null"`
	OrganizationID   string    `gorm:"type:varchar(64);index:idx_billing_charge_scope_occurred,priority:2;not null"`
	UserID           string    `gorm:"type:varchar(64);index"`
	EventID          string    `gorm:"type:varchar(128);uniqueIndex;not null"`
	BusinessType     string    `gorm:"type:varchar(64);index;not null"`
	SceneCode        string    `gorm:"type:varchar(64);index"`
	SourceType       string    `gorm:"type:varchar(64);index"`
	SourceID         string    `gorm:"type:varchar(128);index"`
	BillableItemCode string    `gorm:"type:varchar(128)"`
	ChargeMode       string    `gorm:"type:varchar(64)"`
	ChargeSessionID  string    `gorm:"type:varchar(128)"`
	SettlementID     string    `gorm:"type:varchar(128)"`
	Currency         string    `gorm:"type:varchar(32)"`
	GrossAmount      int64     `gorm:"not null;default:0"`
	DiscountAmount   int64     `gorm:"not null;default:0"`
	NetAmount        int64     `gorm:"not null;default:0"`
	QuotaConsumed    int64     `gorm:"not null;default:0"`
	CreditsConsumed  int64     `gorm:"not null;default:0"`
	WalletAssetCode  string    `gorm:"type:varchar(128)"`
	WalletDebited    int64     `gorm:"not null;default:0"`
	BillingAmount    int64     `gorm:"not null;default:0"`
	RewardAmount     int64     `gorm:"not null;default:0"`
	CommissionAmount int64     `gorm:"not null;default:0"`
	Status           string    `gorm:"type:varchar(32);index;not null"`
	OccurredAt       time.Time `gorm:"index:idx_billing_charge_scope_occurred,priority:3,sort:desc;not null"`
	RefundedAt       *time.Time
	RouteSnapshot    string    `gorm:"type:text"`
	MetadataJSON     string    `gorm:"type:text"`
	ChannelStatus    string    `gorm:"type:varchar(32);index;not null;default:skipped"`
	ChannelLedgerID  string    `gorm:"type:varchar(128)"`
	ChannelError     string    `gorm:"type:text"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime"`
}

type CommercialEventOutbox struct {
	ID             string    `gorm:"type:varchar(64);primaryKey"`
	ProductCode    string    `gorm:"type:varchar(64);index:idx_commercial_outbox_status_available,priority:1;not null"`
	OrganizationID string    `gorm:"type:varchar(64);index"`
	UserID         string    `gorm:"type:varchar(64);index"`
	EventType      string    `gorm:"type:varchar(64);index;not null"`
	AggregateType  string    `gorm:"type:varchar(64);not null"`
	AggregateID    string    `gorm:"type:varchar(64);index;not null"`
	Status         string    `gorm:"type:varchar(32);index:idx_commercial_outbox_status_available,priority:2;not null"`
	PayloadJSON    string    `gorm:"type:text;not null"`
	AttemptCount   int       `gorm:"not null;default:0"`
	LastError      string    `gorm:"type:text"`
	AvailableAt    time.Time `gorm:"index:idx_commercial_outbox_status_available,priority:3;not null"`
	ProcessedAt    *time.Time
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

type CommercialOrder struct {
	ID                string     `gorm:"type:varchar(64);primaryKey" json:"id"`
	UserID            string     `gorm:"type:varchar(64);index:idx_commercial_order_org_created,priority:2;not null" json:"user_id"`
	OrganizationID    string     `gorm:"type:varchar(64);index:idx_commercial_order_org_created,priority:1;not null" json:"organization_id"`
	ProductCode       string     `gorm:"type:varchar(64);not null;default:ecommerce" json:"product_code"`
	SKUCode           string     `gorm:"type:varchar(128);index;not null" json:"sku_code"`
	PackageCode       string     `gorm:"type:varchar(128);index;not null" json:"package_code"`
	PackageType       string     `gorm:"type:varchar(64);not null" json:"package_type"`
	Currency          string     `gorm:"type:varchar(32);not null" json:"currency"`
	Quantity          int64      `gorm:"not null;default:1" json:"quantity"`
	UnitAmount        int64      `gorm:"not null;default:0" json:"unit_amount"`
	TotalAmount       int64      `gorm:"not null;default:0" json:"total_amount"`
	Status            string     `gorm:"type:varchar(32);index;not null;default:pending_payment" json:"status"`
	PaymentStatus     string     `gorm:"type:varchar(32);not null;default:pending" json:"payment_status"`
	FulfillmentStatus string     `gorm:"type:varchar(32);not null;default:pending" json:"fulfillment_status"`
	MetadataJSON      string     `gorm:"type:text" json:"metadata_json"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	FulfilledAt       *time.Time `json:"fulfilled_at,omitempty"`
	CreatedAt         time.Time  `gorm:"index:idx_commercial_order_org_created,priority:3,sort:desc;autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

type CommercialPayment struct {
	ID                string     `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrderID           string     `gorm:"type:varchar(64);index;not null" json:"order_id"`
	UserID            string     `gorm:"type:varchar(64);not null" json:"user_id"`
	OrganizationID    string     `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Amount            int64      `gorm:"not null;default:0" json:"amount"`
	Currency          string     `gorm:"type:varchar(32);not null" json:"currency"`
	PaymentMethod     string     `gorm:"type:varchar(32);not null;default:wallet_balance" json:"payment_method"`
	ProviderCode      string     `gorm:"type:varchar(64);not null;default:platform_wallet" json:"provider_code"`
	ExternalPaymentID string     `gorm:"type:varchar(128)" json:"external_payment_id"`
	Status            string     `gorm:"type:varchar(32);index;not null;default:succeeded" json:"status"`
	MetadataJSON      string     `gorm:"type:text" json:"metadata_json"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

type CommercialFulfillment struct {
	ID                string     `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrderID           string     `gorm:"type:varchar(64);index;not null" json:"order_id"`
	UserID            string     `gorm:"type:varchar(64);not null" json:"user_id"`
	OrganizationID    string     `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PackageCode       string     `gorm:"type:varchar(128);not null" json:"package_code"`
	FulfillmentMode   string     `gorm:"type:varchar(32);not null" json:"fulfillment_mode"`
	Status            string     `gorm:"type:varchar(32);index;not null;default:succeeded" json:"status"`
	AssetCode         string     `gorm:"type:varchar(64)" json:"asset_code"`
	Amount            int64      `gorm:"not null;default:0" json:"amount"`
	AllowancePolicyID string     `gorm:"type:varchar(64)" json:"allowance_policy_id"`
	CycleKey          string     `gorm:"type:varchar(32)" json:"cycle_key"`
	WalletAccountID   string     `gorm:"type:varchar(64)" json:"wallet_account_id"`
	WalletBucketID    string     `gorm:"type:varchar(64)" json:"wallet_bucket_id"`
	WalletLedgerID    string     `gorm:"type:varchar(64)" json:"wallet_ledger_id"`
	MetadataJSON      string     `gorm:"type:text" json:"metadata_json"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	FulfilledAt       *time.Time `json:"fulfilled_at,omitempty"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}
