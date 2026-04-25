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
	BillingSubjectType string `json:"billing_subject_type"`
	BillingSubjectID   string `json:"billing_subject_id"`
	ProductCode        string `json:"product_code"`
	TotalBalance       int64  `json:"total_balance"`
	PermanentBalance   int64  `json:"permanent_balance"`
	RewardBalance      int64  `json:"reward_balance"`
	AllowanceBalance   int64  `json:"allowance_balance"`
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
func (c *Client) CreateRuntimeJob(input CreateRuntimeJobInput) (*RuntimeJob, error) {
	return doInternalPost[CreateRuntimeJobInput, RuntimeJob](c, "/runtime/jobs", input)
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
