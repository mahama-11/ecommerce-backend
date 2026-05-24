package platform

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
func (c *Client) ListRuntimeCapabilities(productCode, taskType string) (*RuntimeCapabilityMatrix, error) {
	return doInternalGet[RuntimeCapabilityMatrix](c, withQuery("/runtime/capabilities", map[string]string{
		"product_code": productCode,
		"task_type":    taskType,
	}))
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
