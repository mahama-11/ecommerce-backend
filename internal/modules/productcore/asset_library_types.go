package productcore

import "time"

type AssetLibraryFilterInput struct {
	SKUCode    string `form:"sku_code"`
	ProductID  string `form:"product_id"`
	SourceType string `form:"source_type"`
	AssetRole  string `form:"asset_role"`
	Role       string `form:"role"`
	Visibility string `form:"visibility"`
	Status     string `form:"status"`
	Tag        string `form:"tag"`
	Query      string `form:"q"`
	Limit      int    `form:"limit"`
	Offset     int    `form:"offset"`
}

type AssetLibraryListResponse struct {
	Items  []AssetLibraryItem `json:"items"`
	Total  int64              `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

type AssetLibraryItem struct {
	ProductID    string                 `json:"product_id"`
	SKUCode      string                 `json:"sku_code"`
	RelationID   string                 `json:"relation_id"`
	ProductTitle string                 `json:"product_title,omitempty"`
	Asset        *AssetLibraryAsset     `json:"asset"`
	Lineage      AssetLibraryLineage    `json:"lineage"`
	Governance   AssetLibraryGovernance `json:"governance"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type AssetLibraryAsset struct {
	ID           string    `json:"id"`
	AssetType    string    `json:"asset_type"`
	SourceType   string    `json:"source_type"`
	MimeType     string    `json:"mime_type,omitempty"`
	Width        int       `json:"width,omitempty"`
	Height       int       `json:"height,omitempty"`
	FileName     string    `json:"file_name,omitempty"`
	Metadata     string    `json:"metadata,omitempty"`
	ContentURL   string    `json:"content_url,omitempty"`
	PreviewURL   string    `json:"preview_url,omitempty"`
	ReferenceURI string    `json:"reference_uri,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AssetLibraryLineage struct {
	PromptID          string `json:"prompt_id,omitempty"`
	JobID             string `json:"job_id,omitempty"`
	GenerationTaskID  string `json:"generation_task_id,omitempty"`
	RuntimeJobID      string `json:"runtime_job_id,omitempty"`
	ProviderJobID     string `json:"provider_job_id,omitempty"`
	TemplateID        string `json:"template_id,omitempty"`
	TemplateVersionID string `json:"template_version_id,omitempty"`
	PromptContentHash string `json:"prompt_content_hash,omitempty"`
}

type AssetLibraryGovernance struct {
	AssetRole    string   `json:"asset_role"`
	RelationType string   `json:"relation_type"`
	IsPrimary    bool     `json:"is_primary"`
	SortOrder    int      `json:"sort_order"`
	Visibility   string   `json:"visibility"`
	Status       string   `json:"status"`
	Tags         []string `json:"tags,omitempty"`
	PlatformCode string   `json:"platform_code,omitempty"`
	SiteCode     string   `json:"site_code,omitempty"`
	LocaleCode   string   `json:"locale_code,omitempty"`
}

type UpdateAssetGovernanceInput struct {
	AssetRole  *string  `json:"asset_role,omitempty"`
	Role       *string  `json:"role,omitempty"`
	IsPrimary  *bool    `json:"is_primary,omitempty"`
	SortOrder  *int     `json:"sort_order,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Visibility *string  `json:"visibility,omitempty"`
	Status     *string  `json:"status,omitempty"`
}

type BatchAssetGovernanceInput struct {
	RelationIDs []string                   `json:"relation_ids" binding:"required"`
	Patch       UpdateAssetGovernanceInput `json:"patch" binding:"required"`
}

type BatchAssetGovernanceResponse struct {
	Items   []AssetLibraryItem               `json:"items"`
	Results []BatchAssetGovernanceResultItem `json:"results"`
	Total   int                              `json:"total"`
	Success int                              `json:"success"`
	Failed  int                              `json:"failed"`
}

type BatchAssetGovernanceResultItem struct {
	RelationID string `json:"relation_id"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

type AssetLibraryLineageResponse struct {
	AssetID    string              `json:"asset_id"`
	RelationID string              `json:"relation_id,omitempty"`
	ProductID  string              `json:"product_id,omitempty"`
	SKUCode    string              `json:"sku_code,omitempty"`
	Lineage    AssetLibraryLineage `json:"lineage"`
}

type AssetLibraryStatsResponse struct {
	Groups []AssetLibraryStatsGroup `json:"groups"`
}

type AssetLibraryStatsGroup struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}
