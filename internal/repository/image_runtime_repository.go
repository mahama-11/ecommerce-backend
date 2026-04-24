package repository

import (
	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type ImageRuntimeRepository struct {
	db *gorm.DB
}

func NewImageRuntimeRepository(db *gorm.DB) *ImageRuntimeRepository {
	return &ImageRuntimeRepository{db: db}
}

func (r *ImageRuntimeRepository) CreateJob(item *models.EcommerceImageJob) error {
	return r.db.Create(item).Error
}

func (r *ImageRuntimeRepository) FindJobByID(jobID string) (*models.EcommerceImageJob, error) {
	var item models.EcommerceImageJob
	if err := r.db.Where("id = ?", jobID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ImageRuntimeRepository) SaveJob(item *models.EcommerceImageJob) error {
	return r.db.Save(item).Error
}

func (r *ImageRuntimeRepository) ListJobs(orgID, userID, sceneType string, limit int) ([]models.EcommerceImageJob, error) {
	var items []models.EcommerceImageJob
	query := r.db.Model(&models.EcommerceImageJob{}).Where("organization_id = ?", orgID)
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if sceneType != "" {
		query = query.Where("scene_type = ?", sceneType)
	}
	if limit <= 0 {
		limit = 10
	}
	if err := query.Order("updated_at DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ImageRuntimeRepository) CreateAsset(item *models.EcommerceAsset) error {
	return r.db.Create(item).Error
}

func (r *ImageRuntimeRepository) FindAssetByID(orgID, assetID string) (*models.EcommerceAsset, error) {
	var item models.EcommerceAsset
	if err := r.db.Where("id = ? AND organization_id = ?", assetID, orgID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ImageRuntimeRepository) FindAssetByIDGlobal(assetID string) (*models.EcommerceAsset, error) {
	var item models.EcommerceAsset
	if err := r.db.Where("id = ?", assetID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}
