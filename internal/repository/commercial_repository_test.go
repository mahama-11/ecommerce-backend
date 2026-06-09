package repository

import (
	"path/filepath"
	"testing"
	"time"

	"ecommerce-service/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newCommercialRepositoryTestDB(t *testing.T) (*CommercialRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "commercial-repository.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.CommercialOrder{}, &models.CommercialPayment{}, &models.CommercialFulfillment{}, &models.BillingChargeRecord{}, &models.CommercialEventOutbox{}, &models.PromotionAttributionAttempt{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return NewCommercialRepository(db), db
}

func TestCommercialRepositoryScopesOrdersAndBillingByOrganization(t *testing.T) {
	repo, _ := newCommercialRepositoryTestDB(t)
	for _, orgID := range []string{"org-a", "org-b"} {
		if err := repo.CreateOrder(&models.CommercialOrder{UserID: "user-1", OrganizationID: orgID, ProductCode: "ecommerce", SKUCode: "sku", PackageCode: "pkg", PackageType: "one_time", Currency: "CNY", Quantity: 1, UnitAmount: 10, TotalAmount: 10, Status: "pending_payment", PaymentStatus: "pending", FulfillmentStatus: "pending"}); err != nil {
			t.Fatalf("create order for %s: %v", orgID, err)
		}
		if err := repo.CreateBillingChargeRecord(&models.BillingChargeRecord{ProductCode: "ecommerce", OrganizationID: orgID, EventID: "event-" + orgID, BusinessType: "runtime", NetAmount: 10, Status: "settled", OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatalf("create charge for %s: %v", orgID, err)
		}
	}
	orgAOrders, err := repo.ListOrders("org-a", 10, 0)
	if err != nil || len(orgAOrders) != 1 || orgAOrders[0].OrganizationID != "org-a" {
		t.Fatalf("orders not org-scoped: items=%+v err=%v", orgAOrders, err)
	}
	if _, err := repo.FindOrderByID("org-b", orgAOrders[0].ID); err == nil {
		t.Fatalf("FindOrderByID leaked org-a order to org-b scope")
	}
	orgACharges, err := repo.ListBillingChargeRecords("org-a", 10, 0)
	if err != nil || len(orgACharges) != 1 || orgACharges[0].OrganizationID != "org-a" {
		t.Fatalf("billing charges not org-scoped: items=%+v err=%v", orgACharges, err)
	}
}

func TestCommercialRepositoryEventIDUniquenessSupportsBillingIdempotency(t *testing.T) {
	repo, _ := newCommercialRepositoryTestDB(t)
	charge := &models.BillingChargeRecord{ProductCode: "ecommerce", OrganizationID: "org-a", EventID: "event-idempotent", BusinessType: "runtime", NetAmount: 11, Status: "settled", OccurredAt: time.Now().UTC()}
	if err := repo.CreateBillingChargeRecord(charge); err != nil {
		t.Fatalf("create charge: %v", err)
	}
	if err := repo.CreateBillingChargeRecord(&models.BillingChargeRecord{ProductCode: "ecommerce", OrganizationID: "org-a", EventID: "event-idempotent", BusinessType: "runtime", NetAmount: 99, Status: "settled", OccurredAt: time.Now().UTC()}); err == nil {
		t.Fatalf("expected unique event_id violation for duplicate billing event")
	}
	found, err := repo.GetBillingChargeByEventID("event-idempotent")
	if err != nil || found.ID != charge.ID || found.NetAmount != 11 {
		t.Fatalf("idempotency lookup returned wrong charge: got=%+v err=%v", found, err)
	}
}
