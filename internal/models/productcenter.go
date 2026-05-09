package models

import (
	"time"

	"github.com/lib/pq"
)

// ProductStatus 枚举
const (
	ProductStatusDraft        = "draft"
	ProductStatusAssetsReady  = "assets_ready"
	ProductStatusListingReady = "listing_ready"
	ProductStatusExportReady  = "export_ready"
	ProductStatusPublished    = "published"
	ProductStatusArchived     = "archived"
)

// AssetStatus 枚举
const (
	AssetStatusMissing = "missing"
	AssetStatusPartial = "partial"
	AssetStatusReady   = "ready"
)

// ListingStatus 枚举
const (
	ListingStatusMissing = "missing"
	ListingStatusPartial = "partial"
	ListingStatusReady   = "ready"
)

// ExportStatus 枚举
const (
	ExportStatusPending = "pending"
	ExportStatusReady   = "ready"
	ExportStatusDone    = "done"
)

// ListingVersionStatus 枚举
const (
	ListingVersionStatusDraft   = "draft"
	ListingVersionStatusReady   = "ready"
	ListingVersionStatusAdopted = "adopted"
)

// ExportTaskStatus 枚举
const (
	ExportTaskStatusPending    = "pending"
	ExportTaskStatusGenerating = "generating"
	ExportTaskStatusSucceeded  = "succeeded"
	ExportTaskStatusFailed     = "failed"
)

// ExportPackageStatus 枚举
const (
	ExportPackageStatusSucceeded        = "succeeded"
	ExportPackageStatusPartialSucceeded = "partial_succeeded"
	ExportPackageStatusFailed           = "failed"
)

// AssetRelationOwnerType 枚举
const (
	AssetRelationOwnerTypeProduct    = "product"
	AssetRelationOwnerTypeListing    = "listing"
	AssetRelationOwnerTypeExportTask = "export_task"
	AssetRelationOwnerTypeJob        = "job"
)

// AssetRelationType 枚举
const (
	AssetRelationTypeSource      = "source"
	AssetRelationTypeResult      = "result"
	AssetRelationTypePrimary     = "primary"
	AssetRelationTypePackageItem = "package_item"
)

// AssetRole 枚举
const (
	AssetRoleHero          = "hero"
	AssetRoleModelShot     = "model_shot"
	AssetRoleSceneShot     = "scene_shot"
	AssetRoleDetailShot    = "detail_shot"
	AssetRoleListingAttach = "listing_attachment"
)

// EcomProductSKU 商品 SKU 表
type EcomProductSKU struct {
	ID             string         `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID string         `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	SPUID          string         `gorm:"type:varchar(64);index" json:"spu_id,omitempty"`
	SKUCode        string         `gorm:"type:varchar(128);index;not null" json:"sku_code"`
	Title          string         `gorm:"type:varchar(256);not null" json:"title"`
	CategoryID     string         `gorm:"type:varchar(64);index" json:"category_id,omitempty"`
	BrandID        string         `gorm:"type:varchar(64);index" json:"brand_id,omitempty"`
	SpecJSON       string         `gorm:"type:text" json:"spec_json,omitempty"`
	CostJSON       string         `gorm:"type:text" json:"cost_json,omitempty"`
	CostCurrency   string         `gorm:"type:varchar(16);default:'USD'" json:"cost_currency,omitempty"`
	Tags           pq.StringArray `gorm:"type:text[]" json:"tags,omitempty"`
	Status         string         `gorm:"type:varchar(32);index;not null" json:"status"`
	AssetStatus    string         `gorm:"type:varchar(32);index;not null" json:"asset_status"`
	ListingStatus  string         `gorm:"type:varchar(32);index;not null" json:"listing_status"`
	ExportStatus   string         `gorm:"type:varchar(32);index;not null" json:"export_status"`
	CreatedBy      string         `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	UpdatedBy      string         `gorm:"type:varchar(64)" json:"updated_by,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 显式指定表名
func (EcomProductSKU) TableName() string {
	return "ecom_product_sku"
}

// EcomAssetRelation 资产关系表（核心设计）
type EcomAssetRelation struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	AssetID        string    `gorm:"type:varchar(64);index;not null" json:"asset_id"`
	OwnerType      string    `gorm:"type:varchar(32);index;not null" json:"owner_type"`
	OwnerID        string    `gorm:"type:varchar(64);index;not null" json:"owner_id"`
	RelationType   string    `gorm:"type:varchar(32);index;not null" json:"relation_type"`
	AssetRole      string    `gorm:"type:varchar(32);index;not null" json:"asset_role"`
	IsPrimary      bool      `gorm:"default:false;not null" json:"is_primary"`
	PlatformCode   string    `gorm:"type:varchar(32);index" json:"platform_code,omitempty"`
	SiteCode       string    `gorm:"type:varchar(32);index" json:"site_code,omitempty"`
	LocaleCode     string    `gorm:"type:varchar(16)" json:"locale_code,omitempty"`
	SortOrder      int       `gorm:"default:0;not null" json:"sort_order"`
	Visibility     string    `gorm:"type:varchar(32);index;default:'library';not null" json:"visibility"`
	Metadata       string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 显式指定表名
func (EcomAssetRelation) TableName() string {
	return "ecom_asset_relation"
}

// ProductActivityType 枚举
const (
	ProductActivityTypeAssetCreated     = "ASSET_CREATED"
	ProductActivityTypeAssetDeleted     = "ASSET_DELETED"
	ProductActivityTypeListingGenerated = "LISTING_GENERATED"
	ProductActivityTypeListingAdopted   = "LISTING_ADOPTED"
	ProductActivityTypeListingDeleted   = "LISTING_DELETED"
	ProductActivityTypeExportCreated    = "EXPORT_CREATED"
	ProductActivityTypeProfitCalculated = "PROFIT_CALCULATED"
	ProductActivityTypeStatusChanged    = "STATUS_CHANGED"
)

// EcomListingVersion Listing 版本表
type EcomListingVersion struct {
	ID             string         `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID string         `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProductID      string         `gorm:"type:varchar(64);index;not null" json:"product_id"`
	VersionNo      int            `gorm:"type:int;not null;default:1" json:"version_no"`
	VersionLabel   string         `gorm:"type:varchar(128)" json:"version_label,omitempty"`
	Status         string         `gorm:"type:varchar(32);index;not null" json:"status"`
	Title          string         `gorm:"type:varchar(512)" json:"title"`
	Description    string         `gorm:"type:text" json:"description,omitempty"`
	BulletPoints   pq.StringArray `gorm:"type:text[]" json:"bullet_points,omitempty"`
	Keywords       pq.StringArray `gorm:"type:text[]" json:"keywords,omitempty"`
	Platform       string         `gorm:"type:varchar(32);index;not null" json:"platform"`
	Site           string         `gorm:"type:varchar(32);index;not null" json:"site"`
	Locale         string         `gorm:"type:varchar(16);index;not null" json:"locale"`
	AdoptedAt      *time.Time     `json:"adopted_at,omitempty"`
	CreatedBy      string         `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 显式指定表名
func (EcomListingVersion) TableName() string {
	return "ecom_listing_version"
}

// EcomProfitSnapshot 利润快照表
type EcomProfitSnapshot struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProductID      string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	Platform       string    `gorm:"type:varchar(32);index;not null" json:"platform"`
	Site           string    `gorm:"type:varchar(32);index;not null" json:"site"`
	CostPrice      float64   `gorm:"type:decimal(12,2);not null" json:"cost_price"`
	ListingPrice   float64   `gorm:"type:decimal(12,2);not null" json:"listing_price"`
	LogisticsCost  float64   `gorm:"type:decimal(12,2);default:0;not null" json:"logistics_cost"`
	PlatformFee    float64   `gorm:"type:decimal(12,2);default:0;not null" json:"platform_fee"`
	OtherFee       float64   `gorm:"type:decimal(12,2);default:0;not null" json:"other_fee"`
	GrossProfit    float64   `gorm:"type:decimal(12,2);not null" json:"gross_profit"`
	NetProfit      float64   `gorm:"type:decimal(12,2);not null" json:"net_profit"`
	GrossMargin    float64   `gorm:"type:decimal(5,4);not null" json:"gross_margin"`
	NetMargin      float64   `gorm:"type:decimal(5,4);not null" json:"net_margin"`
	BreakevenPrice float64   `gorm:"type:decimal(12,2);not null" json:"breakeven_price"`
	CreatedBy      string    `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 显式指定表名
func (EcomProfitSnapshot) TableName() string {
	return "ecom_profit_snapshot"
}

// EcomExportTask 导出任务表
type EcomExportTask struct {
	ID                  string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID      string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProductID           string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	PackageID           string    `gorm:"type:varchar(64);index" json:"package_id,omitempty"`
	Status              string    `gorm:"type:varchar(32);index;not null" json:"status"`
	Platform            string    `gorm:"type:varchar(32);index;not null" json:"platform"`
	Site                string    `gorm:"type:varchar(32);index;not null" json:"site"`
	Locale              string    `gorm:"type:varchar(16);index;not null" json:"locale"`
	Format              string    `gorm:"type:varchar(16);not null" json:"format"`
	ListingVersionID    string    `gorm:"type:varchar(64);index" json:"listing_version_id,omitempty"`
	ListingVersionLabel string    `gorm:"type:varchar(128)" json:"listing_version_label,omitempty"`
	PrimaryAssetRole    string    `gorm:"type:varchar(32)" json:"primary_asset_role,omitempty"`
	AssetCount          int       `gorm:"default:0;not null" json:"asset_count"`
	AssetManifest       string    `gorm:"type:text" json:"asset_manifest,omitempty"` // JSON snapshot
	StorageKey          string    `gorm:"type:varchar(256)" json:"storage_key,omitempty"`
	PackageURL          string    `gorm:"type:varchar(512)" json:"package_url,omitempty"`
	FileSize            string    `gorm:"type:varchar(32)" json:"file_size,omitempty"`
	CreatedBy           string    `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	CreatedAt           time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 显式指定表名
func (EcomExportTask) TableName() string {
	return "ecom_export_task"
}

// EcomExportPackage 导出包/多 SKU 任务组表
// PackageManifest stores the public package manifest JSON; internal storage keys stay on child tasks.
type EcomExportPackage struct {
	ID              string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID  string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	Status          string    `gorm:"type:varchar(32);index;not null" json:"status"`
	Platform        string    `gorm:"type:varchar(32);index;not null" json:"platform"`
	Site            string    `gorm:"type:varchar(32);index;not null" json:"site"`
	Locale          string    `gorm:"type:varchar(16);index;not null" json:"locale"`
	Format          string    `gorm:"type:varchar(16);not null" json:"format"`
	Schema          string    `gorm:"type:varchar(64);not null" json:"schema"`
	TotalCount      int       `gorm:"default:0;not null" json:"total_count"`
	SucceededCount  int       `gorm:"default:0;not null" json:"succeeded_count"`
	FailedCount     int       `gorm:"default:0;not null" json:"failed_count"`
	PackageManifest string    `gorm:"type:text" json:"package_manifest,omitempty"`
	FileSize        string    `gorm:"type:varchar(32)" json:"file_size,omitempty"`
	CreatedBy       string    `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 显式指定表名
func (EcomExportPackage) TableName() string {
	return "ecom_export_package"
}

// EcomProductActivity 商品活动表
type EcomProductActivity struct {
	ID             string    `gorm:"type:varchar(64);primaryKey" json:"id"`
	OrganizationID string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ProductID      string    `gorm:"type:varchar(64);index;not null" json:"product_id"`
	Type           string    `gorm:"type:varchar(32);index;not null" json:"type"`
	Title          string    `gorm:"type:varchar(256);not null" json:"title"`
	Summary        string    `gorm:"type:text" json:"summary,omitempty"`
	Metadata       string    `gorm:"type:text" json:"metadata,omitempty"` // JSON
	PerformedBy    string    `gorm:"type:varchar(64)" json:"performed_by,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 显式指定表名
func (EcomProductActivity) TableName() string {
	return "ecom_product_activity"
}
