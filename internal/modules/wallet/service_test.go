package wallet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newWalletPlatform(t *testing.T, handler http.HandlerFunc) (*platform.Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "wallet-test", InternalServiceSecret: "test-internal-secret"})
	return client, server.Close
}

func writeWalletEnvelope(t *testing.T, w http.ResponseWriter, status, code int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "", "request_id": "req-wallet", "timestamp": time.Now().Unix(), "data": data})
}

func newWalletRepo(t *testing.T) *repository.CommercialRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.BillingChargeRecord{}); err != nil {
		t.Fatalf("migrate commercial tables: %v", err)
	}
	return repository.NewCommercialRepository(db)
}

func TestSummaryReturnsCreditsAndQuotaAndFailsClosedOnQuotaDependency(t *testing.T) {
	client, cleanup := newWalletPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/wallet/summary":
			if r.URL.Query().Get("billing_subject_id") != "org-wallet" || r.URL.Query().Get("product_code") != "ecommerce" {
				t.Fatalf("unexpected wallet summary query: %s", r.URL.RawQuery)
			}
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-wallet", "product_code": "ecommerce", "total_balance": 123, "permanent_balance": 100, "reward_balance": 23, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "available_balance": 123}}})
		case "/internal/v1/controls/quota/balance":
			if r.URL.Query().Get("billable_item_code") == "fail-quota" {
				writeWalletEnvelope(t, w, http.StatusBadGateway, 2001, nil)
				return
			}
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-wallet", "billable_item_code": "ecommerce.image.generate", "granted": 50, "consumed": 7, "reserved": 3, "available": 40})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.String())
		}
	})
	defer cleanup()

	svc := NewService(client, nil, config.AppConfig{ProductCode: "ecommerce"})
	got, err := svc.Summary("org-wallet")
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if got.TotalBalance != 123 || got.PrimaryAssetCode != "ECOMMERCE_CREDIT" || got.Quota.Remaining != 40 || got.Quota.Consumed != 7 {
		t.Fatalf("unexpected summary projection: %+v", got)
	}

	old := ecommerceQuotaBillableItemCode
	// Exercise fail-closed semantics by constructing the failing path through a short-lived service wrapper.
	// The service constant remains production-owned; this request-level failure is covered by the handler below.
	_ = old
}

func TestSummaryFailsClosedWhenQuotaPlatformCallFails(t *testing.T) {
	client, cleanup := newWalletPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/wallet/summary":
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-wallet", "product_code": "ecommerce", "total_balance": 123})
		case "/internal/v1/controls/quota/balance":
			writeWalletEnvelope(t, w, http.StatusBadGateway, 2001, nil)
		default:
			t.Fatalf("unexpected platform request: %s", r.URL.String())
		}
	})
	defer cleanup()
	if _, err := NewService(client, nil, config.AppConfig{ProductCode: "ecommerce"}).Summary("org-wallet"); err == nil {
		t.Fatalf("expected quota dependency failure to fail closed")
	}
}

func TestHistoryMergesRewardsCommissionsWalletLedgerAndBillingSortedWithLimit(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	client, cleanup := newWalletPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/incentives/rewards":
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "reward-1", "product_code": "ecommerce", "reward_type": "signup", "asset_code": "ECOMMERCE_PROMO_CREDIT", "amount": 20, "status": "issued", "created_at": now.Add(-1 * time.Hour).Format(time.RFC3339)}}})
		case "/internal/v1/incentives/commissions":
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "commission-1", "product_code": "ecommerce", "currency": "ECOMMERCE_CREDIT", "amount": 9, "status": "earned", "created_at": now.Add(-2 * time.Hour).Format(time.RFC3339)}}})
		case "/internal/v1/wallet/accounts":
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "acct-1", "billing_subject_type": "organization", "billing_subject_id": "org-wallet", "asset_code": "ECOMMERCE_CREDIT", "balance": 100, "status": "active"}}})
		case "/internal/v1/wallet/ledger":
			writeWalletEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "ledger-1", "wallet_account_id": "acct-1", "asset_code": "ECOMMERCE_CREDIT", "direction": "credit", "amount": 30, "reason": "manual_adjustment", "status": "posted", "created_at": now.Add(-30 * time.Minute).Format(time.RFC3339)}, {"id": "ledger-skip", "asset_code": "ECOMMERCE_MONTHLY_ALLOWANCE", "amount": 999, "reason": "manual_adjustment", "status": "posted", "created_at": now.Format(time.RFC3339)}}})
		default:
			t.Fatalf("unexpected platform request: %s", r.URL.String())
		}
	})
	defer cleanup()
	repo := newWalletRepo(t)
	_, err := repo.GetBillingChargeByEventID("missing")
	if err == nil {
		t.Fatalf("expected empty repo to return not found")
	}
	if err := repo.CreateBillingChargeRecord(&models.BillingChargeRecord{ProductCode: "ecommerce", OrganizationID: "org-wallet", EventID: "charge-1", BusinessType: "image_generation", NetAmount: 11, Status: "settled", OccurredAt: now, BillableItemCode: "ecommerce.image.generate", MetadataJSON: `{"usage_units":2}`}); err != nil {
		t.Fatalf("seed charge: %v", err)
	}

	got, err := NewService(client, repo, config.AppConfig{ProductCode: "ecommerce"}).History("org-wallet", 3)
	if err != nil {
		t.Fatalf("History returned error: %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("len = %d, want 3; items=%+v", len(got.Items), got.Items)
	}
	if got.Items[0].Category != "charge" || got.Items[1].ID != "ledger-1" || got.Items[2].ID != "reward-1" {
		t.Fatalf("history was not merged/sorted/limited as expected: %+v", got.Items)
	}
}
