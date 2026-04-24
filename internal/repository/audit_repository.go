package repository

import (
	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type AuditRepository struct {
	db *gorm.DB
}

func NewAuditRepository(db *gorm.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) Create(item *models.AuditLog) error {
	return r.db.Create(item).Error
}

func (r *AuditRepository) List(actorOrgID, actorUserID, targetType, status string, limit, offset int) ([]models.AuditLog, int64, error) {
	var (
		items []models.AuditLog
		total int64
	)
	q := r.db.Model(&models.AuditLog{}).Where("actor_org_id = ?", actorOrgID)
	if actorUserID != "" {
		q = q.Where("actor_user_id = ?", actorUserID)
	}
	if targetType != "" {
		q = q.Where("target_type = ?", targetType)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	if err := q.Order("created_at desc").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
