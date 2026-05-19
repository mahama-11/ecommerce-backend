package models

import "time"

const (
	VisualWorkflowStageSource         = "source"
	VisualWorkflowStageDeconstruction = "deconstruction"
	VisualWorkflowStagePrompt         = "prompt"
	VisualWorkflowStageGeneration     = "generation"
	VisualWorkflowStageReview         = "review"
	VisualWorkflowStageExport         = "export"

	VisualWorkflowStatusDraft      = "draft"
	VisualWorkflowStatusReady      = "ready"
	VisualWorkflowStatusProcessing = "processing"
	VisualWorkflowStatusBlocked    = "blocked"
	VisualWorkflowStatusCompleted  = "completed"
	VisualWorkflowStatusFailed     = "failed"
	VisualWorkflowStatusCanceled   = "canceled"

	VisualReadinessReady       = "ready"
	VisualReadinessPartial     = "partial"
	VisualReadinessMissing     = "missing"
	VisualReadinessNeedsReview = "needs_review"
	VisualReadinessBlocked     = "blocked"

	VisualSourceKindPlatformSourceRef = "platform_source_ref"
	VisualSourceKindProductAsset      = "product_asset"
	VisualSourceKindURL               = "url"
	VisualSourceKindVideoFrame        = "video_frame"
	VisualSourceKindUpload            = "upload"

	VisualSourceStatusReady          = "ready"
	VisualSourceStatusPending        = "pending"
	VisualSourceStatusContractNeeded = "contract_needed"
	VisualSourceStatusArchived       = "archived"

	VisualDeconstructionStatusQueued         = "queued"
	VisualDeconstructionStatusProcessing     = "processing"
	VisualDeconstructionStatusCompleted      = "completed"
	VisualDeconstructionStatusFailed         = "failed"
	VisualDeconstructionStatusCanceled       = "canceled"
	VisualDeconstructionStatusContractNeeded = "contract_needed"
)

type EcommerceVisualWorkflowSession struct {
	ID                     string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID         string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID                 string    `gorm:"type:varchar(64);index" json:"user_id"`
	ProductID              string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	SKUCode                string    `gorm:"type:varchar(128);index;not null" json:"sku_code"`
	ToolSlug               string    `gorm:"type:varchar(64);index" json:"tool_slug,omitempty"`
	TemplateID             string    `gorm:"type:varchar(64);index" json:"template_id,omitempty"`
	TemplateVersionID      string    `gorm:"type:varchar(64);index" json:"template_version_id,omitempty"`
	CurrentStage           string    `gorm:"type:varchar(32);index;not null" json:"current_stage"`
	Status                 string    `gorm:"type:varchar(32);index;not null" json:"status"`
	ReadinessJSON          string    `gorm:"type:text;not null" json:"readiness_json"`
	IntentSpecJSON         string    `gorm:"type:text;not null" json:"intent_spec_json"`
	PromptPlanJSON         string    `gorm:"type:text;not null" json:"prompt_plan_json"`
	GenerationVersionsJSON string    `gorm:"type:text;not null" json:"generation_versions_json"`
	IdempotencyKey         string    `gorm:"type:varchar(128);index" json:"idempotency_key,omitempty"`
	Metadata               string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt              time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt              time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

type EcommerceVisualSourceReference struct {
	ID              string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID  string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID          string    `gorm:"type:varchar(64);index" json:"user_id"`
	SessionID       string    `gorm:"type:varchar(64);index;not null" json:"session_id"`
	ProductID       string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	SKUCode         string    `gorm:"type:varchar(128);index;not null" json:"sku_code"`
	SourceKind      string    `gorm:"type:varchar(32);index;not null" json:"source_kind"`
	SourceRef       string    `gorm:"type:varchar(255);index" json:"source_ref,omitempty"`
	SourceURL       string    `gorm:"type:text" json:"source_url,omitempty"`
	AssetID         string    `gorm:"type:varchar(64);index" json:"asset_id,omitempty"`
	AssetRelationID string    `gorm:"type:varchar(64);index" json:"asset_relation_id,omitempty"`
	StorageKey      string    `gorm:"type:varchar(255);index" json:"-"`
	MimeType        string    `gorm:"type:varchar(128)" json:"mime_type,omitempty"`
	Status          string    `gorm:"type:varchar(32);index;not null" json:"status"`
	ResolveStatus   string    `gorm:"type:varchar(32);index;not null" json:"resolve_status"`
	ErrorCode       string    `gorm:"type:varchar(64);index" json:"error_code,omitempty"`
	ErrorMessage    string    `gorm:"type:text" json:"error_message,omitempty"`
	Metadata        string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

type EcommerceVisualDeconstructionJob struct {
	ID                 string     `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID     string     `gorm:"type:varchar(64);uniqueIndex:idx_vdj_org_session_idempotency;index;not null" json:"organization_id"`
	UserID             string     `gorm:"type:varchar(64);index" json:"user_id"`
	SessionID          string     `gorm:"type:varchar(64);uniqueIndex:idx_vdj_org_session_idempotency;index;not null" json:"session_id"`
	ProductID          string     `gorm:"type:varchar(64);index;not null" json:"product_id"`
	SKUCode            string     `gorm:"type:varchar(128);index;not null" json:"sku_code"`
	SourceReferenceID  string     `gorm:"type:varchar(64);index" json:"source_reference_id,omitempty"`
	RuntimeJobID       string     `gorm:"type:varchar(64);index" json:"runtime_job_id,omitempty"`
	ChargeSessionID    string     `gorm:"type:varchar(64);index" json:"charge_session_id,omitempty"`
	Status             string     `gorm:"type:varchar(32);index;not null" json:"status"`
	Stage              string     `gorm:"type:varchar(64);index" json:"stage,omitempty"`
	StageMessage       string     `gorm:"type:text" json:"stage_message,omitempty"`
	Progress           int        `gorm:"not null;default:0" json:"progress"`
	CapabilityCode     string     `gorm:"type:varchar(64);index" json:"capability_code,omitempty"`
	RuntimeTaskType    string     `gorm:"type:varchar(64);index" json:"runtime_task_type,omitempty"`
	InputManifestJSON  string     `gorm:"type:text;not null" json:"input_manifest_json"`
	OutputManifestJSON string     `gorm:"type:text;not null" json:"output_manifest_json"`
	ErrorCode          string     `gorm:"type:varchar(64);index" json:"error_code,omitempty"`
	ErrorMessage       string     `gorm:"type:text" json:"error_message,omitempty"`
	IdempotencyKey     string     `gorm:"type:varchar(128);uniqueIndex:idx_vdj_org_session_idempotency" json:"idempotency_key,omitempty"`
	Metadata           string     `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type EcommerceVisualDeconstructionElement struct {
	ID              string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID  string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	SessionID       string    `gorm:"type:varchar(64);index;not null" json:"session_id"`
	JobID           string    `gorm:"type:varchar(64);index;not null" json:"job_id"`
	ProductID       string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	SKUCode         string    `gorm:"type:varchar(128);index;not null" json:"sku_code"`
	ElementType     string    `gorm:"type:varchar(64);index;not null" json:"element_type"`
	ElementKey      string    `gorm:"type:varchar(128);index" json:"element_key,omitempty"`
	Label           string    `gorm:"type:varchar(255)" json:"label,omitempty"`
	Confidence      float64   `gorm:"type:decimal(6,5);not null;default:0" json:"confidence"`
	BoundingBoxJSON string    `gorm:"type:text" json:"bounding_box_json,omitempty"`
	MaskAssetID     string    `gorm:"type:varchar(64);index" json:"mask_asset_id,omitempty"`
	SourceAssetID   string    `gorm:"type:varchar(64);index" json:"source_asset_id,omitempty"`
	ValueJSON       string    `gorm:"type:text;not null" json:"value_json"`
	Readiness       string    `gorm:"type:varchar(32);index;not null" json:"readiness"`
	Selected        bool      `gorm:"not null;default:false" json:"selected"`
	Confirmed       bool      `gorm:"not null;default:false" json:"confirmed"`
	SortOrder       int       `gorm:"not null;default:0" json:"sort_order"`
	Metadata        string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
