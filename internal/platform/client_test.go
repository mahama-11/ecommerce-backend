package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
)

func newTestClient(server *httptest.Server) *Client {
	return New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "v-ecommerce-backend-test", InternalServiceSecret: "test-internal-secret", JWTSecret: "jwt-secret"})
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, status int, code int, data any, message, errorCode, errorHint string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "error_code": errorCode, "error_hint": errorHint, "request_id": "req-test", "timestamp": time.Now().Unix(), "data": data})
}

func TestClientDecodesPublicAuthAndInternalWalletContracts(t *testing.T) {
	seenInternalSecret := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"access_token": "token-1", "user": map[string]any{"id": "user-1", "email": "merchant@example.com", "full_name": "Merchant", "org_id": "org-1", "org_role": "owner", "status": "active"}}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/summary":
			seenInternalSecret = r.Header.Get("X-Internal-Service-Secret")
			if r.URL.Query().Get("product_code") != "ecommerce" {
				t.Fatalf("product_code query = %q", r.URL.Query().Get("product_code"))
			}
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "product_code": "ecommerce", "total_balance": 100, "permanent_balance": 80, "reward_balance": 20, "allowance_balance": 0, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "balance": 100}}}, "", "", "")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := newTestClient(server)
	login, err := client.Login(AuthLoginInput{Email: "merchant@example.com", Password: "secret"})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if login.AccessToken != "token-1" || login.User.OrgID != "org-1" {
		t.Fatalf("unexpected login projection: %+v", login)
	}
	wallet, err := client.GetWalletSummary("organization", "org-1", "ecommerce")
	if err != nil {
		t.Fatalf("GetWalletSummary returned error: %v", err)
	}
	if wallet.TotalBalance != 100 || wallet.Assets[0].AssetCode != "ECOMMERCE_CREDIT" {
		t.Fatalf("unexpected wallet projection: %+v", wallet)
	}
	if seenInternalSecret != "test-internal-secret" {
		t.Fatalf("internal service secret header not sent")
	}
}

func TestClientMapsPlatformErrorEnvelopeWithoutLeakingSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusConflict, 409, nil, "already exists", "ACCOUNT_EXISTS", "Use login instead.")
	}))
	defer server.Close()

	_, err := newTestClient(server).Register(AuthRegisterInput{Email: "merchant@example.com", Password: "secret"})
	if err == nil {
		t.Fatalf("expected platform error")
	}
	if !IsConflict(err) || ErrorCode(err) != "ACCOUNT_EXISTS" || ErrorHint(err) != "Use login instead." {
		t.Fatalf("platform error helpers did not preserve contract semantics: %v", err)
	}
	if strings.Contains(err.Error(), "test-internal-secret") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked secret material: %s", err.Error())
	}
}

func TestClientRejectsMalformedPlatformEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	_, err := newTestClient(server).GetCatalogOfferings("ecommerce")
	if err == nil {
		t.Fatalf("expected malformed envelope error")
	}
}
