package billing

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newBillingRepo(t *testing.T) *repository.CommercialRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "billing.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.BillingChargeRecord{}, &models.CommercialEventOutbox{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return repository.NewCommercialRepository(db)
}

func writeBillingEnvelope(t *testing.T, w http.ResponseWriter, status, code int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "", "request_id": "req-billing", "timestamp": time.Now().Unix(), "data": data})
}

func TestRecordChargeIsIdempotentAndReplayOutboxRetriesPlatformFailure(t *testing.T) {
	repo := newBillingRepo(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/incentives/channel-events/charges" || r.Method != http.MethodPost {
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.String())
		}
		calls++
		if calls == 1 {
			writeBillingEnvelope(t, w, http.StatusBadGateway, 2001, nil)
			return
		}
		writeBillingEnvelope(t, w, http.StatusOK, 0, map[string]any{"matched": true, "status": "earned", "ledger": map[string]any{"id": "channel-ledger-1", "status": "earned", "commission_amount": 7, "created_at": time.Now().UTC().Format(time.RFC3339)}})
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "billing-test", InternalServiceSecret: "secret"})
	svc := NewService(client, repo, config.AppConfig{ProductCode: "ecommerce"})

	input := RecordChargeInput{OrganizationID: "org-billing", UserID: "user-billing", EventID: "event-charge-1", BusinessType: "image_generation", BillableItemCode: "ecommerce.image.generate", Currency: "CNY", GrossAmount: 100, NetAmount: 80, WalletDebited: 80, Status: "settled", OccurredAt: "2026-06-07T12:00:00Z", ChannelCharge: &ChannelChargeInput{AppliesTo: "image_generation", SourceOrderID: "order-1"}}
	first, err := svc.RecordCharge(input)
	if err != nil {
		t.Fatalf("RecordCharge first: %v", err)
	}
	if first.ChannelStatus != "failed" || first.ChannelError == "" {
		t.Fatalf("expected failed channel status and outbox-worthy error, got %+v", first)
	}
	second, err := svc.RecordCharge(input)
	if err != nil {
		t.Fatalf("RecordCharge duplicate: %v", err)
	}
	if second.ID != first.ID || calls != 1 {
		t.Fatalf("duplicate event was not idempotent: first=%s second=%s calls=%d", first.ID, second.ID, calls)
	}

	replay, err := svc.ReplayOutbox(10)
	if err != nil {
		t.Fatalf("ReplayOutbox: %v", err)
	}
	if replay.Processed != 1 || replay.Succeeded != 1 || replay.Failed != 0 || calls != 2 {
		t.Fatalf("unexpected replay result: %+v calls=%d", replay, calls)
	}
	refreshed, err := repo.GetBillingChargeRecord(first.ID)
	if err != nil || refreshed.ChannelStatus != "succeeded" || refreshed.ChannelLedgerID != "channel-ledger-1" {
		t.Fatalf("charge not updated after replay: record=%+v err=%v", refreshed, err)
	}
}

func TestSummaryAndRefundStayOrgScoped(t *testing.T) {
	repo := newBillingRepo(t)
	svc := NewService(platform.New(config.PlatformConfig{BaseURL: "http://127.0.0.1:1", Timeout: time.Millisecond, InternalServiceSecret: "secret"}), repo, config.AppConfig{ProductCode: "ecommerce"})
	charge, err := svc.RecordCharge(RecordChargeInput{OrganizationID: "org-a", EventID: "event-a", BusinessType: "runtime", NetAmount: 11, CreditsConsumed: 3, WalletDebited: 8, Status: "settled"})
	if err != nil {
		t.Fatalf("RecordCharge org-a: %v", err)
	}
	if _, err := svc.RecordCharge(RecordChargeInput{OrganizationID: "org-b", EventID: "event-b", BusinessType: "runtime", NetAmount: 99, Status: "settled"}); err != nil {
		t.Fatalf("RecordCharge org-b: %v", err)
	}
	if _, err := svc.RefundCharge(charge.ID, RefundChargeInput{RefundEventID: "refund-a", RefundAmount: 11, RefundType: "full"}); err != nil {
		t.Fatalf("RefundCharge: %v", err)
	}
	summary, err := svc.Summary("org-a")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.ChargeCount != 1 || summary.RefundedCount != 1 || summary.TotalNetAmount != 11 || summary.TotalWalletDebited != 8 || summary.TotalCreditsConsumed != 3 {
		t.Fatalf("unexpected org-scoped summary: %+v", summary)
	}
}

func TestHandlerRecordChargeEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(NewService(platform.New(config.PlatformConfig{BaseURL: "http://127.0.0.1:1", Timeout: time.Millisecond, InternalServiceSecret: "secret"}), newBillingRepo(t), config.AppConfig{ProductCode: "ecommerce"}))
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.POST("/charges", h.RecordCharge)

	bad := httptest.NewRecorder()
	r.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/charges", strings.NewReader(`{"event_id":"missing-org"}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad request status = %d body=%s", bad.Code, bad.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/charges", bytes.NewBufferString(`{"organization_id":"org-handler","event_id":"event-handler","business_type":"runtime","net_amount":5}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-billing-handler")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"request_id":"req-billing-handler"`) || !strings.Contains(w.Body.String(), `"EventID":"event-handler"`) {
		t.Fatalf("unexpected record charge response: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandlersSummaryListRefundAndReplayOutbox(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newBillingRepo(t)
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	charge, err := NewService(platform.New(config.PlatformConfig{BaseURL: "http://127.0.0.1:1", Timeout: time.Millisecond, InternalServiceSecret: "secret"}), repo, config.AppConfig{ProductCode: "ecommerce"}).RecordCharge(RecordChargeInput{OrganizationID: "org-handler", EventID: "event-handler-list", BusinessType: "runtime", NetAmount: 42, WalletDebited: 40, CreditsConsumed: 2, Status: "settled", OccurredAt: now.Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("seed charge: %v", err)
	}
	refundCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/incentives/channel-events/refunds" {
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.String())
		}
		refundCalls++
		var req platform.RecordChannelRefundInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode refund payload: %v", err)
		}
		if req.ProductCode != "ecommerce" || req.OrgID != "org-handler" || req.SourceChargeID != charge.ID || req.RefundAmount != 42 {
			t.Fatalf("unexpected refund payload: %+v", req)
		}
		writeBillingEnvelope(t, w, http.StatusOK, 0, map[string]any{"matched": true, "status": "reversed", "ledger": map[string]any{"id": "refund-ledger", "status": "reversed", "created_at": now.Format(time.RFC3339)}})
	}))
	defer server.Close()
	h := NewHandler(NewService(platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "billing-handler-test", InternalServiceSecret: "secret"}), repo, config.AppConfig{ProductCode: "ecommerce"}))
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.GET("/summary", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.Summary(c) })
	r.GET("/charges", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.ListCharges(c) })
	r.POST("/charges/:recordID/refunds", h.RefundCharge)
	r.POST("/outbox/replay", h.ReplayOutbox)

	for _, tc := range []struct {
		method string
		path   string
		body   string
		want   int
		needle string
	}{
		{http.MethodGet, "/summary", "", http.StatusOK, `"charge_count":1`},
		{http.MethodGet, "/charges?limit=1", "", http.StatusOK, `"event-handler-list"`},
		{http.MethodPost, "/charges/" + charge.ID + "/refunds", `{"refund_event_id":"refund-handler","refund_amount":42,"refund_type":"full","occurred_at":"2026-06-07T12:01:00Z"}`, http.StatusOK, `"Status":"refunded"`},
		{http.MethodPost, "/outbox/replay", `{}`, http.StatusOK, `"processed":0`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.needle) {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if refundCalls != 1 {
		t.Fatalf("refund platform calls = %d, want 1", refundCalls)
	}
}

func TestRefundOutboxFailureAndReplayUnknownEventAreObservable(t *testing.T) {
	repo := newBillingRepo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeBillingEnvelope(t, w, http.StatusBadGateway, 2001, nil)
	}))
	defer server.Close()
	svc := NewService(platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "billing-test", InternalServiceSecret: "secret"}), repo, config.AppConfig{ProductCode: "ecommerce"})
	charge, err := svc.RecordCharge(RecordChargeInput{OrganizationID: "org-refund", EventID: "event-refund-fail", BusinessType: "runtime", NetAmount: 20, Status: "settled"})
	if err != nil {
		t.Fatalf("RecordCharge: %v", err)
	}
	if _, err := svc.RefundCharge(charge.ID, RefundChargeInput{RefundEventID: "refund-fail", RefundAmount: 20, RefundType: "full"}); err != nil {
		t.Fatalf("RefundCharge should persist refund even when platform refund callback fails: %v", err)
	}
	refreshed, err := repo.GetBillingChargeRecord(charge.ID)
	if err != nil || refreshed.ChannelStatus != "failed" || refreshed.ChannelError == "" {
		t.Fatalf("refund failure not recorded on charge: record=%+v err=%v", refreshed, err)
	}
	if err := repo.CreateOutboxEvent(&models.CommercialEventOutbox{ProductCode: "ecommerce", OrganizationID: "org-refund", EventType: "unknown_event", AggregateType: "billing_charge_record", AggregateID: charge.ID, Status: "pending", PayloadJSON: `{}`, AvailableAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("seed unknown outbox: %v", err)
	}
	replay, err := svc.ReplayOutbox(10)
	if err != nil {
		t.Fatalf("ReplayOutbox returns aggregate result, not first failure: %v", err)
	}
	if replay.Processed != 2 || replay.Failed != 2 {
		t.Fatalf("unexpected replay failure accounting: %+v", replay)
	}
}
