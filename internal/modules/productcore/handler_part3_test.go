package productcore

import (
	"testing"

	"ecommerce-service/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func seedBatchListingTestData(t *testing.T, db *gorm.DB) {
	t.Helper()

	records := []any{
		&models.EcomProductSKU{
			ID:             "product-batch-1",
			OrganizationID: "org-1",
			SKUCode:        "SKU-BATCH-1",
			Title:          "Batch Product One",
			Status:         models.ProductStatusDraft,
			AssetStatus:    models.AssetStatusReady,
			ListingStatus:  models.ListingStatusMissing,
			ExportStatus:   models.ExportStatusPending,
			CreatedBy:      "user-1",
			UpdatedBy:      "user-1",
		},
		&models.EcomProductSKU{
			ID:             "product-batch-2",
			OrganizationID: "org-1",
			SKUCode:        "SKU-BATCH-2",
			Title:          "Batch Product Two",
			Status:         models.ProductStatusDraft,
			AssetStatus:    models.AssetStatusReady,
			ListingStatus:  models.ListingStatusMissing,
			ExportStatus:   models.ExportStatusPending,
			CreatedBy:      "user-1",
			UpdatedBy:      "user-1",
		},
	}

	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed batch listing record: %v", err)
		}
	}
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
