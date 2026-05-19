package repository

import (
	"ecommerce-service/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VisualWorkflowSessionFilter struct {
	ProductID string
	SKUCode   string
	Status    string
	Limit     int
	Offset    int
}

type VisualWorkflowRepository struct{ db *gorm.DB }

func NewVisualWorkflowRepository(db *gorm.DB) *VisualWorkflowRepository {
	return &VisualWorkflowRepository{db: db}
}

func (r *VisualWorkflowRepository) WithTransaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r *VisualWorkflowRepository) CreateSession(item *models.EcommerceVisualWorkflowSession) error {
	return r.db.Create(item).Error
}

func (r *VisualWorkflowRepository) GetSession(orgID, sessionID string) (*models.EcommerceVisualWorkflowSession, error) {
	var item models.EcommerceVisualWorkflowSession
	if err := r.db.Where("organization_id = ? AND id = ?", orgID, sessionID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) FindSessionByID(sessionID string) (*models.EcommerceVisualWorkflowSession, error) {
	var item models.EcommerceVisualWorkflowSession
	if err := r.db.Where("id = ?", sessionID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) FindSessionByIdempotencyKey(orgID, key string) (*models.EcommerceVisualWorkflowSession, error) {
	var item models.EcommerceVisualWorkflowSession
	if err := r.db.Where("organization_id = ? AND idempotency_key = ?", orgID, key).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) ListSessions(orgID string, filter VisualWorkflowSessionFilter) ([]models.EcommerceVisualWorkflowSession, error) {
	var items []models.EcommerceVisualWorkflowSession
	query := r.db.Where("organization_id = ?", orgID)
	if filter.ProductID != "" {
		query = query.Where("product_id = ?", filter.ProductID)
	}
	if filter.SKUCode != "" {
		query = query.Where("sku_code = ?", filter.SKUCode)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if err := query.Order("updated_at DESC").Limit(filter.Limit).Offset(filter.Offset).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *VisualWorkflowRepository) SaveSession(item *models.EcommerceVisualWorkflowSession) error {
	return r.db.Save(item).Error
}

func (r *VisualWorkflowRepository) FindSessionByGenerationVersionID(versionID string) (*models.EcommerceVisualWorkflowSession, error) {
	var item models.EcommerceVisualWorkflowSession
	if err := r.db.Where("generation_versions_json LIKE ?", "%"+versionID+"%").Order("updated_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) FindSessionByMetadataLike(needle string) (*models.EcommerceVisualWorkflowSession, error) {
	var item models.EcommerceVisualWorkflowSession
	if err := r.db.Where("metadata LIKE ?", "%"+needle+"%").Order("updated_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) CreateSourceReference(item *models.EcommerceVisualSourceReference) error {
	return r.db.Create(item).Error
}

func (r *VisualWorkflowRepository) ListSourceReferences(orgID, sessionID string) ([]models.EcommerceVisualSourceReference, error) {
	var items []models.EcommerceVisualSourceReference
	if err := r.db.Where("organization_id = ? AND session_id = ? AND status <> ?", orgID, sessionID, models.VisualSourceStatusArchived).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *VisualWorkflowRepository) LatestSourceReference(orgID, sessionID string) (*models.EcommerceVisualSourceReference, error) {
	var item models.EcommerceVisualSourceReference
	if err := r.db.Where("organization_id = ? AND session_id = ? AND status <> ?", orgID, sessionID, models.VisualSourceStatusArchived).Order("created_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) GetSourceReference(orgID, sessionID, sourceReferenceID string) (*models.EcommerceVisualSourceReference, error) {
	var item models.EcommerceVisualSourceReference
	if err := r.db.Where("organization_id = ? AND session_id = ? AND id = ?", orgID, sessionID, sourceReferenceID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) SaveSourceReference(item *models.EcommerceVisualSourceReference) error {
	return r.db.Save(item).Error
}

func (r *VisualWorkflowRepository) CreateDeconstructionJob(item *models.EcommerceVisualDeconstructionJob) error {
	result := r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "organization_id"}, {Name: "session_id"}, {Name: "idempotency_key"}}, DoNothing: true}).Create(item)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 && item.IdempotencyKey != "" {
		existing, err := r.FindDeconstructionJobByIdempotencyKey(item.OrganizationID, item.SessionID, item.IdempotencyKey)
		if err != nil {
			return err
		}
		*item = *existing
	}
	return nil
}

func (r *VisualWorkflowRepository) FindDeconstructionJobByIdempotencyKey(orgID, sessionID, key string) (*models.EcommerceVisualDeconstructionJob, error) {
	var item models.EcommerceVisualDeconstructionJob
	if err := r.db.Where("organization_id = ? AND session_id = ? AND idempotency_key = ?", orgID, sessionID, key).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) GetDeconstructionJob(orgID, sessionID, jobID string) (*models.EcommerceVisualDeconstructionJob, error) {
	var item models.EcommerceVisualDeconstructionJob
	if err := r.db.Where("organization_id = ? AND session_id = ? AND id = ?", orgID, sessionID, jobID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) FindDeconstructionJobByID(jobID string) (*models.EcommerceVisualDeconstructionJob, error) {
	var item models.EcommerceVisualDeconstructionJob
	if err := r.db.Where("id = ?", jobID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) LatestDeconstructionJob(orgID, sessionID string) (*models.EcommerceVisualDeconstructionJob, error) {
	var item models.EcommerceVisualDeconstructionJob
	if err := r.db.Where("organization_id = ? AND session_id = ?", orgID, sessionID).Order("created_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) SaveDeconstructionJob(item *models.EcommerceVisualDeconstructionJob) error {
	return r.db.Save(item).Error
}

func (r *VisualWorkflowRepository) ReplaceDeconstructionElements(orgID, sessionID, jobID string, elements []models.EcommerceVisualDeconstructionElement) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("organization_id = ? AND session_id = ? AND job_id = ?", orgID, sessionID, jobID).Delete(&models.EcommerceVisualDeconstructionElement{}).Error; err != nil {
			return err
		}
		for i := range elements {
			if err := tx.Create(&elements[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *VisualWorkflowRepository) SaveDeconstructionResult(job *models.EcommerceVisualDeconstructionJob, elements []models.EcommerceVisualDeconstructionElement, sessionStatus string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("organization_id = ? AND session_id = ? AND job_id = ?", job.OrganizationID, job.SessionID, job.ID).Delete(&models.EcommerceVisualDeconstructionElement{}).Error; err != nil {
			return err
		}
		for i := range elements {
			if err := tx.Create(&elements[i]).Error; err != nil {
				return err
			}
		}
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		updates := map[string]any{"current_stage": models.VisualWorkflowStageDeconstruction}
		if sessionStatus != "" {
			updates["status"] = sessionStatus
		}
		return tx.Model(&models.EcommerceVisualWorkflowSession{}).
			Where("organization_id = ? AND id = ?", job.OrganizationID, job.SessionID).
			Updates(updates).Error
	})
}

func (r *VisualWorkflowRepository) ListDeconstructionElements(orgID, sessionID string) ([]models.EcommerceVisualDeconstructionElement, error) {
	var items []models.EcommerceVisualDeconstructionElement
	if err := r.db.Where("organization_id = ? AND session_id = ?", orgID, sessionID).Order("sort_order ASC, confidence DESC, created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *VisualWorkflowRepository) GetDeconstructionElement(orgID, sessionID, elementID string) (*models.EcommerceVisualDeconstructionElement, error) {
	var item models.EcommerceVisualDeconstructionElement
	if err := r.db.Where("organization_id = ? AND session_id = ? AND id = ?", orgID, sessionID, elementID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *VisualWorkflowRepository) UpdateDeconstructionElement(item *models.EcommerceVisualDeconstructionElement) error {
	return r.db.Save(item).Error
}
