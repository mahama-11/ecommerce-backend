package commission

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/platform"

	"github.com/gin-gonic/gin"
)

func writeCommissionEnvelope(t *testing.T, w http.ResponseWriter, status, code int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "", "request_id": "req-commission", "timestamp": time.Now().Unix(), "data": data})
}

func TestOverviewSortsAndAggregatesReferralCommissions(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/incentives/commissions" || r.URL.Query().Get("beneficiary_subject_id") != "org-commission" || r.URL.Query().Get("product_code") != "ecommerce" {
			t.Fatalf("unexpected commission request: %s", r.URL.String())
		}
		writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{
			{"id": "pending", "product_code": "ecommerce", "currency": "ECOMMERCE_CREDIT", "amount": 5, "status": "pending", "created_at": now.Add(-2 * time.Hour).Format(time.RFC3339)},
			{"id": "earned", "product_code": "ecommerce", "currency": "ECOMMERCE_CREDIT", "amount": 7, "status": "earned", "created_at": now.Format(time.RFC3339)},
			{"id": "redeemed", "product_code": "ecommerce", "currency": "ECOMMERCE_CREDIT", "amount": 3, "status": "redeemed", "created_at": now.Add(-1 * time.Hour).Format(time.RFC3339)},
			{"id": "reversed", "product_code": "ecommerce", "currency": "ECOMMERCE_CREDIT", "amount": 2, "status": "reversed", "created_at": now.Add(-3 * time.Hour).Format(time.RFC3339)},
		}})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commission-test", InternalServiceSecret: "secret"})
	got, err := NewService(client, config.AppConfig{ProductCode: "ecommerce", RewardAssetCode: "ECOMMERCE_PROMO_CREDIT"}).Overview("org-commission", "")
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if got.Commissions[0].ID != "earned" || got.TotalCommission != 17 || got.EarnedCommission != 7 || got.PendingCommission != 5 || got.RedeemedCommission != 3 || got.ReversedCommission != 2 || got.RedeemableCommission != 7 {
		t.Fatalf("unexpected overview: %+v", got)
	}
}

func TestRedeemPostsOrganizationScopedProductProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/incentives/commissions/redeem" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var req platform.RedeemCommissionsInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode redeem request: %v", err)
		}
		if req.ProductCode != "ecommerce" || req.BeneficiarySubjectType != "organization" || req.BeneficiarySubjectID != "org-commission" || req.AssetCode != "ECOMMERCE_PROMO_CREDIT" || len(req.CommissionIDs) != 2 {
			t.Fatalf("unexpected redeem payload: %+v", req)
		}
		writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"reward_ledger_id": "reward-redeem", "asset_code": "ECOMMERCE_PROMO_CREDIT", "total_amount": 12, "commissions": []map[string]any{{"id": "c1", "amount": 7, "status": "redeemed"}}})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commission-test", InternalServiceSecret: "secret"})
	got, err := NewService(client, config.AppConfig{ProductCode: "ecommerce", RewardAssetCode: "ECOMMERCE_PROMO_CREDIT"}).Redeem("org-commission", RedeemInput{CommissionIDs: []string{"c1", "c2"}, Metadata: `{"source":"unit"}`})
	if err != nil || got.RewardLedgerID != "reward-redeem" || got.TotalAmount != 12 {
		t.Fatalf("unexpected redeem result: got=%+v err=%v", got, err)
	}
}

func TestChannelOverviewDoesNotLeakUnboundPartners(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/incentives/channel-bindings":
			if r.URL.Query().Get("org_id") != "org-channel" {
				t.Fatalf("unexpected org_id: %s", r.URL.RawQuery)
			}
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "bind-1", "product_code": "ecommerce", "org_id": "org-channel", "channel_partner_id": "partner-bound", "channel_program_id": "program-1", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-partners":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "partner-bound", "code": "BOUND", "name": "Bound", "status": "active", "risk_level": "low", "created_at": now}, {"id": "partner-other", "code": "OTHER", "name": "Other", "status": "active", "risk_level": "low", "created_at": now}}})
		case "/internal/v1/incentives/channel-programs":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "program-1", "product_code": "ecommerce", "program_code": "ecom-channel", "name": "Channel", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-commissions", "/internal/v1/incentives/channel-settlement-batches":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{}})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commission-test", InternalServiceSecret: "secret"})
	got, err := NewService(client, config.AppConfig{ProductCode: "ecommerce"}).ChannelOverview("org-channel")
	if err != nil {
		t.Fatalf("ChannelOverview: %v", err)
	}
	if len(got.Partners) != 1 || got.Partners[0].ID != "partner-bound" || strings.Contains(got.Partners[0].Code, "OTHER") {
		t.Fatalf("unbound partner leaked into channel overview: %+v", got.Partners)
	}
}

func TestHandlerRedeemEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"reward_ledger_id": "reward-handler", "asset_code": "ECOMMERCE_PROMO_CREDIT", "total_amount": 5})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commission-handler-test", InternalServiceSecret: "secret"})
	h := NewHandler(NewService(client, config.AppConfig{ProductCode: "ecommerce"}), nil)
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.POST("/redeem", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.Redeem(c) })
	req := httptest.NewRequest(http.MethodPost, "/redeem", bytes.NewBufferString(`{"commission_ids":["c1"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-commission-handler")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"request_id":"req-commission-handler"`) || !strings.Contains(w.Body.String(), `"reward_ledger_id":"reward-handler"`) {
		t.Fatalf("unexpected redeem response: status=%d body=%s", w.Code, w.Body.String())
	}
}
