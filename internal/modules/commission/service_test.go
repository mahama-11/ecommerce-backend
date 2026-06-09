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

func TestChannelCommissionsAndSettlementsAreFilteredToBoundOrgPartners(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/incentives/channel-bindings":
			if r.URL.Query().Get("product_code") != "ecommerce" || r.URL.Query().Get("org_id") != "org-settlement" {
				t.Fatalf("unexpected binding query: %s", r.URL.RawQuery)
			}
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "bind-bound", "product_code": "ecommerce", "org_id": "org-settlement", "channel_partner_id": "partner-bound", "channel_program_id": "program-1", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-partners":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "partner-bound", "code": "BOUND", "name": "Bound", "status": "active", "created_at": now}, {"id": "partner-other", "code": "OTHER", "name": "Other", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-programs":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "program-1", "product_code": "ecommerce", "program_code": "ecom-channel", "name": "Channel", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-commissions":
			if r.URL.Query().Get("channel_partner_id") != "partner-bound" {
				t.Fatalf("unexpected commission partner query: %s", r.URL.RawQuery)
			}
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "ledger-bound", "product_code": "ecommerce", "channel_partner_id": "partner-bound", "channel_program_id": "program-1", "commission_amount": 33, "status": "earned", "created_at": now}}})
		case "/internal/v1/incentives/channel-settlement-batches":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "batch-1", "product_code": "ecommerce", "channel_program_id": "program-1", "currency": "CNY", "status": "generated", "created_at": now}}})
		case "/internal/v1/incentives/channel-settlement-batches/batch-1":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"batch": map[string]any{"id": "batch-1", "product_code": "ecommerce", "channel_program_id": "program-1", "currency": "CNY", "status": "generated", "created_at": now}, "items": []map[string]any{{"item": map[string]any{"id": "settle-bound", "settlement_batch_id": "batch-1", "channel_partner_id": "partner-bound", "currency": "CNY", "commission_amount": 33, "net_amount": 30, "status": "pending", "created_at": now}}, {"item": map[string]any{"id": "settle-other", "settlement_batch_id": "batch-1", "channel_partner_id": "partner-other", "currency": "CNY", "commission_amount": 99, "net_amount": 90, "status": "pending", "created_at": now}}}})
		default:
			t.Fatalf("unexpected channel request: %s", r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commission-channel-test", InternalServiceSecret: "secret"})
	svc := NewService(client, config.AppConfig{ProductCode: "ecommerce"})
	commissions, err := svc.ListChannelCommissions("org-settlement", "earned")
	if err != nil {
		t.Fatalf("ListChannelCommissions: %v", err)
	}
	if len(commissions) != 1 || commissions[0].Partner.ID != "partner-bound" || commissions[0].Ledger.ID != "ledger-bound" || commissions[0].Program.ProgramCode != "ecom-channel" {
		t.Fatalf("unexpected channel commissions: %+v", commissions)
	}
	settlements, err := svc.ListChannelSettlements("org-settlement", "generated")
	if err != nil {
		t.Fatalf("ListChannelSettlements: %v", err)
	}
	if len(settlements) != 1 || settlements[0].Partner.ID != "partner-bound" || settlements[0].Item.ID != "settle-bound" || settlements[0].Batch.ID != "batch-1" {
		t.Fatalf("settlement crossed org/partner scope: %+v", settlements)
	}
}

func TestHandlersExposeCommissionReadModelsWithEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/incentives/commissions":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "commission-earned", "product_code": "ecommerce", "currency": "ECOMMERCE_CREDIT", "amount": 8, "status": "earned", "created_at": now}}})
		case "/internal/v1/incentives/channel-bindings":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "binding-1", "product_code": "ecommerce", "org_id": "org-handler", "channel_partner_id": "partner-1", "channel_program_id": "program-1", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-partners":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "partner-1", "code": "P1", "name": "Partner", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-programs":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "program-1", "product_code": "ecommerce", "program_code": "ecom-channel", "name": "Channel", "status": "active", "created_at": now}}})
		case "/internal/v1/incentives/channel-commissions":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "channel-ledger", "product_code": "ecommerce", "channel_partner_id": "partner-1", "channel_program_id": "program-1", "commission_amount": 12, "status": "settled", "created_at": now}}})
		case "/internal/v1/incentives/channel-settlement-batches":
			writeCommissionEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{}})
		default:
			t.Fatalf("unexpected handler request: %s", r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commission-handler-read-test", InternalServiceSecret: "secret"})
	h := NewHandler(NewService(client, config.AppConfig{ProductCode: "ecommerce"}), nil)
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.GET("/overview", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.Overview(c) })
	r.GET("/referrals", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ListReferralCommissions(c) })
	r.GET("/channel/overview", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ChannelOverview(c) })
	r.GET("/channel/bindings", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ChannelBindings(c) })
	r.GET("/channel/commissions", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ChannelCommissions(c) })
	r.GET("/channel/settlements", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ChannelSettlements(c) })

	for _, tc := range []struct{ path, needle string }{
		{"/overview", `"redeemable_commission":8`},
		{"/referrals", `"id":"commission-earned"`},
		{"/channel/overview", `"total_commission":12`},
		{"/channel/bindings", `"id":"binding-1"`},
		{"/channel/commissions", `"id":"channel-ledger"`},
		{"/channel/settlements", `[]`},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("X-Request-ID", "req-commission-read-handler")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.needle) || !strings.Contains(w.Body.String(), `"request_id":"req-commission-read-handler"`) {
			t.Fatalf("GET %s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
	}
}
