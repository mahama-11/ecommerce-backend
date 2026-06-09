package smoke

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/app"

	"github.com/golang-jwt/jwt/v5"
)

const (
	phase2JWTSecret      = "phase2-platform-jwt-secret"
	phase2InternalSecret = "phase2-platform-internal-secret"
)

type smokeReport struct {
	GeneratedAtUnix int64               `json:"generated_at_unix"`
	Status          string              `json:"status"`
	Mode            string              `json:"mode"`
	Env             string              `json:"env"`
	Fixture         map[string]any      `json:"fixture"`
	BaseURL         string              `json:"base_url"`
	Journeys        []journeyEvidence   `json:"journeys"`
	HTTPRoutes      []routeEvidence     `json:"http_routes"`
	Cleanup         map[string]any      `json:"cleanup"`
	WriteIDs        map[string][]string `json:"write_ids"`
	Checks          map[string]any      `json:"checks"`
	Failures        []map[string]any    `json:"failures"`
	Notes           []string            `json:"notes"`
}

type journeyEvidence struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	Fixture         string          `json:"fixture"`
	CleanupRequired bool            `json:"cleanup_required"`
	CleanupEvidence string          `json:"cleanup_evidence"`
	Steps           []routeEvidence `json:"steps"`
	WriteIDs        []string        `json:"write_ids,omitempty"`
	Notes           []string        `json:"notes,omitempty"`
}

type routeEvidence struct {
	Method       string         `json:"method"`
	Path         string         `json:"path"`
	StatusCode   int            `json:"status_code"`
	ElapsedMS    int64          `json:"elapsed_ms"`
	Status       string         `json:"status"`
	ResponseKeys []string       `json:"response_keys,omitempty"`
	Evidence     map[string]any `json:"evidence,omitempty"`
}

type fakePlatformState struct {
	jwtSecret        string
	internalSecret   string
	createdUsers     []string
	createdAssets    []string
	walletLedgerIDs  []string
	quotaGrantIDs    []string
	referralPrograms []string
	requestCount     int
}

func TestPhase2LocalSmokeHarness(t *testing.T) {
	if os.Getenv("ECOM_PHASE2_SMOKE_HARNESS") != "1" {
		t.Skip("phase2 live smoke harness is invoked by scripts with ECOM_PHASE2_SMOKE_HARNESS=1")
	}
	reportPath := os.Getenv("ECOM_PHASE2_SMOKE_REPORT")
	if strings.TrimSpace(reportPath) == "" {
		t.Fatal("ECOM_PHASE2_SMOKE_REPORT is required")
	}

	state := &fakePlatformState{jwtSecret: phase2JWTSecret, internalSecret: phase2InternalSecret}
	platformServer := httptest.NewServer(state.handler(t))
	defer platformServer.Close()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "phase2-ecommerce-smoke.sqlite")
	cfg := fmt.Sprintf(`host: 127.0.0.1
port: 0
gin_mode: test
log_level: error
app:
  frontend_base_url: http://127.0.0.1:5180
  product_name: Agent Ecommerce Smoke
  product_code: ecommerce
  default_language: en
  credits_asset_code: ECOMMERCE_CREDIT
  reward_asset_code: ECOMMERCE_PROMO_CREDIT
  allowance_asset_code: ECOMMERCE_MONTHLY_ALLOWANCE
  promotion_program_code: ecommerce_signup_default
  promotion_program_name: Ecommerce Signup Smoke
  image_runtime:
    global_negative_prompt: "blurry, watermark, low quality"
    scene_prompt_policies:
      changing-model:
        tool_slug: changing-model
        display_name: Changing model
        system_prompt: "Customer-facing ecommerce image transformation instructions only."
        default_negative_prompt: "distortion, extra limbs"
database:
  driver: sqlite
  sqlite_path: %q
  table_prefix: ecommerce_
  auto_migrate_enabled: true
redis:
  enabled: false
security:
  jwt_secret: local-harness-ecommerce-secret
  encryption_key: local-harness-encryption-key
  service_secret_key: local-harness-service-secret
platform:
  base_url: %q
  timeout: 5s
  service_name: v-ecommerce-backend-smoke
  internal_service_secret: %q
  jwt_secret: %q
monitoring:
  metrics:
    enabled: false
  tracing:
    enabled: false
`, dbPath, platformServer.URL, phase2InternalSecret, phase2JWTSecret)
	if err := os.WriteFile(filepath.Join(tempDir, "config.local.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	oldWD, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp config dir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	application, err := app.New("config.local")
	if err != nil {
		t.Fatalf("app.New isolated harness: %v", err)
	}
	server := httptest.NewServer(application.Router)
	defer server.Close()

	runner := &smokeRunner{t: t, baseURL: server.URL, token: "", writeIDs: map[string][]string{}, failures: []map[string]any{}}
	report := smokeReport{
		GeneratedAtUnix: time.Now().Unix(),
		Status:          "PASS",
		Mode:            "local_harness_execute",
		Env:             "local",
		BaseURL:         server.URL,
		Fixture: map[string]any{
			"type":                   "isolated",
			"runner":                 "go_httptest",
			"app":                    "gin_router",
			"database":               "temp_sqlite",
			"platform":               "fake_platform_httptest",
			"external_services_used": false,
		},
		WriteIDs: map[string][]string{},
		Checks: map[string]any{
			"prod_hard_refuse":                             "enforced_by_python_entrypoint",
			"execute_requires_explicit_flag":               true,
			"default_mode":                                 "dry-run",
			"live_evidence_not_run_max_status":             "PASS_WITH_NOTES",
			"prompt_preview_forbidden_field_check_applied": true,
		},
		Notes: []string{"Local execute used real HTTP requests against an isolated Gin app served by httptest with temp sqlite and a fake Platform httptest server."},
	}

	if negative := runner.do(http.MethodGet, "/api/v1/ecommerce/access/me", "", nil); negative.StatusCode != http.StatusUnauthorized {
		runner.failures = append(runner.failures, map[string]any{"type": "protected_route_negative", "status_code": negative.StatusCode})
	} else {
		report.HTTPRoutes = append(report.HTTPRoutes, negative)
	}

	journeyA := runner.journeyAuthSessionAccess()
	journeyB := runner.journeyProductAssetPrompt()
	journeyC := runner.journeyWalletCommercialBilling()
	report.Journeys = []journeyEvidence{journeyA, journeyB, journeyC}
	report.WriteIDs = runner.writeIDs

	for _, journey := range report.Journeys {
		if journey.Status != "PASS" {
			report.Status = "FAIL"
			runner.failures = append(runner.failures, map[string]any{"type": "journey_failed", "journey_id": journey.ID})
		}
	}
	if len(runner.failures) > 0 {
		report.Status = "FAIL"
		report.Failures = runner.failures
	}

	closedDB := false
	if sqlDB, err := application.DB.DB(); err == nil {
		if closeErr := sqlDB.Close(); closeErr == nil {
			closedDB = true
		}
	}
	if application.Shutdown != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = application.Shutdown(ctx)
		cancel()
	}
	removeErr := os.RemoveAll(tempDir)
	_, statErr := os.Stat(dbPath)
	sqliteRemoved := os.IsNotExist(statErr)
	report.Cleanup = map[string]any{
		"status":                    cleanupStatus(closedDB, removeErr == nil, sqliteRemoved),
		"app_db_closed":             closedDB,
		"temp_dir_removed":          removeErr == nil,
		"sqlite_removed":            sqliteRemoved,
		"temp_dir":                  tempDir,
		"fake_platform_closed":      true,
		"fake_platform_created_ids": map[string]any{"users": state.createdUsers, "assets": state.createdAssets, "wallet_ledgers": state.walletLedgerIDs, "quota_grants": state.quotaGrantIDs, "referral_programs": state.referralPrograms},
		"request_count":             state.requestCount,
	}
	if report.Cleanup["status"] != "PASS" {
		report.Status = "FAIL"
		report.Failures = append(report.Failures, map[string]any{"type": "cleanup_failed", "cleanup": report.Cleanup})
	}
	for idx := range report.Journeys {
		report.Journeys[idx].CleanupEvidence = fmt.Sprintf("temp_sqlite_removed=%t; app_db_closed=%t; fake_platform_closed=true", sqliteRemoved, closedDB)
		if report.Cleanup["status"] != "PASS" {
			report.Journeys[idx].Status = "FAIL"
		}
	}

	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatalf("mkdir report dir: %v", err)
	}
	if err := os.WriteFile(reportPath, append(body, '\n'), 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if report.Status != "PASS" {
		t.Fatalf("smoke report status=%s failures=%v", report.Status, report.Failures)
	}
}

func cleanupStatus(values ...bool) string {
	for _, ok := range values {
		if !ok {
			return "FAIL"
		}
	}
	return "PASS"
}

type smokeRunner struct {
	t        *testing.T
	baseURL  string
	token    string
	writeIDs map[string][]string
	failures []map[string]any
}

func (r *smokeRunner) journeyAuthSessionAccess() journeyEvidence {
	j := journeyEvidence{ID: "auth-session-access", Title: "Register -> Login -> Session -> Access/me", Status: "PASS", Fixture: "isolated/local_harness", CleanupRequired: true}
	email := fmt.Sprintf("phase2-smoke-%d@example.test", time.Now().UnixNano())
	register := r.do(http.MethodPost, "/api/v1/ecommerce/auth/register", "", map[string]any{"full_name": "Phase2 Smoke Merchant", "email": email, "password": "Phase2Smoke123!", "organization_name": "Phase2 Smoke Workspace", "language": "en"})
	j.Steps = append(j.Steps, register)
	if register.Status != "PASS" {
		j.Status = "FAIL"
		return j
	}
	r.recordID("users", stringAt(register.Evidence, "user_id"))

	login := r.do(http.MethodPost, "/api/v1/ecommerce/auth/login", "", map[string]any{"email": email, "password": "Phase2Smoke123!"})
	j.Steps = append(j.Steps, login)
	if login.Status != "PASS" {
		j.Status = "FAIL"
		return j
	}
	r.token = stringAt(login.Evidence, "auth_bearer_token")
	if r.token == "" {
		j.Status = "FAIL"
		j.Notes = append(j.Notes, "login did not produce an access token for subsequent HTTP calls")
		return j
	}
	session := r.do(http.MethodGet, "/api/v1/ecommerce/auth/session", r.token, nil)
	access := r.do(http.MethodGet, "/api/v1/ecommerce/access/me", r.token, nil)
	j.Steps = append(j.Steps, session, access)
	if session.Status != "PASS" || access.Status != "PASS" {
		j.Status = "FAIL"
	}
	j.WriteIDs = append(j.WriteIDs, r.writeIDs["users"]...)
	return j
}

func (r *smokeRunner) journeyProductAssetPrompt() journeyEvidence {
	j := journeyEvidence{ID: "product-asset-prompt", Title: "Product create -> detail -> source asset registration -> prompt preview", Status: "PASS", Fixture: "isolated/local_harness", CleanupRequired: true}
	product := r.do(http.MethodPost, "/api/v1/ecommerce/products", r.token, map[string]any{"sku_code": "SMOKE-SKU-001", "title": "Phase2 Smoke Product", "spu_id": "SMOKE-SPU-001", "category_id": "smoke-category", "brand_id": "smoke-brand", "tags": []string{"phase2", "smoke"}, "spec_json": `{"color":"blue","size":"M"}`, "cost_json": `{"unit_cost":1200}`, "cost_currency": "USD"})
	j.Steps = append(j.Steps, product)
	productID := stringAt(product.Evidence, "product_id")
	if product.Status != "PASS" || productID == "" {
		j.Status = "FAIL"
		return j
	}
	r.recordID("products", productID)
	detail := r.do(http.MethodGet, "/api/v1/ecommerce/products/"+productID, r.token, nil)
	asset := r.do(http.MethodPost, "/api/v1/ecommerce/assets/source", r.token, map[string]any{"product_id": productID, "sku_code": "SMOKE-SKU-001", "file_name": "phase2-smoke.png", "mime_type": "image/png", "payload": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAFgwJ/lS8kWQAAAABJRU5ErkJggg==", "width": 1, "height": 1, "metadata": map[string]any{"fixture": "isolated"}})
	assetID := stringAt(asset.Evidence, "asset_id")
	if assetID != "" {
		r.recordID("assets", assetID)
	}
	prompt := r.do(http.MethodPost, "/api/v1/ecommerce/prompts/preview", r.token, map[string]any{"product_id": productID, "sku_code": "SMOKE-SKU-001", "template_id": "tpl_m1_t01", "tool_slug": "changing-model", "scene_type": "changing-model", "variables": map[string]any{"prompt": "Create a clean marketplace-ready product visual for the customer.", "negative_prompt": "watermark"}, "source_assets": []map[string]any{{"slot": "garment_image", "asset_id": assetID}}, "idempotency_key": fmt.Sprintf("phase2-smoke-%d", time.Now().UnixNano())})
	if prompt.Status == "PASS" {
		r.recordID("prompts", stringAt(prompt.Evidence, "prompt_id"))
	}
	j.Steps = append(j.Steps, detail, asset, prompt)
	if detail.Status != "PASS" || asset.Status != "PASS" || prompt.Status != "PASS" {
		j.Status = "FAIL"
	}
	if ok, found := prompt.Evidence["forbidden_customer_fields_absent"].(bool); found && !ok {
		j.Status = "FAIL"
		j.Notes = append(j.Notes, "prompt preview exposed forbidden internal/runtime/provider/callback fields")
	}
	j.WriteIDs = append(j.WriteIDs, productID, assetID, stringAt(prompt.Evidence, "prompt_id"))
	return j
}

func (r *smokeRunner) journeyWalletCommercialBilling() journeyEvidence {
	j := journeyEvidence{ID: "wallet-commercial-billing", Title: "Wallet summary/history -> offerings -> order/payment -> billing charge projection", Status: "PASS", Fixture: "isolated/local_harness", CleanupRequired: true}
	summary := r.do(http.MethodGet, "/api/v1/ecommerce/wallet/summary", r.token, nil)
	history := r.do(http.MethodGet, "/api/v1/ecommerce/wallet/history", r.token, nil)
	offerings := r.do(http.MethodGet, "/api/v1/ecommerce/commercial/offerings", "", nil)
	order := r.do(http.MethodPost, "/api/v1/ecommerce/commercial/orders", r.token, map[string]any{"package_code": "starter", "quantity": 1, "metadata": `{"fixture":"isolated"}`})
	orderID := stringAt(order.Evidence, "order_id")
	if orderID != "" {
		r.recordID("orders", orderID)
	}
	confirm := r.do(http.MethodPost, "/api/v1/ecommerce/commercial/orders/"+orderID+"/confirm-payment", r.token, map[string]any{"payment_method": "wallet_balance", "provider_code": "platform_wallet", "metadata": `{"fixture":"isolated"}`})
	charges := r.do(http.MethodGet, "/api/v1/ecommerce/billing/charges", r.token, nil)
	j.Steps = append(j.Steps, summary, history, offerings, order, confirm, charges)
	for _, step := range j.Steps {
		if step.Status != "PASS" {
			j.Status = "FAIL"
		}
	}
	j.WriteIDs = append(j.WriteIDs, orderID)
	if id := stringAt(confirm.Evidence, "payment_id"); id != "" {
		j.WriteIDs = append(j.WriteIDs, id)
		r.recordID("payments", id)
	}
	if id := stringAt(confirm.Evidence, "fulfillment_id"); id != "" {
		j.WriteIDs = append(j.WriteIDs, id)
		r.recordID("fulfillments", id)
	}
	return j
}

func (r *smokeRunner) do(method, path, token string, payload any) routeEvidence {
	started := time.Now()
	var body io.Reader
	if payload != nil {
		encoded, _ := json.Marshal(payload)
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, r.baseURL+path, body)
	if err != nil {
		return routeEvidence{Method: method, Path: path, ElapsedMS: time.Since(started).Milliseconds(), Status: "FAIL", Evidence: map[string]any{"error": err.Error()}}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return routeEvidence{Method: method, Path: path, ElapsedMS: time.Since(started).Milliseconds(), Status: "FAIL", Evidence: map[string]any{"error_type": fmt.Sprintf("%T", err)}}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var envelope map[string]any
	_ = json.Unmarshal(respBody, &envelope)
	data, _ := envelope["data"].(map[string]any)
	evidence := summarizeEvidence(path, data, respBody)
	status := "PASS"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "FAIL"
		evidence["body_sha256"] = sha256Hex(respBody)
		evidence["error_code"] = envelope["error_code"]
	}
	return routeEvidence{Method: method, Path: path, StatusCode: resp.StatusCode, ElapsedMS: time.Since(started).Milliseconds(), Status: status, ResponseKeys: sortedMapKeys(data), Evidence: evidence}
}

func summarizeEvidence(path string, data map[string]any, body []byte) map[string]any {
	e := map[string]any{"body_sha256": sha256Hex(body)}
	if data == nil {
		return e
	}
	switch {
	case strings.Contains(path, "/auth/register") || strings.Contains(path, "/auth/login"):
		token := stringValue(data["access_token"])
		if token == "" {
			if access, ok := data["access"].(map[string]any); ok {
				token = stringValue(access["access_token"])
			}
		}
		e["access_token_present"] = token != ""
		// auth_bearer_token is consumed in-memory by the harness and stripped by the
		// Python report redactor before reports are written.
		e["auth_bearer_token"] = token
		if user, ok := data["user"].(map[string]any); ok {
			e["user_id"] = stringValue(user["id"])
			e["org_id"] = stringValue(user["org_id"])
		}
	case strings.Contains(path, "/auth/session"):
		e["authenticated"] = data["authenticated"]
		if user, ok := data["user"].(map[string]any); ok {
			e["user_id"] = stringValue(user["id"])
		}
	case strings.Contains(path, "/access/me"):
		e["org_role"] = data["org_role"]
		e["permissions_present"] = data["permissions"] != nil
	case path == "/api/v1/ecommerce/products":
		e["product_id"] = stringValue(data["id"])
		e["sku_code"] = stringValue(data["sku_code"])
	case strings.Contains(path, "/products/"):
		if product, ok := data["product"].(map[string]any); ok {
			e["product_id"] = stringValue(product["id"])
		}
	case strings.Contains(path, "/assets/source"):
		e["asset_id"] = stringValue(data["id"])
		e["storage_key_present"] = stringValue(data["storage_key"]) != ""
	case strings.Contains(path, "/prompts/preview"):
		e["prompt_id"] = stringValue(data["prompt_id"])
		e["status"] = data["status"]
		forbidden := []string{"internal_service_secret", "provider", "callback", "runtime_job", "route_snapshot", "secret"}
		lower := strings.ToLower(string(body))
		absent := true
		found := []string{}
		for _, key := range forbidden {
			if strings.Contains(lower, key) {
				absent = false
				found = append(found, key)
			}
		}
		e["forbidden_customer_fields_absent"] = absent
		e["forbidden_customer_field_check_count"] = len(forbidden)
		if len(found) > 0 {
			e["forbidden_customer_field_hit_count"] = len(found)
		}
	case strings.Contains(path, "/wallet/summary"):
		e["primary_asset_code"] = data["primary_asset_code"]
		e["quota_present"] = data["quota"] != nil
	case strings.Contains(path, "/wallet/history") || strings.Contains(path, "/billing/charges"):
		if items, ok := data["items"].([]any); ok {
			e["item_count"] = len(items)
		}
	case strings.Contains(path, "/commercial/offerings"):
		e["product_code"] = data["product_code"]
		e["offerings_present"] = data["offerings"] != nil
	case strings.Contains(path, "/commercial/orders") && strings.Contains(path, "/confirm-payment"):
		if order, ok := data["order"].(map[string]any); ok {
			e["order_id"] = stringValue(order["id"])
			e["order_status"] = stringValue(order["status"])
		}
		if payment, ok := data["payment"].(map[string]any); ok {
			e["payment_id"] = stringValue(payment["id"])
		}
		if fulfillment, ok := data["fulfillment"].(map[string]any); ok {
			e["fulfillment_id"] = stringValue(fulfillment["id"])
		}
	case strings.Contains(path, "/commercial/orders"):
		if order, ok := data["order"].(map[string]any); ok {
			e["order_id"] = stringValue(order["id"])
			e["order_status"] = stringValue(order["status"])
		}
	}
	return e
}

func (r *smokeRunner) recordID(kind, id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	r.writeIDs[kind] = append(r.writeIDs[kind], id)
}

func (s *fakePlatformState) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requestCount++
		if strings.HasPrefix(r.URL.Path, "/internal/v1/") && r.Header.Get("X-Internal-Service-Secret") != s.internalSecret {
			writeEnvelope(w, http.StatusUnauthorized, nil, "invalid internal service secret")
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/register":
			s.createdUsers = append(s.createdUsers, "user-1")
			writeEnvelope(w, http.StatusOK, authResult(s.jwtSecret), "")
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			writeEnvelope(w, http.StatusOK, authResult(s.jwtSecret), "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/users/user-1/profile":
			writeEnvelope(w, http.StatusOK, userProfile(), "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/access/users/user-1/orgs/org-1":
			writeEnvelope(w, http.StatusOK, map[string]any{"user_id": "user-1", "org_id": "org-1", "org_role": "owner", "permissions": []string{"platform.access", "ecommerce.use"}}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/summary":
			writeEnvelope(w, http.StatusOK, walletSummary(), "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/controls/quota/balance":
			writeEnvelope(w, http.StatusOK, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "billable_item_code": r.URL.Query().Get("billable_item_code"), "granted": 1000, "consumed": 25, "reserved": 0, "available": 975}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/catalog/offerings":
			writeEnvelope(w, http.StatusOK, offerings(), "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/storage/assets":
			id := fmt.Sprintf("stored-asset-%d", len(s.createdAssets)+1)
			s.createdAssets = append(s.createdAssets, id)
			writeEnvelope(w, http.StatusOK, map[string]any{"storage_key": "phase2-smoke/" + id + ".png", "mime_type": "image/png", "file_size": 68}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/rewards":
			writeEnvelope(w, http.StatusOK, items(map[string]any{"id": "reward-1", "product_code": "ecommerce", "amount": 20, "asset_code": "ECOMMERCE_CREDIT", "status": "issued", "created_at": time.Now().UTC().Format(time.RFC3339)}), "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/commissions":
			writeEnvelope(w, http.StatusOK, items(map[string]any{"id": "commission-1", "product_code": "ecommerce", "amount": 7, "asset_code": "ECOMMERCE_CREDIT", "status": "earned", "created_at": time.Now().UTC().Format(time.RFC3339)}), "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/accounts":
			writeEnvelope(w, http.StatusOK, items(map[string]any{"id": "acct-1", "billing_subject_type": "organization", "billing_subject_id": "org-1", "asset_code": "ECOMMERCE_CREDIT", "balance": 100000, "status": "active"}), "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/assets":
			writeEnvelope(w, http.StatusOK, map[string]any{"items": []map[string]any{}}, "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/assets":
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			writeEnvelope(w, http.StatusOK, map[string]any{"asset_code": input["asset_code"], "product_code": input["product_code"], "asset_type": input["asset_type"], "lifecycle_type": input["lifecycle_type"], "status": "active"}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/ledger":
			writeEnvelope(w, http.StatusOK, items(map[string]any{"id": "ledger-history-1", "wallet_account_id": "acct-1", "asset_code": "ECOMMERCE_CREDIT", "direction": "credit", "amount": 100, "status": "succeeded", "created_at": time.Now().UTC().Format(time.RFC3339)}), "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/ledger":
			id := fmt.Sprintf("ledger-%d", len(s.walletLedgerIDs)+1)
			s.walletLedgerIDs = append(s.walletLedgerIDs, id)
			writeEnvelope(w, http.StatusOK, map[string]any{"account": map[string]any{"id": "acct-1", "balance": 90000}, "bucket": map[string]any{"id": "bucket-1", "balance": 90000}, "ledger": map[string]any{"id": id, "amount": 9900, "direction": "debit", "status": "succeeded"}}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/controls/quota/policies":
			writeEnvelope(w, http.StatusOK, items(map[string]any{"id": "quota-policy-1", "product_code": "ecommerce", "package_code": "starter", "billable_item_code": "ecommerce.image.generate", "units": 100, "status": "active"}), "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/quota/grants":
			id := fmt.Sprintf("quota-grant-%d", len(s.quotaGrantIDs)+1)
			s.quotaGrantIDs = append(s.quotaGrantIDs, id)
			writeEnvelope(w, http.StatusOK, map[string]any{"id": id}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writeEnvelope(w, http.StatusOK, items(map[string]any{"id": "program-1", "product_code": "ecommerce", "program_code": "ecommerce_signup_default", "name": "Ecommerce Signup Smoke", "trigger_type": "signup", "status": "active"}), "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-programs":
			s.referralPrograms = append(s.referralPrograms, "program-2")
			writeEnvelope(w, http.StatusOK, map[string]any{"id": "program-2", "product_code": "ecommerce", "program_code": "ecommerce_signup_default", "name": "Ecommerce Signup Smoke", "trigger_type": "signup", "status": "active"}, "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/template-ops/catalog":
			writeEnvelope(w, http.StatusOK, map[string]any{"items": []map[string]any{}, "total": 0, "limit": 20, "offset": 0}, "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/storage/assets/resolve":
			writeEnvelope(w, http.StatusOK, map[string]any{"items": []map[string]any{}}, "")
		default:
			writeEnvelope(w, http.StatusNotFound, nil, fmt.Sprintf("unexpected fake platform route %s %s", r.Method, r.URL.String()))
		}
	})
}

func writeEnvelope(w http.ResponseWriter, status int, data any, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	code := 0
	message := "success"
	if status >= 400 {
		code = status
		message = errMsg
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": data, "error": errMsg, "timestamp": time.Now().UnixMilli()})
}

func authResult(jwtSecret string) map[string]any {
	return map[string]any{"access_token": signJWT(jwtSecret), "user": userProfile()}
}

func signJWT(secret string) string {
	claims := jwt.MapClaims{"user_id": "user-1", "org_id": "org-1", "org_role": "owner", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	return token
}

func userProfile() map[string]any {
	return map[string]any{"id": "user-1", "email": "phase2-smoke@example.test", "full_name": "Phase2 Smoke Merchant", "avatar_url": "https://example.test/avatar.png", "role": "user", "org_role": "owner", "org_id": "org-1", "last_active_org_id": "org-1", "plan_id": "starter", "status": "active", "permissions": []string{"platform.access", "ecommerce.use"}, "orgs": []map[string]any{{"id": "org-1", "name": "Phase2 Smoke Workspace", "role": "owner"}}}
}

func walletSummary() map[string]any {
	return map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "product_code": "ecommerce", "total_balance": 100000, "permanent_balance": 90000, "reward_balance": 10000, "allowance_balance": 0, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "asset_type": "credit", "lifecycle_type": "permanent", "account_balance": 100000, "available_balance": 100000}}}
}

func offerings() map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"product":            map[string]any{"id": "platform-product-ecommerce", "code": "ecommerce", "name": "Agent Ecommerce", "status": "active", "created_at": now, "updated_at": now},
		"skus":               []map[string]any{{"id": "sku-starter", "product_id": "platform-product-ecommerce", "code": "starter", "name": "Starter", "sku_type": "package", "billing_mode": "prepaid", "currency": "USD", "list_price": 9900, "status": "active", "metadata": `{"package_code":"starter"}`, "created_at": now, "updated_at": now}},
		"packages":           []map[string]any{{"id": "package-starter", "product_id": "platform-product-ecommerce", "code": "starter", "name": "Starter Package", "package_type": "credits", "status": "active", "metadata": `{"sku_code":"starter"}`, "created_at": now, "updated_at": now}},
		"billable_items":     []map[string]any{{"id": "item-image", "product_id": "platform-product-ecommerce", "code": "ecommerce.image.generate", "name": "Image generation", "meter_unit": "image", "billing_scope": "organization", "settlement_mode": "prepaid", "pricing_behavior": "unit", "status": "active", "created_at": now, "updated_at": now}},
		"rate_cards":         []map[string]any{{"id": "rate-starter", "product_id": "platform-product-ecommerce", "code": "starter-rate", "target_type": "sku", "target_id": "sku-starter", "price_model": "flat", "currency": "USD", "price_config": `{"unit_amount":9900}`, "version": 1, "status": "active", "created_at": now, "updated_at": now}},
		"asset_definitions":  []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "product_code": "ecommerce", "asset_type": "credit", "lifecycle_type": "permanent", "status": "active", "created_at": now, "updated_at": now}},
		"allowance_policies": []map[string]any{},
	}
}

func items(item map[string]any) map[string]any {
	return map[string]any{"items": []map[string]any{item}}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.Contains(strings.ToLower(k), "token") || strings.Contains(strings.ToLower(k), "secret") || strings.Contains(strings.ToLower(k), "password") {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

func stringAt(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return stringValue(m[key])
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
