package repository

import (
	"fmt"
	"time"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type CommercialRepository struct {
	db *gorm.DB
}

func NewCommercialRepository(db *gorm.DB) *CommercialRepository {
	return &CommercialRepository{db: db}
}

func (r *CommercialRepository) CreatePromotionAttempt(item *models.PromotionAttributionAttempt) error {
	if item.ID == "" {
		item.ID = buildCommercialID("promo")
	}
	return r.db.Create(item).Error
}

func (r *CommercialRepository) UpdatePromotionAttempt(item *models.PromotionAttributionAttempt) error {
	return r.db.Save(item).Error
}

func (r *CommercialRepository) CreateBillingChargeRecord(item *models.BillingChargeRecord) error {
	if item.ID == "" {
		item.ID = buildCommercialID("bill")
	}
	return r.db.Create(item).Error
}

func (r *CommercialRepository) UpdateBillingChargeRecord(item *models.BillingChargeRecord) error {
	return r.db.Save(item).Error
}

func (r *CommercialRepository) GetBillingChargeRecord(recordID string) (*models.BillingChargeRecord, error) {
	var item models.BillingChargeRecord
	if err := r.db.Where("id = ?", recordID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CommercialRepository) GetBillingChargeByEventID(eventID string) (*models.BillingChargeRecord, error) {
	var item models.BillingChargeRecord
	if err := r.db.Where("event_id = ?", eventID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CommercialRepository) ListBillingChargeRecords(orgID string, limit, offset int) ([]models.BillingChargeRecord, error) {
	var items []models.BillingChargeRecord
	query := r.db.Where("organization_id = ?", orgID).Order("occurred_at DESC, created_at DESC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *CommercialRepository) CreateOutboxEvent(item *models.CommercialEventOutbox) error {
	if item.ID == "" {
		item.ID = buildCommercialID("outbox")
	}
	return r.db.Create(item).Error
}

func (r *CommercialRepository) UpdateOutboxEvent(item *models.CommercialEventOutbox) error {
	return r.db.Save(item).Error
}

func (r *CommercialRepository) ListReplayableOutbox(limit int, now time.Time) ([]models.CommercialEventOutbox, error) {
	if limit <= 0 {
		limit = 20
	}
	var items []models.CommercialEventOutbox
	if err := r.db.
		Where("status IN ? AND available_at <= ?", []string{"pending", "failed"}, now).
		Order("available_at ASC, created_at ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *CommercialRepository) CreateOrder(item *models.CommercialOrder) error {
	if item.ID == "" {
		item.ID = buildCommercialID("ord")
	}
	return r.db.Create(item).Error
}

func (r *CommercialRepository) SaveOrder(item *models.CommercialOrder) error {
	return r.db.Save(item).Error
}

func (r *CommercialRepository) FindOrderByID(orgID, orderID string) (*models.CommercialOrder, error) {
	var item models.CommercialOrder
	if err := r.db.Where("organization_id = ? AND id = ?", orgID, orderID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CommercialRepository) ListOrders(orgID string, limit, offset int) ([]models.CommercialOrder, error) {
	var items []models.CommercialOrder
	query := r.db.Where("organization_id = ?", orgID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *CommercialRepository) FindLatestFulfilledSubscriptionOrder(orgID, packageCode string) (*models.CommercialOrder, error) {
	var item models.CommercialOrder
	if err := r.db.
		Where("organization_id = ? AND package_code = ? AND package_type = ? AND status = ?", orgID, packageCode, "subscription", "fulfilled").
		Order("fulfilled_at DESC, created_at DESC").
		First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CommercialRepository) CreatePayment(item *models.CommercialPayment) error {
	if item.ID == "" {
		item.ID = buildCommercialID("pay")
	}
	return r.db.Create(item).Error
}

func (r *CommercialRepository) FindLatestPaymentByOrderID(orderID string) (*models.CommercialPayment, error) {
	var item models.CommercialPayment
	if err := r.db.Where("order_id = ?", orderID).Order("created_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CommercialRepository) CreateFulfillment(item *models.CommercialFulfillment) error {
	if item.ID == "" {
		item.ID = buildCommercialID("ful")
	}
	return r.db.Create(item).Error
}

func (r *CommercialRepository) FindLatestFulfillmentByOrderID(orderID string) (*models.CommercialFulfillment, error) {
	var item models.CommercialFulfillment
	if err := r.db.Where("order_id = ?", orderID).Order("created_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func buildCommercialID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
