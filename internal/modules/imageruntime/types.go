package imageruntime

import "ecommerce-service/internal/billinggate"

type UpdateJobRuntimeInput struct {
	Status        string         `json:"status"`
	Stage         string         `json:"stage"`
	StageMessage  string         `json:"stage_message"`
	Progress      *int           `json:"progress"`
	EtaSeconds    *int           `json:"eta_seconds"`
	ProviderJobID string         `json:"provider_job_id"`
	ErrorCode     string         `json:"error_code,omitempty"`
	ErrorMessage  string         `json:"error_message,omitempty"`
	Metadata      map[string]any `json:"metadata"`
}

type RecordResultAssetInput struct {
	AssetType  string `json:"asset_type"`
	SourceType string `json:"source_type"`
	FileName   string `json:"file_name,omitempty"`
	StorageKey string `json:"storage_key,omitempty"`
	SourceURL  string `json:"source_url"`
	PreviewURL string `json:"preview_url,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

type RecordResultVariantInput struct {
	Index      int                    `json:"index" binding:"required"`
	Status     string                 `json:"status" binding:"required"`
	IsSelected bool                   `json:"is_selected,omitempty"`
	Asset      RecordResultAssetInput `json:"asset"`
}

type RecordJobResultsInput struct {
	Status       string                     `json:"status" binding:"required"`
	Progress     int                        `json:"progress"`
	StageMessage string                     `json:"stage_message,omitempty"`
	ErrorCode    string                     `json:"error_code,omitempty"`
	ErrorMessage string                     `json:"error_message,omitempty"`
	Metadata     map[string]any             `json:"metadata,omitempty"`
	Variants     []RecordResultVariantInput `json:"variants"`
}

type RegisterSourceAssetInput struct {
	ProductID string         `json:"product_id" binding:"required"`
	SKUCode   string         `json:"sku_code" binding:"required"`
	FileName  string         `json:"file_name"`
	MimeType  string         `json:"mime_type" binding:"required"`
	Payload   string         `json:"payload" binding:"required"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type CreateImageJobInput struct {
	ProductID          string                     `json:"product_id" binding:"required"`
	SKUCode            string                     `json:"sku_code" binding:"required"`
	SceneType          string                     `json:"scene_type" binding:"required"`
	InputMode          string                     `json:"input_mode,omitempty"`
	SourceAssetID      string                     `json:"source_asset_id,omitempty"`
	SourceAssets       []ImageJobSourceAssetInput `json:"source_assets,omitempty"`
	PromptID           string                     `json:"prompt_id,omitempty"`
	Prompt             string                     `json:"prompt,omitempty"`
	NegativePrompt     string                     `json:"negative_prompt,omitempty"`
	Objective          string                     `json:"objective,omitempty"`
	PreferredProviders []string                   `json:"preferred_providers,omitempty"`
	RequestedVariants  int                        `json:"requested_variants,omitempty"`
	Width              int                        `json:"width,omitempty"`
	Height             int                        `json:"height,omitempty"`
	Steps              int                        `json:"steps,omitempty"`
	CFG                float64                    `json:"cfg,omitempty"`
	Denoise            float64                    `json:"denoise,omitempty"`
	TemplateCode       string                     `json:"template_code,omitempty"`
	IdempotencyKey     string                     `json:"idempotency_key,omitempty"`
}

type ImageJobSourceAssetInput struct {
	Slot        string         `json:"slot,omitempty"`
	Role        string         `json:"role,omitempty"`
	AssetID     string         `json:"asset_id"`
	Required    bool           `json:"required,omitempty"`
	Label       string         `json:"label,omitempty"`
	Constraints map[string]any `json:"constraints,omitempty"`
}

type AssetSummary struct {
	ID         string         `json:"id"`
	AssetType  string         `json:"asset_type"`
	SourceType string         `json:"source_type"`
	StorageKey string         `json:"storage_key"`
	MimeType   string         `json:"mime_type"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	FileName   string         `json:"file_name"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ImageJobSummary struct {
	JobID                 string         `json:"job_id"`
	OrganizationID        string         `json:"organization_id"`
	UserID                string         `json:"user_id"`
	SceneType             string         `json:"scene_type"`
	InputMode             string         `json:"input_mode"`
	SourceAssetID         string         `json:"source_asset_id"`
	PromptID              string         `json:"prompt_id,omitempty"`
	RuntimeJobID          string         `json:"runtime_job_id"`
	Status                string         `json:"status"`
	Stage                 string         `json:"stage"`
	StageMessage          string         `json:"stage_message"`
	Progress              int            `json:"progress"`
	ProviderJobID         string         `json:"provider_job_id,omitempty"`
	SelectedResultAssetID string         `json:"selected_result_asset_id,omitempty"`
	LastErrorCode         string         `json:"last_error_code,omitempty"`
	LastErrorMessage      string         `json:"last_error_message,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type compiledPromptPlan struct {
	ToolSlug             string
	PromptStrategy       string
	FinalPrompt          string
	FinalNegativePrompt  string
	ResolvedTemplateID   string
	ResolvedTemplateCode string
	ResolvedTemplateName string
	L1Source             string
	L2Enabled            bool
}

type chargeContext = billinggate.Context
