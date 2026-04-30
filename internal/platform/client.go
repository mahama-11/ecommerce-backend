package platform

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
func (c *Client) PostWalletLedger(input PostWalletLedgerInput) (*WalletAccount, *WalletBucket, *WalletLedger, error) {
	type walletLedgerResp struct {
		Ledger   *WalletLedger  `json:"ledger"`
		Account  *WalletAccount `json:"account"`
		Bucket   *WalletBucket  `json:"bucket"`
		Consumed []WalletBucket `json:"consumed"`
	}
	resp, err := doInternalPost[PostWalletLedgerInput, walletLedgerResp](c, "/wallet/ledger", input)
	if err != nil {
		return nil, nil, nil, err
	}
	return resp.Account, resp.Bucket, resp.Ledger, nil
}
func (c *Client) GrantCycleAllowance(input GrantCycleAllowanceInput) (*WalletBucket, *WalletAccount, error) {
	type allowanceResp struct {
		Account *WalletAccount `json:"account"`
		Bucket  *WalletBucket  `json:"bucket"`
	}
	resp, err := doInternalPost[GrantCycleAllowanceInput, allowanceResp](c, "/wallet/cycle-allowances", input)
	if err != nil {
		return nil, nil, err
	}
	return resp.Bucket, resp.Account, nil
}
func (c *Client) CreateRuntimeJob(input CreateRuntimeJobInput) (*RuntimeJob, error) {
	return doInternalPost[CreateRuntimeJobInput, RuntimeJob](c, "/runtime/jobs", input)
}
func (c *Client) ListQuotaGrantPolicies(productCode, packageCode string) ([]QuotaGrantPolicy, error) {
	out, err := doInternalGet[platformItemsResponse[QuotaGrantPolicy]](c, withQuery("/controls/quota/policies", map[string]string{
		"product_code": productCode,
		"package_code": packageCode,
	}))
	if err != nil {
		return nil, err
	}
	return out.Items, nil
}
func (c *Client) GrantQuota(input GrantQuotaInput) error {
	_, err := doInternalPost[GrantQuotaInput, map[string]any](c, "/controls/quota/grants", input)
	return err
}
func (c *Client) CancelRuntimeJob(runtimeJobID string) (*RuntimeJob, error) {
	return doInternalPost[map[string]any, RuntimeJob](c, fmt.Sprintf("/runtime/jobs/%s/cancel", runtimeJobID), map[string]any{})
}
func (c *Client) CreateChargeSession(input CreateChargeSessionInput) (*ChargeSession, error) {
	return doInternalPost[CreateChargeSessionInput, ChargeSession](c, "/runtime/charge-sessions", input)
}
func (c *Client) UpdateChargeSession(chargeSessionID string, input UpdateChargeSessionInput) (*ChargeSession, error) {
	return doInternalPut[UpdateChargeSessionInput, ChargeSession](c, fmt.Sprintf("/runtime/charge-sessions/%s", chargeSessionID), input)
}
func (c *Client) ReserveResources(input ReserveInput) (*ResourceReservation, error) {
	return doInternalPost[ReserveInput, ResourceReservation](c, "/controls/reservations", input)
}
func (c *Client) CommitReservation(reservationID string) (*ResourceReservation, error) {
	return doInternalPost[map[string]any, ResourceReservation](c, fmt.Sprintf("/controls/reservations/%s/commit", reservationID), map[string]any{})
}
func (c *Client) ReleaseReservation(reservationID string) (*ResourceReservation, error) {
	return doInternalPost[map[string]any, ResourceReservation](c, fmt.Sprintf("/controls/reservations/%s/release", reservationID), map[string]any{})
}
func (c *Client) IngestMeteringEvent(input IngestEventInput) error {
	_, err := doInternalPost[IngestEventInput, map[string]any](c, "/metering/events", input)
	return err
}
func (c *Client) FinalizeMetering(input FinalizeInput) (*FinalizeResult, error) {
	return doInternalPost[FinalizeInput, FinalizeResult](c, "/metering/finalizations", input)
}
func (c *Client) UploadAsset(input UploadAssetInput) (*StoredAsset, error) {
	return doInternalPost[UploadAssetInput, StoredAsset](c, "/storage/assets", input)
}
func (c *Client) ResolveAssets(items []ResolveAssetInput) ([]AssetRecord, error) {
	type resolveResp struct {
		Items []AssetRecord `json:"items"`
	}
	resp, err := doInternalPost[map[string]any, resolveResp](c, "/storage/assets/resolve", map[string]any{"items": items})
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}
func (c *Client) DownloadAsset(storageKey string) (io.ReadCloser, http.Header, error) {
	path := withQuery("/storage/assets/content", map[string]string{"storage_key": storageKey})
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/internal/v1"+path, nil)
	if err != nil {
		return nil, nil, err
	}
	for key, value := range c.buildHeaders(http.MethodGet, path, nil) {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, nil, fmt.Errorf("platform asset download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, resp.Header, nil
}

func doPublicPost[Req any, Resp any](c *Client, path string, payload Req) (*Resp, error) {
	return doRequest[Resp](c, http.MethodPost, c.baseURL+"/api/v1"+path, path, payload, false)
}
func doInternalGet[Resp any](c *Client, path string) (*Resp, error) {
	return doRequest[Resp](c, http.MethodGet, c.baseURL+"/internal/v1"+path, path, nil, true)
}
func doInternalPost[Req any, Resp any](c *Client, path string, payload Req) (*Resp, error) {
	return doRequest[Resp](c, http.MethodPost, c.baseURL+"/internal/v1"+path, path, payload, true)
}
func doInternalPut[Req any, Resp any](c *Client, path string, payload Req) (*Resp, error) {
	return doRequest[Resp](c, http.MethodPut, c.baseURL+"/internal/v1"+path, path, payload, true)
}

type InternalTemplateCatalogInput struct {
	ProductCode   string
	ToolSlug      string
	Limit         int
	Offset        int
	PublishedOnly bool
}

func (c *Client) InternalTemplateCatalog(input InternalTemplateCatalogInput) (*PlatformTemplateCatalogResult, error) {
	params := url.Values{}
	params.Set("product_code", input.ProductCode)
	if strings.TrimSpace(input.ToolSlug) != "" {
		params.Set("tool_slug", strings.TrimSpace(input.ToolSlug))
	}
	if input.Limit > 0 {
		params.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Offset > 0 {
		params.Set("offset", strconv.Itoa(input.Offset))
	}
	if input.PublishedOnly {
		params.Set("published_only", "true")
	}
	return doInternalGet[PlatformTemplateCatalogResult](c, "/template-ops/catalog?"+params.Encode())
}

func (c *Client) InternalTemplateCatalogDetail(templateRef string) (*PlatformTemplateCatalogDetail, error) {
	return doInternalGet[PlatformTemplateCatalogDetail](c, "/template-ops/catalog/"+url.PathEscape(templateRef))
}

func doRequest[T any](c *Client, method, url, path string, payload any, internal bool) (*T, error) {
	body, err := encodePayload(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if internal {
		for key, value := range c.buildHeaders(method, path, body) {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out envelope[T]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 || out.Code != 0 {
		return nil, &platformError{Status: resp.StatusCode, Code: out.Code, Message: out.Message, ErrorCode: out.ErrorCode, ErrorHint: out.ErrorHint, Err: out.Error}
	}
	return &out.Data, nil
}

func (c *Client) buildHeaders(method, path string, body []byte) map[string]string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := sign(c.secret, c.serviceName, method, path, timestamp, body)
	return map[string]string{"X-Internal-Service": c.serviceName, "X-Internal-Timestamp": timestamp, "X-Internal-Signature": signature, "X-Internal-Service-Secret": c.secret, "X-Request-ID": buildRequestID(c.serviceName), "X-Trace-ID": buildRequestID("trace")}
}

func encodePayload(payload any) ([]byte, error) {
	if payload == nil {
		return nil, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" {
		return nil, nil
	}
	return data, nil
}

func sign(secret, service, method, path, timestamp string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", service, method, path, timestamp, hex.EncodeToString(bodyHash[:]))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func buildRequestID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }

func withQuery(path string, values map[string]string) string {
	q := url.Values{}
	for key, value := range values {
		if value != "" {
			q.Set(key, value)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
