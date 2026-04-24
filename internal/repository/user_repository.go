package repository

import (
	"errors"
	"fmt"
	"time"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type UserRepository struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) *UserRepository { return &UserRepository{db: db} }

func (r *UserRepository) GetPreference(userID, orgID string) (*models.UserPreference, error) {
	var item models.UserPreference
	if err := r.db.Where("user_id = ? AND organization_id = ?", userID, orgID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *UserRepository) UpsertPreference(userID, orgID, language string) (*models.UserPreference, error) {
	item, err := r.GetPreference(userID, orgID)
	if err == nil {
		item.LanguagePreference = language
		item.UpdatedAt = time.Now()
		if saveErr := r.db.Save(item).Error; saveErr != nil {
			return nil, saveErr
		}
		return item, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := time.Now()
	item = &models.UserPreference{ID: buildID("pref"), UserID: userID, OrganizationID: orgID, LanguagePreference: language, CreatedAt: now, UpdatedAt: now}
	if createErr := r.db.Create(item).Error; createErr != nil {
		return nil, createErr
	}
	return item, nil
}

func (r *UserRepository) CreateActivity(item *models.Activity) error {
	if item.ID == "" {
		item.ID = buildID("act")
	}
	return r.db.Create(item).Error
}

func buildID(prefix string) string { return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()) }
