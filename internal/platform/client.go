package platform

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ecommerce-service/internal/config"
)

type Client struct {
	baseURL     string
	secret      string
	serviceName string
	http        *http.Client
}

func New(cfg config.PlatformConfig) *Client {
	return &Client{baseURL: strings.TrimRight(cfg.BaseURL, "/"), secret: cfg.InternalServiceSecret, serviceName: defaultString(cfg.ServiceName, "v-ecommerce-backend"), http: &http.Client{Timeout: cfg.Timeout}}
}

func (c *Client) BaseURL() string { return c.baseURL }

type envelope[T any] struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	ErrorCode string `json:"error_code"`
	ErrorHint string `json:"error_hint"`
	RequestID string `json:"request_id"`
	Timestamp int64  `json:"timestamp"`
	Data      T      `json:"data"`
	Error     string `json:"error"`
}

type platformError struct {
	Status    int
	Code      int
	Message   string
	ErrorCode string
	ErrorHint string
	Err       string
}

func (e *platformError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("platform request failed: status=%d code=%d message=%s error_code=%s error=%s", e.Status, e.Code, e.Message, e.ErrorCode, e.Err)
}

func IsConflict(err error) bool {
	var pe *platformError
	return errors.As(err, &pe) && pe.Status == http.StatusConflict
}
func IsUnauthorized(err error) bool {
	var pe *platformError
	return errors.As(err, &pe) && pe.Status == http.StatusUnauthorized
}
func IsNotFound(err error) bool {
	var pe *platformError
	return errors.As(err, &pe) && pe.Status == http.StatusNotFound
}
func IsBadRequest(err error) bool {
	var pe *platformError
	return errors.As(err, &pe) && pe.Status == http.StatusBadRequest
}
func IsPayloadTooLarge(err error) bool {
	var pe *platformError
	return errors.As(err, &pe) && pe.Status == http.StatusRequestEntityTooLarge
}
func StatusCode(err error) int {
	var pe *platformError
	if errors.As(err, &pe) {
		return pe.Status
	}
	return 0
}
func ErrorCode(err error) string {
	var pe *platformError
	if errors.As(err, &pe) {
		return pe.ErrorCode
	}
	return ""
}
func ErrorHint(err error) string {
	var pe *platformError
	if errors.As(err, &pe) {
		return pe.ErrorHint
	}
	return ""
}

type AuthRegisterInput struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Company  string `json:"company"`
	Password string `json:"password"`
	Avatar   string `json:"avatar,omitempty"`
}

type AuthLoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type PlatformOrganizationLite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type PlatformUserProfile struct {
	ID              string                     `json:"id"`
	Email           string                     `json:"email"`
	FullName        string                     `json:"full_name"`
	AvatarURL       string                     `json:"avatar_url"`
	Role            string                     `json:"role"`
	OrgRole         string                     `json:"org_role"`
	OrgID           string                     `json:"org_id"`
	LastActiveOrgID string                     `json:"last_active_org_id"`
	PlanID          string                     `json:"plan_id"`
	Status          string                     `json:"status"`
	Permissions     []string                   `json:"permissions"`
	Orgs            []PlatformOrganizationLite `json:"orgs"`
}

type PlatformAuthResult struct {
	AccessToken string              `json:"access_token"`
	User        PlatformUserProfile `json:"user"`
}

type PlatformAccessData struct {
	UserID      string   `json:"user_id"`
	OrgID       string   `json:"org_id"`
	OrgRole     string   `json:"org_role"`
	Permissions []string `json:"permissions"`
}

type WalletSummary struct {
	BillingSubjectType string               `json:"billing_subject_type"`
	BillingSubjectID   string               `json:"billing_subject_id"`
	ProductCode        string               `json:"product_code"`
	TotalBalance       int64                `json:"total_balance"`
	PermanentBalance   int64                `json:"permanent_balance"`
	RewardBalance      int64                `json:"reward_balance"`
	AllowanceBalance   int64                `json:"allowance_balance"`
	Assets             []WalletAssetSummary `json:"assets"`
}

type Product struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	OwnerTeam string    `json:"owner_team"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SKU struct {
	ID          string    `json:"id"`
	ProductID   string    `json:"product_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	SKUType     string    `json:"sku_type"`
	BillingMode string    `json:"billing_mode"`
	Currency    string    `json:"currency"`
	ListPrice   int64     `json:"list_price"`
	Status      string    `json:"status"`
	Metadata    string    `json:"metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CommercialPackage struct {
	ID          string    `json:"id"`
	ProductID   string    `json:"product_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	PackageType string    `json:"package_type"`
	Status      string    `json:"status"`
	Metadata    string    `json:"metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type BillableItem struct {
	ID              string    `json:"id"`
	ProductID       string    `json:"product_id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	MeterUnit       string    `json:"meter_unit"`
	BillingScope    string    `json:"billing_scope"`
	SettlementMode  string    `json:"settlement_mode"`
	PricingBehavior string    `json:"pricing_behavior"`
	Status          string    `json:"status"`
	Metadata        string    `json:"metadata"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type RateCard struct {
	ID            string     `json:"id"`
	ProductID     string     `json:"product_id"`
	Code          string     `json:"code"`
	TargetType    string     `json:"target_type"`
	TargetID      string     `json:"target_id"`
	PriceModel    string     `json:"price_model"`
	Currency      string     `json:"currency"`
	PriceConfig   string     `json:"price_config"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
	Version       int64      `json:"version"`
	Status        string     `json:"status"`
	Metadata      string     `json:"metadata"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type AllowancePolicy struct {
	ID                 string     `json:"id"`
	ProductCode        string     `json:"product_code"`
	BillingSubjectType string     `json:"billing_subject_type"`
	BillingSubjectID   string     `json:"billing_subject_id"`
	AssetCode          string     `json:"asset_code"`
	Amount             int64      `json:"amount"`
	ResetCycle         string     `json:"reset_cycle"`
	Status             string     `json:"status"`
	EffectiveFrom      *time.Time `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time `json:"effective_to,omitempty"`
	Metadata           string     `json:"metadata"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type QuotaGrantPolicy struct {
	ID               string    `json:"id"`
	ProductCode      string    `json:"product_code"`
	PackageCode      string    `json:"package_code"`
	BillableItemCode string    `json:"billable_item_code"`
	GrantMode        string    `json:"grant_mode"`
	Units            int64     `json:"units"`
	ResetCycle       string    `json:"reset_cycle"`
	Status           string    `json:"status"`
	Metadata         string    `json:"metadata"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type OfferingsView struct {
	Product           *Product            `json:"product,omitempty"`
	SKUs              []SKU               `json:"skus"`
	Packages          []CommercialPackage `json:"packages"`
	BillableItems     []BillableItem      `json:"billable_items"`
	RateCards         []RateCard          `json:"rate_cards"`
	AssetDefinitions  []AssetDefinition   `json:"asset_definitions"`
	AllowancePolicies []AllowancePolicy   `json:"allowance_policies"`
}

type GrantQuotaInput struct {
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	BillableItemCode   string `json:"billable_item_code"`
	Units              int64  `json:"units"`
	Reason             string `json:"reason,omitempty"`
	ReferenceID        string `json:"reference_id,omitempty"`
}

type PostWalletLedgerInput struct {
	BillingSubjectType string     `json:"billing_subject_type"`
	BillingSubjectID   string     `json:"billing_subject_id"`
	AssetCode          string     `json:"asset_code"`
	AssetType          string     `json:"asset_type,omitempty"`
	Direction          string     `json:"direction"`
	Amount             int64      `json:"amount"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	Reason             string     `json:"reason,omitempty"`
	ReferenceType      string     `json:"reference_type,omitempty"`
	ReferenceID        string     `json:"reference_id,omitempty"`
	Metadata           string     `json:"metadata,omitempty"`
}

type WalletBucket struct {
	ID                 string     `json:"id"`
	WalletAccountID    string     `json:"wallet_account_id"`
	BillingSubjectType string     `json:"billing_subject_type"`
	BillingSubjectID   string     `json:"billing_subject_id"`
	AssetCode          string     `json:"asset_code"`
	AssetType          string     `json:"asset_type"`
	LifecycleType      string     `json:"lifecycle_type"`
	SourceType         string     `json:"source_type"`
	SourceID           string     `json:"source_id"`
	CycleKey           string     `json:"cycle_key"`
	Balance            int64      `json:"balance"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	Status             string     `json:"status"`
	Metadata           string     `json:"metadata"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type GrantCycleAllowanceInput struct {
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	AssetCode          string `json:"asset_code"`
	CycleKey           string `json:"cycle_key"`
	Amount             int64  `json:"amount"`
	Metadata           string `json:"metadata,omitempty"`
}

type CreateRuntimeJobInput struct {
	ProductCode     string `json:"product_code"`
	TaskType        string `json:"task_type"`
	ProviderCode    string `json:"provider_code,omitempty"`
	ProviderMode    string `json:"provider_mode"`
	OrganizationID  string `json:"organization_id"`
	UserID          string `json:"user_id,omitempty"`
	SourceType      string `json:"source_type"`
	SourceID        string `json:"source_id"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
	ChargeSessionID string `json:"charge_session_id,omitempty"`
	InputManifest   string `json:"input_manifest,omitempty"`
	RouteSnapshot   string `json:"route_snapshot,omitempty"`
	Metadata        string `json:"metadata,omitempty"`
	Priority        int    `json:"priority,omitempty"`
	MaxAttempts     int    `json:"max_attempts,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
}

type RuntimeJob struct {
	ID             string `json:"id"`
	ProductCode    string `json:"product_code"`
	TaskType       string `json:"task_type"`
	ProviderCode   string `json:"provider_code"`
	ProviderMode   string `json:"provider_mode"`
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	SourceType     string `json:"source_type"`
	SourceID       string `json:"source_id"`
	Status         string `json:"status"`
	Stage          string `json:"stage"`
	StageMessage   string `json:"stage_message"`
	ProviderJobID  string `json:"provider_job_id"`
	InputManifest  string `json:"input_manifest"`
	RouteSnapshot  string `json:"route_snapshot"`
	Metadata       string `json:"metadata"`
}

type RuntimeCapabilityMatrix struct {
	ProductCode string                  `json:"product_code"`
	Items       []RuntimeCapabilityItem `json:"items"`
}

type RuntimeCapabilityItem struct {
	TaskType          string                    `json:"task_type"`
	Status            string                    `json:"status"`
	Available         bool                      `json:"available"`
	UnavailableReason string                    `json:"unavailable_reason"`
	ContractStatus    string                    `json:"contract_status"`
	ProviderBindings  []RuntimeProviderBinding  `json:"provider_bindings"`
	Callback          RuntimeCallbackCapability `json:"callback"`
	Storage           RuntimeStorageCapability  `json:"storage"`
	Billing           RuntimeBillingCapability  `json:"billing"`
	Reasons           []RuntimeCapabilityReason `json:"reasons"`
}

type RuntimeProviderBinding struct {
	ProviderCode string         `json:"provider_code"`
	Enabled      bool           `json:"enabled"`
	Registered   bool           `json:"registered"`
	Status       string         `json:"status"`
	Priority     int            `json:"priority"`
	Model        string         `json:"model"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type RuntimeCallbackCapability struct {
	Configured   bool   `json:"configured"`
	CallbackKind string `json:"callback_kind"`
}

type RuntimeStorageCapability struct {
	OutputCategory    string `json:"output_category"`
	BindingConfigured bool   `json:"binding_configured"`
}

type RuntimeBillingCapability struct {
	BillableItemCode string `json:"billable_item_code"`
	MeterUnit        string `json:"meter_unit"`
	SettlementMode   string `json:"settlement_mode"`
	Configured       bool   `json:"configured"`
}

type RuntimeCapabilityReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ChargeSession struct {
	ID                 string `json:"id"`
	SourceType         string `json:"source_type"`
	SourceID           string `json:"source_id"`
	ProductCode        string `json:"product_code"`
	OrganizationID     string `json:"organization_id"`
	UserID             string `json:"user_id"`
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	BillableItemCode   string `json:"billable_item_code"`
	ResourceType       string `json:"resource_type"`
	ReservationKey     string `json:"reservation_key"`
	EstimatedUnits     int64  `json:"estimated_units"`
	Status             string `json:"status"`
	RouteSnapshot      string `json:"route_snapshot"`
	Metadata           string `json:"metadata"`
}

type PlatformTemplateCatalogItem struct {
	TemplateRef    string         `json:"template_ref"`
	ProductCode    string         `json:"product_code"`
	TemplateID     string         `json:"template_id"`
	Slug           string         `json:"slug"`
	Name           string         `json:"name"`
	Summary        string         `json:"summary"`
	Status         string         `json:"status"`
	CoverAssetURL  string         `json:"cover_asset_url"`
	CoverAssetID   string         `json:"cover_asset_id"`
	RecommendScore int            `json:"recommend_score"`
	Tags           []string       `json:"tags"`
	Platforms      []string       `json:"platforms"`
	Series         string         `json:"series"`
	CapabilityType string         `json:"capability_type"`
	Modality       string         `json:"modality"`
	Scope          string         `json:"scope"`
	ManagedSource  string         `json:"managed_source"`
	Raw            map[string]any `json:"raw"`
}

type PlatformTemplateCatalogResult struct {
	Items  []PlatformTemplateCatalogItem `json:"items"`
	Total  int                           `json:"total"`
	Limit  int                           `json:"limit"`
	Offset int                           `json:"offset"`
}

type PlatformTemplateCatalogDetail struct {
	Item      PlatformTemplateCatalogItem `json:"item"`
	Product   string                      `json:"product"`
	DetailRaw map[string]any              `json:"detail_raw"`
}

type CreateChargeSessionInput struct {
	SourceType         string `json:"source_type"`
	SourceID           string `json:"source_id"`
	ProductCode        string `json:"product_code"`
	OrganizationID     string `json:"organization_id"`
	UserID             string `json:"user_id,omitempty"`
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	BillableItemCode   string `json:"billable_item_code"`
	ResourceType       string `json:"resource_type"`
	ReservationKey     string `json:"reservation_key,omitempty"`
	EstimatedUnits     int64  `json:"estimated_units,omitempty"`
	RouteSnapshot      string `json:"route_snapshot,omitempty"`
	Metadata           string `json:"metadata,omitempty"`
}

type UpdateChargeSessionInput struct {
	Status         string `json:"status,omitempty"`
	ReservationID  string `json:"reservation_id,omitempty"`
	FinalizationID string `json:"finalization_id,omitempty"`
	EventID        string `json:"event_id,omitempty"`
	SettlementID   string `json:"settlement_id,omitempty"`
	FinalUnits     *int64 `json:"final_units,omitempty"`
	RouteSnapshot  string `json:"route_snapshot,omitempty"`
	Metadata       string `json:"metadata,omitempty"`
}

type ResourceReservation struct {
	ID                 string `json:"id"`
	ResourceType       string `json:"resource_type"`
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	BillableItemCode   string `json:"billable_item_code"`
	ReservationKey     string `json:"reservation_key"`
	FinalizationID     string `json:"finalization_id"`
	Units              int64  `json:"units"`
	Status             string `json:"status"`
	ReferenceID        string `json:"reference_id"`
	Metadata           string `json:"metadata"`
}

type ReserveInput struct {
	ResourceType       string `json:"resource_type"`
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	BillableItemCode   string `json:"billable_item_code,omitempty"`
	ReservationKey     string `json:"reservation_key,omitempty"`
	Units              int64  `json:"units"`
	ReferenceID        string `json:"reference_id,omitempty"`
	Metadata           string `json:"metadata,omitempty"`
}

type IngestEventInput struct {
	EventID               string `json:"event_id"`
	RequestID             string `json:"request_id,omitempty"`
	TraceID               string `json:"trace_id,omitempty"`
	SourceType            string `json:"source_type,omitempty"`
	SourceID              string `json:"source_id,omitempty"`
	SourceAction          string `json:"source_action,omitempty"`
	ProductCode           string `json:"product_code"`
	OrgID                 string `json:"org_id,omitempty"`
	UserID                string `json:"user_id,omitempty"`
	BillableItemCode      string `json:"billable_item_code"`
	ChargeGroupID         string `json:"charge_group_id,omitempty"`
	ParentEventID         string `json:"parent_event_id,omitempty"`
	EventRole             string `json:"event_role,omitempty"`
	BillingSubjectType    string `json:"billing_subject_type,omitempty"`
	BillingSubjectID      string `json:"billing_subject_id,omitempty"`
	UsageUnits            int64  `json:"usage_units,omitempty"`
	Unit                  string `json:"unit,omitempty"`
	Billable              *bool  `json:"billable,omitempty"`
	BillingProfileKey     string `json:"billing_profile_key,omitempty"`
	CurrencyContext       string `json:"currency_context,omitempty"`
	Dimensions            string `json:"dimensions,omitempty"`
	OccurredAt            string `json:"occurred_at,omitempty"`
	DiscountType          string `json:"discount_type,omitempty"`
	DiscountAmount        int64  `json:"discount_amount,omitempty"`
	CampaignCode          string `json:"campaign_code,omitempty"`
	RewardAmount          int64  `json:"reward_amount,omitempty"`
	RewardAssetCode       string `json:"reward_asset_code,omitempty"`
	RewardSubjectType     string `json:"reward_subject_type,omitempty"`
	RewardSubjectID       string `json:"reward_subject_id,omitempty"`
	ReferralCode          string `json:"referral_code,omitempty"`
	CommissionAmount      int64  `json:"commission_amount,omitempty"`
	CommissionType        string `json:"commission_type,omitempty"`
	CommissionSubjectType string `json:"commission_subject_type,omitempty"`
	CommissionSubjectID   string `json:"commission_subject_id,omitempty"`
	Metadata              string `json:"metadata,omitempty"`
}

type FinalizeInput struct {
	FinalizationID string `json:"finalization_id"`
	ReservationID  string `json:"reservation_id"`
	IngestEventInput
}

type SettlementRecord struct {
	ID               string `json:"id"`
	EventID          string `json:"event_id"`
	ProductCode      string `json:"product_code"`
	BillableItemCode string `json:"billable_item_code"`
	Currency         string `json:"currency"`
	Amount           int64  `json:"amount"`
	Status           string `json:"status"`
	Metadata         string `json:"metadata"`
}

type FinalizeResult struct {
	Reservation map[string]any    `json:"reservation"`
	Event       map[string]any    `json:"event"`
	Settlement  *SettlementRecord `json:"settlement"`
}

type UploadAssetInput struct {
	ProductCode string `json:"product_code"`
	Category    string `json:"category"`
	FileName    string `json:"file_name"`
	MimeType    string `json:"mime_type"`
	Payload     string `json:"payload"`
}

type StoredAsset struct {
	StorageKey string `json:"storage_key"`
	MimeType   string `json:"mime_type"`
	FileSize   int64  `json:"file_size"`
}

type ResolveAssetInput struct {
	ProductCode string `json:"product_code,omitempty"`
	Category    string `json:"category,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	SourceRef   string `json:"source_ref,omitempty"`
	StorageKey  string `json:"storage_key,omitempty"`
}

type AssetRecord struct {
	ID          string         `json:"id"`
	ProductCode string         `json:"product_code"`
	Category    string         `json:"category"`
	SourceType  string         `json:"source_type"`
	SourceRef   string         `json:"source_ref"`
	StorageKey  string         `json:"storage_key"`
	FileName    string         `json:"file_name"`
	MimeType    string         `json:"mime_type"`
	FileSize    int64          `json:"file_size"`
	Checksum    string         `json:"checksum"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Tags        []string       `json:"tags"`
	Metadata    map[string]any `json:"metadata"`
	Status      string         `json:"status"`
}

type QuotaBalance struct {
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	BillableItemCode   string `json:"billable_item_code"`
	Granted            int64  `json:"granted"`
	Consumed           int64  `json:"consumed"`
	Reserved           int64  `json:"reserved"`
	Available          int64  `json:"available"`
}

func (c *Client) Register(input AuthRegisterInput) (*PlatformAuthResult, error) {
	return doPublicPost[AuthRegisterInput, PlatformAuthResult](c, "/auth/register", input)
}
func (c *Client) Login(input AuthLoginInput) (*PlatformAuthResult, error) {
	return doPublicPost[AuthLoginInput, PlatformAuthResult](c, "/auth/login", input)
}
func (c *Client) GetUserProfile(userID, orgID string) (*PlatformUserProfile, error) {
	path := fmt.Sprintf("/users/%s/profile", userID)
	if orgID != "" {
		path += "?org_id=" + orgID
	}
	return doInternalGet[PlatformUserProfile](c, path)
}
func (c *Client) GetAccessContext(userID, orgID string) (*PlatformAccessData, error) {
	return doInternalGet[PlatformAccessData](c, fmt.Sprintf("/access/users/%s/orgs/%s", userID, orgID))
}
func (c *Client) GetWalletSummary(subjectType, subjectID, productCode string) (*WalletSummary, error) {
	return doInternalGet[WalletSummary](c, fmt.Sprintf("/wallet/summary?billing_subject_type=%s&billing_subject_id=%s&product_code=%s", subjectType, subjectID, productCode))
}
func (c *Client) GetQuotaBalance(subjectType, subjectID, billableItemCode string) (*QuotaBalance, error) {
	return doInternalGet[QuotaBalance](c, withQuery("/controls/quota/balance", map[string]string{
		"billing_subject_type": subjectType,
		"billing_subject_id":   subjectID,
		"billable_item_code":   billableItemCode,
	}))
}
func (c *Client) GetCatalogOfferings(productCode string) (*OfferingsView, error) {
	return doInternalGet[OfferingsView](c, fmt.Sprintf("/catalog/offerings?product_code=%s", productCode))
}
func (c *Client) GetAllowancePolicy(policyID string) (*AllowancePolicy, error) {
	return doInternalGet[AllowancePolicy](c, "/wallet/allowances/"+policyID)
}
