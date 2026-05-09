package repository

import (
	"strings"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type PromptCenterRepository struct{ db *gorm.DB }

func NewPromptCenterRepository(db *gorm.DB) *PromptCenterRepository {
	return &PromptCenterRepository{db: db}
}

func (r *PromptCenterRepository) CreatePromptRun(item *models.EcommercePromptRun) error {
	return r.db.Create(item).Error
}

func (r *PromptCenterRepository) FindPromptRunByID(orgID, promptID string) (*models.EcommercePromptRun, error) {
	var item models.EcommercePromptRun
	query := r.db.Where("id = ?", strings.TrimSpace(promptID))
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("organization_id = ?", strings.TrimSpace(orgID))
	}
	if err := query.First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PromptCenterRepository) FindPromptRunByIdempotencyKey(orgID, key string) (*models.EcommercePromptRun, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var item models.EcommercePromptRun
	if err := r.db.Where("organization_id = ? AND idempotency_key = ?", orgID, key).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PromptCenterRepository) SavePromptRun(item *models.EcommercePromptRun) error {
	return r.db.Save(item).Error
}
