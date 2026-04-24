package models

import "time"

type TemplateCatalog struct {
	ID                 string    `gorm:"type:varchar(64);primaryKey"`
	Slug               string    `gorm:"type:varchar(128);uniqueIndex;not null"`
	ExternalCode       string    `gorm:"type:varchar(64);index"`
	Scope              string    `gorm:"type:varchar(16);not null;default:official"`
	ManagedSource      string    `gorm:"type:varchar(32);index;not null;default:ops_manual"`
	Modality           string    `gorm:"type:varchar(16);not null"`
	ExecutorType       string    `gorm:"type:varchar(32);not null"`
	Series             string    `gorm:"type:varchar(64);index;not null"`
	CapabilityType     string    `gorm:"type:varchar(64);index;not null"`
	InteractionMode    string    `gorm:"type:varchar(32);not null"`
	Status             string    `gorm:"type:varchar(16);index;not null"`
	CurrentVersionID   string    `gorm:"type:varchar(64);index"`
	DefaultLocale      string    `gorm:"type:varchar(16);not null;default:zh"`
	CoverAssetURL      string    `gorm:"type:text"`
	IconAssetURL       string    `gorm:"type:text"`
	PlatformTagsJSON   string    `gorm:"type:text;not null"`
	IndustryTagsJSON   string    `gorm:"type:text;not null"`
	ScenarioTagsJSON   string    `gorm:"type:text;not null"`
	ComplianceTagsJSON string    `gorm:"type:text;not null"`
	IsFeatured         bool      `gorm:"not null;default:false"`
	RecommendScore     int       `gorm:"not null;default:0"`
	SortOrder          int       `gorm:"not null;default:0"`
	CostEstimateMin    int64     `gorm:"not null;default:0"`
	CostEstimateMax    int64     `gorm:"not null;default:0"`
	SuccessRateHint    float64   `gorm:"not null;default:0"`
	OwnerTeam          string    `gorm:"type:varchar(64)"`
	CreatedBy          string    `gorm:"type:varchar(64)"`
	UpdatedBy          string    `gorm:"type:varchar(64)"`
	CreatedAt          time.Time `gorm:"autoCreateTime"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime"`
	PublishedAt        *time.Time
	ArchivedAt         *time.Time
}

type TemplateCatalogLocale struct {
	ID                  string    `gorm:"type:varchar(64);primaryKey"`
	TemplateCatalogID   string    `gorm:"type:varchar(64);index:idx_template_catalog_locale,priority:1;not null"`
	Locale              string    `gorm:"type:varchar(16);index:idx_template_catalog_locale,priority:2;not null"`
	Name                string    `gorm:"type:varchar(255);not null"`
	ShortName           string    `gorm:"type:varchar(128)"`
	Summary             string    `gorm:"type:text;not null"`
	Description         string    `gorm:"type:text;not null"`
	ScenarioDescription string    `gorm:"type:text"`
	InputDescription    string    `gorm:"type:text"`
	OutputDescription   string    `gorm:"type:text"`
	SEOCode             string    `gorm:"type:varchar(255)"`
	SEODescription      string    `gorm:"type:text"`
	CreatedAt           time.Time `gorm:"autoCreateTime"`
	UpdatedAt           time.Time `gorm:"autoUpdateTime"`
}

type TemplateCatalogVersion struct {
	ID                string     `gorm:"type:varchar(64);primaryKey"`
	TemplateCatalogID string     `gorm:"type:varchar(64);index:idx_template_catalog_version,priority:1;not null"`
	VersionNo         int        `gorm:"index:idx_template_catalog_version,priority:2;not null"`
	VersionLabel      string     `gorm:"type:varchar(32);not null"`
	Status            string     `gorm:"type:varchar(16);index;not null"`
	ChangeNote        string     `gorm:"type:text"`
	IsPublishable     bool       `gorm:"not null;default:true"`
	IsDefault         bool       `gorm:"not null;default:false"`
	SourceAssetRef    string     `gorm:"type:varchar(255)"`
	CreatedBy         string     `gorm:"type:varchar(64)"`
	PublishedBy       string     `gorm:"type:varchar(64)"`
	CreatedAt         time.Time  `gorm:"autoCreateTime"`
	PublishedAt       *time.Time
	ArchivedAt        *time.Time
}

type TemplateCatalogSchema struct {
	ID                string    `gorm:"type:varchar(64);primaryKey"`
	TemplateVersionID string    `gorm:"type:varchar(64);uniqueIndex;not null"`
	InputSchemaJSON   string    `gorm:"type:text;not null"`
	OutputSchemaJSON  string    `gorm:"type:text;not null"`
	ExecutionSchemaJSON string  `gorm:"type:text;not null"`
	PromptLayersJSON  string    `gorm:"type:text;not null"`
	PolicySchemaJSON  string    `gorm:"type:text"`
	DefaultVariablesJSON string `gorm:"type:text;not null"`
	ToolBindingJSON   string    `gorm:"type:text;not null"`
	CreatedAt         time.Time `gorm:"autoCreateTime"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime"`
}

type TemplateCatalogExample struct {
	ID                string    `gorm:"type:varchar(64);primaryKey"`
	TemplateVersionID string    `gorm:"type:varchar(64);index;not null"`
	ExampleType       string    `gorm:"type:varchar(32);not null"`
	Title             string    `gorm:"type:varchar(255)"`
	Description       string    `gorm:"type:text"`
	InputAssetURL     string    `gorm:"type:text"`
	OutputAssetURL    string    `gorm:"type:text"`
	PreviewAssetURL   string    `gorm:"type:text"`
	VideoPosterURL    string    `gorm:"type:text"`
	SortOrder         int       `gorm:"not null;default:0"`
	CreatedAt         time.Time `gorm:"autoCreateTime"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime"`
}

type TemplateFavorite struct {
	ID                string    `gorm:"type:varchar(64);primaryKey"`
	TemplateCatalogID string    `gorm:"type:varchar(64);index:idx_template_favorite_unique,priority:1;not null"`
	UserID            string    `gorm:"type:varchar(64);index:idx_template_favorite_unique,priority:2;not null"`
	OrganizationID    string    `gorm:"type:varchar(64);index:idx_template_favorite_unique,priority:3;not null"`
	CreatedAt         time.Time `gorm:"autoCreateTime"`
}

type TemplateInstance struct {
	ID                string    `gorm:"type:varchar(64);primaryKey"`
	UserID            string    `gorm:"type:varchar(64);index:idx_template_instance_scope,priority:1;not null"`
	OrganizationID    string    `gorm:"type:varchar(64);index:idx_template_instance_scope,priority:2;not null"`
	PresetTemplateID  string    `gorm:"type:varchar(64);index"`
	PresetVersionID   string    `gorm:"type:varchar(64);index"`
	SourceType        string    `gorm:"type:varchar(32);not null"`
	SourceLabel       string    `gorm:"type:varchar(255)"`
	Modality          string    `gorm:"type:varchar(16);not null"`
	ExecutorType      string    `gorm:"type:varchar(32);not null"`
	Series            string    `gorm:"type:varchar(64);not null"`
	CapabilityType    string    `gorm:"type:varchar(64);not null"`
	Status            string    `gorm:"type:varchar(16);not null;default:published"`
	IsArchived        bool      `gorm:"not null;default:false"`
	IsFavorite        bool      `gorm:"not null;default:false"`
	EditableSchemaJSON string   `gorm:"type:text;not null"`
	PromptLayersJSON  string    `gorm:"type:text;not null"`
	PlatformTagsJSON  string    `gorm:"type:text;not null"`
	IndustryTagsJSON  string    `gorm:"type:text;not null"`
	SavedAt           time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime"`
	ArchivedAt        *time.Time
}

type TemplateUsageEvent struct {
	ID                string    `gorm:"type:varchar(64);primaryKey"`
	EventType         string    `gorm:"type:varchar(32);index;not null"`
	TemplateCatalogID string    `gorm:"type:varchar(64);index"`
	TemplateVersionID string    `gorm:"type:varchar(64);index"`
	TemplateInstanceID string   `gorm:"type:varchar(64);index"`
	ExecutorType      string    `gorm:"type:varchar(32)"`
	Modality          string    `gorm:"type:varchar(16)"`
	UserID            string    `gorm:"type:varchar(64);index"`
	OrganizationID    string    `gorm:"type:varchar(64);index"`
	RequestID         string    `gorm:"type:varchar(64)"`
	TraceID           string    `gorm:"type:varchar(64)"`
	RoutePath         string    `gorm:"type:varchar(255)"`
	Status            string    `gorm:"type:varchar(16)"`
	CostEstimate      int64     `gorm:"not null;default:0"`
	LatencyMS         int       `gorm:"not null;default:0"`
	PayloadJSON       string    `gorm:"type:text"`
	CreatedAt         time.Time `gorm:"autoCreateTime"`
}
