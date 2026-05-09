package models

import "time"

type EcommercePromptRun struct {
	ID                      string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID          string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	UserID                  string    `gorm:"type:varchar(64);index" json:"user_id"`
	ProductID               string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	SKUCode                 string    `gorm:"type:varchar(64);index;not null" json:"sku_code"`
	TemplateID              string    `gorm:"type:varchar(64);index;not null" json:"template_id"`
	TemplateVersionID       string    `gorm:"type:varchar(64);index;not null" json:"template_version_id"`
	TemplateVersionNo       int       `gorm:"not null;default:0" json:"template_version_no"`
	TemplateCode            string    `gorm:"type:varchar(128);index" json:"template_code"`
	ToolSlug                string    `gorm:"type:varchar(64);index" json:"tool_slug"`
	SceneType               string    `gorm:"type:varchar(64);index;not null" json:"scene_type"`
	Status                  string    `gorm:"type:varchar(16);index;not null" json:"status"`
	SchemaVersion           string    `gorm:"type:varchar(32);index;not null" json:"schema_version"`
	ContentHash             string    `gorm:"type:varchar(128);index;not null" json:"content_hash"`
	SourceMapHash           string    `gorm:"type:varchar(128);index" json:"source_map_hash"`
	InputPayloadJSON        string    `gorm:"type:text;not null" json:"input_payload_json"`
	SourceAssetBindingsJSON string    `gorm:"type:text;not null" json:"source_asset_bindings_json"`
	VariablesJSON           string    `gorm:"type:text;not null" json:"variables_json"`
	CompiledPromptJSON      string    `gorm:"type:text;not null" json:"compiled_prompt_json"`
	SourceMapJSON           string    `gorm:"type:text;not null" json:"source_map_json"`
	ValidationResultJSON    string    `gorm:"type:text;not null" json:"validation_result_json"`
	IdempotencyKey          string    `gorm:"type:varchar(128);index" json:"idempotency_key"`
	CreatedAt               time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
