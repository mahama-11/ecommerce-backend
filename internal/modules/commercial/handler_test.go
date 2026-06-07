package commercial

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

func newCommercialRepo(t *testing.T) *repository.CommercialRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "commercial.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.CommercialOrder{}, &models.CommercialPayment{}, &models.CommercialFulfillment{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return repository.NewCommercialRepository(db)
}

func writeCommercialEnvelope(t *testing.T, w http.ResponseWriter, status, code int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "", "request_id": "req-commercial", "timestamp": time.Now().Unix(), "data": data})
}

func commercialOfferingsFixture() map[string]any {
	return map[string]any{
		"product":    map[string]any{"id": "prod-ecommerce", "code": "ecommerce", "name": "Ecommerce", "status": "active"},
		"packages":   []map[string]any{{"id": "pkg-growth", "code": "pkg-growth", "name": "Growth", "package_type": "subscription", "status": "active", "metadata": `{"sku_code":"sku-growth"}`}},
		"skus":       []map[string]any{{"id": "sku-growth", "code": "sku-growth", "name": "Growth SKU", "sku_type": "subscription", "billing_mode": "prepaid", "currency": "CNY", "list_price": 29900, "status": "active", "metadata": `{"package_code":"pkg-growth"}`}},
		"rate_cards": []map[string]any{{"id": "rate-growth", "target_type": "sku", "target_id": "sku-growth", "currency": "CNY", "price_config": `{"unit_amount":19900}`, "version": 2, "status": "active"}},
	}
}

func TestCreateOrderAndConfirmPaymentAreIdempotentAndScopedToEcommerce(t *testing.T) {
	repo := newCommercialRepo(t)
	ledgerPosts := 0
	grantPosts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/catalog/offerings":
			if got := r.URL.Query().Get("product_code"); got != "ecommerce" {
				t.Fatalf("product_code = %q", got)
			}
			writeCommercialEnvelope(t, w, http.StatusOK, 0, commercialOfferingsFixture())
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/ledger":
			ledgerPosts++
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["billing_subject_id"] != "org-commercial" || req["reference_type"] != "commercial_order" || req["asset_code"] != "ECOMMERCE_CASH" {
				t.Fatalf("unexpected wallet debit payload: %#v", req)
			}
			writeCommercialEnvelope(t, w, http.StatusOK, 0, map[string]any{"ledger": map[string]any{"id": "ledger-payment", "asset_code": "ECOMMERCE_CASH", "direction": "debit", "amount": 19900, "status": "posted", "created_at": time.Now().UTC().Format(time.RFC3339)}})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/controls/quota/policies":
			writeCommercialEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"id": "quota-policy", "product_code": "ecommerce", "package_code": "pkg-growth", "billable_item_code": "ecommerce.image.generate", "units": 100, "status": "active"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/quota/grants":
			grantPosts++
			writeCommercialEnvelope(t, w, http.StatusOK, 0, map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/summary":
			writeCommercialEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-commercial", "product_code": "ecommerce", "total_balance": 1})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commercial-test", InternalServiceSecret: "secret"})
	svc := NewService(client, repo, config.AppConfig{ProductCode: "ecommerce"})

	created, err := svc.CreateOrder("user-commercial", "org-commercial", CreateOrderInput{PackageCode: "pkg-growth", Quantity: 2, Metadata: `{"campaign":"unit"}`})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if created.Order.TotalAmount != 39800 || created.Order.ProductCode != "ecommerce" || created.Order.Status != "pending_payment" {
		t.Fatalf("unexpected created order: %+v", created.Order)
	}
	confirmed, err := svc.ConfirmOrderPayment("user-commercial", "org-commercial", created.Order.ID, ConfirmOrderPaymentInput{})
	if err != nil {
		t.Fatalf("ConfirmOrderPayment: %v", err)
	}
	if confirmed.Order.Status != "fulfilled" || confirmed.Payment == nil || confirmed.Fulfillment == nil || confirmed.Fulfillment.Amount != 100 {
		t.Fatalf("unexpected confirmed order view: %+v", confirmed)
	}
	again, err := svc.ConfirmOrderPayment("user-commercial", "org-commercial", created.Order.ID, ConfirmOrderPaymentInput{})
	if err != nil {
		t.Fatalf("ConfirmOrderPayment idempotent call: %v", err)
	}
	if again.Order.Status != "fulfilled" || ledgerPosts != 1 || grantPosts != 1 {
		t.Fatalf("duplicate confirm was not idempotent: ledgerPosts=%d grantPosts=%d view=%+v", ledgerPosts, grantPosts, again)
	}
}

func TestCreateOrderRejectsDuplicateActiveSubscription(t *testing.T) {
	repo := newCommercialRepo(t)
	now := time.Now().UTC()
	if err := repo.CreateOrder(&models.CommercialOrder{UserID: "user-1", OrganizationID: "org-1", ProductCode: "ecommerce", SKUCode: "sku-growth", PackageCode: "pkg-growth", PackageType: "subscription", Currency: "CNY", Quantity: 1, UnitAmount: 100, TotalAmount: 100, Status: "fulfilled", PaymentStatus: "succeeded", FulfillmentStatus: "succeeded", FulfilledAt: &now}); err != nil {
		t.Fatalf("seed fulfilled order: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCommercialEnvelope(t, w, http.StatusOK, 0, commercialOfferingsFixture())
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commercial-test", InternalServiceSecret: "secret"})
	_, err := NewService(client, repo, config.AppConfig{ProductCode: "ecommerce"}).CreateOrder("user-1", "org-1", CreateOrderInput{PackageCode: "pkg-growth"})
	if err == nil || !strings.Contains(err.Error(), "active subscription already exists") {
		t.Fatalf("expected duplicate active subscription error, got %v", err)
	}
}

func TestHandlerCreateOrderEnvelopeAndInvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newCommercialRepo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCommercialEnvelope(t, w, http.StatusOK, 0, commercialOfferingsFixture())
	}))
	defer server.Close()
	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, ServiceName: "commercial-handler-test", InternalServiceSecret: "secret"})
	h := NewHandler(NewService(client, repo, config.AppConfig{ProductCode: "ecommerce"}))
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.POST("/orders", func(c *gin.Context) { c.Set("userID", "user-1"); c.Set("orgID", "org-1"); h.CreateOrder(c) })

	bad := httptest.NewRecorder()
	r.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader("{")))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d body=%s", bad.Code, bad.Body.String())
	}

	goodReq := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(`{"package_code":"pkg-growth"}`))
	goodReq.Header.Set("Content-Type", "application/json")
	goodReq.Header.Set("X-Request-ID", "req-commercial-handler")
	good := httptest.NewRecorder()
	r.ServeHTTP(good, goodReq)
	if good.Code != http.StatusOK || !strings.Contains(good.Body.String(), `"request_id":"req-commercial-handler"`) || !strings.Contains(good.Body.String(), `"product_code":"ecommerce"`) {
		t.Fatalf("unexpected create order response: status=%d body=%s", good.Code, good.Body.String())
	}
}
