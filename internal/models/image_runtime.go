package models

import "time"

type EcommerceImageJob struct {
	ID                    string     `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID        string     `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID                string     `gorm:"type:varchar(64);index" json:"user_id"`
	SceneType             string     `gorm:"type:varchar(64);index;not null" json:"scene_type"`
	InputMode             string     `gorm:"type:varchar(32);index;not null" json:"input_mode"`
	SourceAssetID         string     `gorm:"type:varchar(64);index" json:"source_asset_id"`
	RuntimeJobID          string     `gorm:"type:varchar(64);index" json:"runtime_job_id"`
	Status                string     `gorm:"type:varchar(16);index;not null" json:"status"`
	Stage                 string     `gorm:"type:varchar(64);index" json:"stage"`
	StageMessage          string     `gorm:"type:text" json:"stage_message"`
	Progress              int        `gorm:"not null;default:0" json:"progress"`
	ProviderJobID         string     `gorm:"type:varchar(128);index" json:"provider_job_id"`
	SelectedResultAssetID string     `gorm:"type:varchar(64);index" json:"selected_result_asset_id"`
	LastErrorCode         string     `gorm:"type:varchar(64);index" json:"last_error_code"`
	LastErrorMessage      string     `gorm:"type:text" json:"last_error_message"`
	Metadata              string     `gorm:"type:text" json:"metadata"`
	CreatedAt             time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt             time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	CanceledAt            *time.Time `json:"canceled_at,omitempty"`
}

type EcommerceAsset struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID         string    `gorm:"type:varchar(64);index" json:"user_id"`
	AssetType      string    `gorm:"type:varchar(32);index;not null" json:"asset_type"`
	SourceType     string    `gorm:"type:varchar(32);index;not null" json:"source_type"`
	StorageKey     string    `gorm:"type:varchar(255);index" json:"storage_key"`
	MimeType       string    `gorm:"type:varchar(128)" json:"mime_type"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	FileName       string    `gorm:"type:varchar(255)" json:"file_name"`
	Metadata       string    `gorm:"type:text" json:"metadata"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
