package models

import "time"

type SavedTemplate struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	UserID         string    `gorm:"type:varchar(64);index:idx_saved_template_scope,priority:1;not null" json:"-"`
	OrganizationID string    `gorm:"type:varchar(64);index:idx_saved_template_scope,priority:2;not null" json:"-"`
	Platform       string    `gorm:"type:varchar(64);not null" json:"platform"`
	TagsJSON       string    `gorm:"type:text;not null" json:"-"`
	UsageCount     string    `gorm:"type:varchar(32);not null" json:"usageCount"`
	Favorite       float64   `gorm:"not null" json:"favorite"`
	SavedAt        string    `gorm:"type:varchar(64);not null" json:"savedAt"`
	SourceType     string    `gorm:"type:varchar(32)" json:"sourceType,omitempty"`
	SourceLabel    string    `gorm:"type:text" json:"sourceLabel,omitempty"`
	ZHTitle        string    `gorm:"type:text;not null" json:"-"`
	ZHSummary      string    `gorm:"type:text;not null" json:"-"`
	ZHScenario     string    `gorm:"type:text;not null" json:"-"`
	ENTitle        string    `gorm:"type:text;not null" json:"-"`
	ENSummary      string    `gorm:"type:text;not null" json:"-"`
	ENScenario     string    `gorm:"type:text;not null" json:"-"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"-"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"-"`
}

type WorkflowEvent struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	UserID         string    `gorm:"type:varchar(64);index:idx_workflow_scope_created,priority:1;not null" json:"-"`
	OrganizationID string    `gorm:"type:varchar(64);index:idx_workflow_scope_created,priority:2;not null" json:"-"`
	Module         string    `gorm:"type:varchar(32);not null" json:"module"`
	TitleZH        string    `gorm:"type:text;not null" json:"-"`
	TitleEN        string    `gorm:"type:text;not null" json:"-"`
	DetailZH       string    `gorm:"type:text;not null" json:"-"`
	DetailEN       string    `gorm:"type:text;not null" json:"-"`
	CreatedAtISO   string    `gorm:"type:varchar(64);not null" json:"createdAt"`
	CreatedAt      time.Time `gorm:"index:idx_workflow_scope_created,priority:3,sort:desc;autoCreateTime" json:"-"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"-"`
}

type LinkedDesignAsset struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	UserID         string    `gorm:"type:varchar(64);index:idx_design_asset_scope,priority:1;not null" json:"-"`
	OrganizationID string    `gorm:"type:varchar(64);index:idx_design_asset_scope,priority:2;not null" json:"-"`
	SourcePath     string    `gorm:"type:text;not null" json:"sourcePath"`
	TitleZH        string    `gorm:"type:text;not null" json:"-"`
	TitleEN        string    `gorm:"type:text;not null" json:"-"`
	DescZH         string    `gorm:"type:text;not null" json:"-"`
	DescEN         string    `gorm:"type:text;not null" json:"-"`
	SyncedAt       string    `gorm:"type:varchar(64);not null" json:"syncedAt"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"-"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"-"`
}

type LinkedDelivery struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	UserID         string    `gorm:"type:varchar(64);index:idx_delivery_scope,priority:1;not null" json:"-"`
	OrganizationID string    `gorm:"type:varchar(64);index:idx_delivery_scope,priority:2;not null" json:"-"`
	SourcePath     string    `gorm:"type:text;not null" json:"sourcePath"`
	TitleZH        string    `gorm:"type:text;not null" json:"-"`
	TitleEN        string    `gorm:"type:text;not null" json:"-"`
	Size           string    `gorm:"type:varchar(32);not null" json:"size"`
	Status         string    `gorm:"type:varchar(32);not null" json:"status"`
	MetaZH         string    `gorm:"type:text;not null" json:"-"`
	MetaEN         string    `gorm:"type:text;not null" json:"-"`
	CreatedAtISO   string    `gorm:"type:varchar(64);not null" json:"createdAt"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"-"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"-"`
}

type TemplateBridge struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	UserID         string    `gorm:"type:varchar(64);index:idx_template_bridge_scope,priority:1;not null" json:"-"`
	OrganizationID string    `gorm:"type:varchar(64);index:idx_template_bridge_scope,priority:2;not null" json:"-"`
	DesignTitleZH  string    `gorm:"type:text;not null" json:"-"`
	DesignTitleEN  string    `gorm:"type:text;not null" json:"-"`
	AITitleZH      string    `gorm:"type:text;not null" json:"-"`
	AITitleEN      string    `gorm:"type:text;not null" json:"-"`
	ScenarioZH     string    `gorm:"type:text;not null" json:"-"`
	ScenarioEN     string    `gorm:"type:text;not null" json:"-"`
	CreatedAtISO   string    `gorm:"type:varchar(64);not null" json:"createdAt"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"-"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"-"`
}
