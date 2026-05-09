package repository

import (
	"ecommerce-service/internal/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ProductCenterRepository 商品中心仓储
type ProductCenterRepository struct{ db *gorm.DB }

func NewProductCenterRepository(db *gorm.DB) *ProductCenterRepository {
	return &ProductCenterRepository{db: db}
}

// ==================== Product SKU ====================

// ListProducts 列出商品
func (r *ProductCenterRepository) ListProducts(scope Scope) ([]models.EcomProductSKU, error) {
	var items []models.EcomProductSKU
	if err := r.db.Where("organization_id = ?", scope.OrgID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetProduct 获取单个商品
func (r *ProductCenterRepository) GetProduct(scope Scope, productID string) (*models.EcomProductSKU, error) {
	var item models.EcomProductSKU
	if err := r.db.Where("id = ? AND organization_id = ?", productID, scope.OrgID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ProductCenterRepository) GetProductBySKUCode(scope Scope, skuCode string) (*models.EcomProductSKU, error) {
	var item models.EcomProductSKU
	if err := r.db.Where("organization_id = ? AND sku_code = ?", scope.OrgID, strings.TrimSpace(skuCode)).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateProduct 创建商品
func (r *ProductCenterRepository) CreateProduct(scope Scope, item models.EcomProductSKU) (*models.EcomProductSKU, error) {
	item.OrganizationID = scope.OrgID
	item.CreatedBy = scope.UserID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateProduct 更新商品
func (r *ProductCenterRepository) UpdateProduct(scope Scope, item models.EcomProductSKU) (*models.EcomProductSKU, error) {
	item.OrganizationID = scope.OrgID
	item.UpdatedBy = scope.UserID
	if err := r.db.Save(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// DeleteProduct 删除商品（软删除，同时清理所有关联数据）
func (r *ProductCenterRepository) DeleteProduct(scope Scope, productID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 删除活动记录
		if err := tx.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Delete(&models.EcomProductActivity{}).Error; err != nil {
			return err
		}
		// 删除导出任务
		if err := tx.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Delete(&models.EcomExportTask{}).Error; err != nil {
			return err
		}
		// 删除利润快照
		if err := tx.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Delete(&models.EcomProfitSnapshot{}).Error; err != nil {
			return err
		}
		// 删除 Listing 版本
		if err := tx.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Delete(&models.EcomListingVersion{}).Error; err != nil {
			return err
		}
		// 删除资产关联
		if err := tx.Where("organization_id = ? AND owner_type = ? AND owner_id = ?", scope.OrgID, models.AssetRelationOwnerTypeProduct, productID).Delete(&models.EcomAssetRelation{}).Error; err != nil {
			return err
		}
		// 删除商品本身
		if err := tx.Where("organization_id = ? AND id = ?", scope.OrgID, productID).Delete(&models.EcomProductSKU{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// DeleteProductAsset 删除商品资产关联
func (r *ProductCenterRepository) DeleteProductAsset(scope Scope, assetRelationID string) error {
	return r.db.Where("organization_id = ? AND id = ?", scope.OrgID, assetRelationID).Delete(&models.EcomAssetRelation{}).Error
}

// ==================== Asset Relation ====================

// ListProductAssets 列出商品关联的资产
func (r *ProductCenterRepository) ListProductAssets(scope Scope, productID string) ([]models.EcomAssetRelation, error) {
	var items []models.EcomAssetRelation
	if err := r.db.Where("organization_id = ? AND owner_type = ? AND owner_id = ?", scope.OrgID, models.AssetRelationOwnerTypeProduct, productID).Order("sort_order ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// AddProductAsset 添加商品资产关联
func (r *ProductCenterRepository) AddProductAsset(scope Scope, item models.EcomAssetRelation) (*models.EcomAssetRelation, error) {
	item.OrganizationID = scope.OrgID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetProductAssetRelation 获取单个商品资产关联
func (r *ProductCenterRepository) GetProductAssetRelation(scope Scope, productID string, assetRelationID string) (*models.EcomAssetRelation, error) {
	var item models.EcomAssetRelation
	if err := r.db.Where(
		"organization_id = ? AND owner_type = ? AND owner_id = ? AND id = ?",
		scope.OrgID,
		models.AssetRelationOwnerTypeProduct,
		productID,
		assetRelationID,
	).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateProductAssetRelation 更新商品资产关联
func (r *ProductCenterRepository) UpdateProductAssetRelation(scope Scope, item models.EcomAssetRelation) (*models.EcomAssetRelation, error) {
	item.OrganizationID = scope.OrgID
	if err := r.db.Save(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ClearPrimaryProductAssets 清除商品下其他主资产标记
func (r *ProductCenterRepository) ClearPrimaryProductAssets(scope Scope, productID string, exceptRelationID string) error {
	query := r.db.Model(&models.EcomAssetRelation{}).
		Where("organization_id = ? AND owner_type = ? AND owner_id = ? AND is_primary = ?",
			scope.OrgID, models.AssetRelationOwnerTypeProduct, productID, true)
	if exceptRelationID != "" {
		query = query.Where("id <> ?", exceptRelationID)
	}
	return query.Update("is_primary", false).Error
}

// FindProductAssetRelation 查找商品与资产的既有关联，避免重复归档
func (r *ProductCenterRepository) FindProductAssetRelation(scope Scope, productID string, assetID string) (*models.EcomAssetRelation, error) {
	var item models.EcomAssetRelation
	if err := r.db.Where(
		"organization_id = ? AND owner_type = ? AND owner_id = ? AND asset_id = ?",
		scope.OrgID,
		models.AssetRelationOwnerTypeProduct,
		productID,
		assetID,
	).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ==================== Asset Library ====================

type AssetLibraryFilter struct {
	SKUCode    string
	ProductID  string
	SourceType string
	AssetRole  string
	Visibility string
	Status     string
	Tag        string
	Query      string
	Limit      int
	Offset     int
}

type AssetLibraryRecord struct {
	RelationID        string
	ProductID         string
	SKUCode           string
	ProductTitle      string
	AssetID           string
	RelationType      string
	AssetRole         string
	IsPrimary         bool
	PlatformCode      string
	SiteCode          string
	LocaleCode        string
	SortOrder         int
	Visibility        string
	RelationMetadata  string
	RelationCreatedAt time.Time
	RelationUpdatedAt time.Time
	AssetType         string
	SourceType        string
	StorageKey        string
	MimeType          string
	Width             int
	Height            int
	FileName          string
	AssetMetadata     string
	AssetCreatedAt    time.Time
	AssetUpdatedAt    time.Time
}

func (r *ProductCenterRepository) assetLibraryBaseQuery(scope Scope, filter AssetLibraryFilter) *gorm.DB {
	query := r.db.Table("ecom_asset_relation AS rel").
		Joins("JOIN ecom_product_sku AS sku ON sku.id = rel.owner_id AND sku.organization_id = rel.organization_id").
		Joins("JOIN ecommerce_assets AS asset ON asset.id = rel.asset_id AND asset.organization_id = rel.organization_id").
		Where("rel.organization_id = ? AND rel.owner_type = ?", scope.OrgID, models.AssetRelationOwnerTypeProduct)
	if filter.SKUCode = strings.TrimSpace(filter.SKUCode); filter.SKUCode != "" {
		query = query.Where("sku.sku_code = ?", filter.SKUCode)
	}
	if filter.ProductID = strings.TrimSpace(filter.ProductID); filter.ProductID != "" {
		query = query.Where("sku.id = ?", filter.ProductID)
	}
	if filter.SourceType = strings.TrimSpace(filter.SourceType); filter.SourceType != "" {
		query = query.Where("asset.source_type = ?", filter.SourceType)
	}
	if filter.AssetRole = strings.TrimSpace(filter.AssetRole); filter.AssetRole != "" {
		query = query.Where("rel.asset_role = ?", filter.AssetRole)
	}
	if filter.Visibility = strings.TrimSpace(filter.Visibility); filter.Visibility != "" {
		query = query.Where("rel.visibility = ?", filter.Visibility)
	}
	if filter.Status = strings.TrimSpace(filter.Status); filter.Status != "" {
		query = query.Where("rel.metadata LIKE ?", "%\"status\":\""+filter.Status+"\"%")
	}
	if filter.Tag = strings.TrimSpace(filter.Tag); filter.Tag != "" {
		query = query.Where("rel.metadata LIKE ?", "%\""+filter.Tag+"\"%")
	}
	if filter.Query = strings.TrimSpace(filter.Query); filter.Query != "" {
		like := "%" + filter.Query + "%"
		query = query.Where("sku.sku_code LIKE ? OR sku.title LIKE ? OR rel.asset_id LIKE ? OR rel.id LIKE ? OR asset.file_name LIKE ? OR asset.metadata LIKE ?", like, like, like, like, like, like)
	}
	return query
}

func (r *ProductCenterRepository) ListAssetLibrary(scope Scope, filter AssetLibraryFilter) ([]AssetLibraryRecord, int64, error) {
	var total int64
	if err := r.assetLibraryBaseQuery(scope, filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	var items []AssetLibraryRecord
	err := r.assetLibraryBaseQuery(scope, filter).
		Select(`rel.id AS relation_id, rel.owner_id AS product_id, sku.sku_code, sku.title AS product_title,
			rel.asset_id, rel.relation_type, rel.asset_role, rel.is_primary, rel.platform_code, rel.site_code, rel.locale_code,
			rel.sort_order, rel.visibility, rel.metadata AS relation_metadata, rel.created_at AS relation_created_at, rel.updated_at AS relation_updated_at,
			asset.asset_type, asset.source_type, asset.storage_key, asset.mime_type, asset.width, asset.height, asset.file_name,
			asset.metadata AS asset_metadata, asset.created_at AS asset_created_at, asset.updated_at AS asset_updated_at`).
		Order("rel.created_at DESC, rel.id DESC").
		Limit(filter.Limit).Offset(filter.Offset).
		Scan(&items).Error
	return items, total, err
}

func (r *ProductCenterRepository) GetAssetLibraryRelation(scope Scope, relationID string) (*models.EcomAssetRelation, error) {
	var item models.EcomAssetRelation
	if err := r.db.Where("organization_id = ? AND owner_type = ? AND id = ?", scope.OrgID, models.AssetRelationOwnerTypeProduct, relationID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ProductCenterRepository) GetAssetLibraryRecord(scope Scope, relationID string) (*AssetLibraryRecord, error) {
	var item AssetLibraryRecord
	err := r.assetLibraryBaseQuery(scope, AssetLibraryFilter{}).
		Where("rel.id = ?", strings.TrimSpace(relationID)).
		Select(`rel.id AS relation_id, rel.owner_id AS product_id, sku.sku_code, sku.title AS product_title,
			rel.asset_id, rel.relation_type, rel.asset_role, rel.is_primary, rel.platform_code, rel.site_code, rel.locale_code,
			rel.sort_order, rel.visibility, rel.metadata AS relation_metadata, rel.created_at AS relation_created_at, rel.updated_at AS relation_updated_at,
			asset.asset_type, asset.source_type, asset.storage_key, asset.mime_type, asset.width, asset.height, asset.file_name,
			asset.metadata AS asset_metadata, asset.created_at AS asset_created_at, asset.updated_at AS asset_updated_at`).
		Limit(1).
		Scan(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type AssetLibraryStatRow struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

func (r *ProductCenterRepository) AssetLibraryStats(scope Scope, filter AssetLibraryFilter, groupBy string) ([]AssetLibraryStatRow, error) {
	selectExpr := "rel.asset_role AS key"
	groupExpr := "rel.asset_role"
	switch groupBy {
	case "source_type":
		selectExpr, groupExpr = "asset.source_type AS key", "asset.source_type"
	case "sku", "sku_code":
		selectExpr, groupExpr = "sku.sku_code AS key", "sku.sku_code"
	case "asset_role", "":
	default:
		selectExpr, groupExpr = "rel.asset_role AS key", "rel.asset_role"
	}
	var rows []AssetLibraryStatRow
	return rows, r.assetLibraryBaseQuery(scope, filter).
		Select(selectExpr + ", COUNT(*) AS count").
		Group(groupExpr).
		Order("count DESC").
		Scan(&rows).Error
}

// ==================== Listing Version ====================

// ListListingVersions 列出商品的 Listing 版本
func (r *ProductCenterRepository) ListListingVersions(scope Scope, productID string) ([]models.EcomListingVersion, error) {
	var items []models.EcomListingVersion
	if err := r.db.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Order("version_no DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// CreateListingVersion 创建 Listing 版本
func (r *ProductCenterRepository) CreateListingVersion(scope Scope, item models.EcomListingVersion) (*models.EcomListingVersion, error) {
	item.OrganizationID = scope.OrgID
	item.CreatedBy = scope.UserID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateListingVersion 更新 Listing 版本
func (r *ProductCenterRepository) UpdateListingVersion(scope Scope, item models.EcomListingVersion) (*models.EcomListingVersion, error) {
	item.OrganizationID = scope.OrgID
	if err := r.db.Save(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetAdoptedListingVersion 获取已采用的 Listing 版本
func (r *ProductCenterRepository) GetAdoptedListingVersion(scope Scope, productID, platform, site, locale string) (*models.EcomListingVersion, error) {
	var item models.EcomListingVersion
	if err := r.db.Where("organization_id = ? AND product_id = ? AND platform = ? AND site = ? AND locale = ? AND status = ?",
		scope.OrgID, productID, platform, site, locale, models.ListingVersionStatusAdopted).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetNextListingVersionNo 获取下一个 Listing 版本号
func (r *ProductCenterRepository) GetNextListingVersionNo(scope Scope, productID string) (int, error) {
	var maxVersion int
	r.db.Model(&models.EcomListingVersion{}).Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Select("COALESCE(MAX(version_no), 0)").Scan(&maxVersion)
	return maxVersion + 1, nil
}

// DeleteListingVersion 删除 Listing 版本
func (r *ProductCenterRepository) DeleteListingVersion(scope Scope, productID string, versionID string) error {
	return r.db.Where("organization_id = ? AND product_id = ? AND id = ?", scope.OrgID, productID, versionID).Delete(&models.EcomListingVersion{}).Error
}

// GetProductAssetCount 获取商品资产数量
func (r *ProductCenterRepository) GetProductAssetCount(scope Scope, productID string) (int, error) {
	var count int64
	if err := r.db.Model(&models.EcomAssetRelation{}).Where("organization_id = ? AND owner_type = ? AND owner_id = ?", scope.OrgID, models.AssetRelationOwnerTypeProduct, productID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// HasAdoptedListingVersion 检查是否有采用的 Listing 版本
func (r *ProductCenterRepository) HasAdoptedListingVersion(scope Scope, productID string) (bool, error) {
	var count int64
	if err := r.db.Model(&models.EcomListingVersion{}).Where("organization_id = ? AND product_id = ? AND status = ?", scope.OrgID, productID, models.ListingVersionStatusAdopted).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasSuccessfulExportTask 检查是否有成功的导出任务
func (r *ProductCenterRepository) HasSuccessfulExportTask(scope Scope, productID string) (bool, error) {
	var count int64
	if err := r.db.Model(&models.EcomExportTask{}).Where("organization_id = ? AND product_id = ? AND status = ?", scope.OrgID, productID, models.ExportTaskStatusSucceeded).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ==================== Profit Snapshot ====================

// ListProfitSnapshots 列出商品的利润快照
func (r *ProductCenterRepository) ListProfitSnapshots(scope Scope, productID string) ([]models.EcomProfitSnapshot, error) {
	var items []models.EcomProfitSnapshot
	if err := r.db.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// CreateProfitSnapshot 创建利润快照
func (r *ProductCenterRepository) CreateProfitSnapshot(scope Scope, item models.EcomProfitSnapshot) (*models.EcomProfitSnapshot, error) {
	item.OrganizationID = scope.OrgID
	item.CreatedBy = scope.UserID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ==================== Export Task ====================

// ListExportTasks 列出商品的导出任务
func (r *ProductCenterRepository) ListExportTasks(scope Scope, productID string) ([]models.EcomExportTask, error) {
	var items []models.EcomExportTask
	if err := r.db.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListAllExportTasks 列出组织下全部导出任务
func (r *ProductCenterRepository) ListAllExportTasks(scope Scope) ([]models.EcomExportTask, error) {
	var items []models.EcomExportTask
	if err := r.db.Where("organization_id = ?", scope.OrgID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListExportTasksByPackage 列出导出包子任务
func (r *ProductCenterRepository) ListExportTasksByPackage(scope Scope, packageID string) ([]models.EcomExportTask, error) {
	var items []models.EcomExportTask
	if err := r.db.Where("organization_id = ? AND package_id = ?", scope.OrgID, packageID).Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListExportPackages 列出组织下全部导出包
func (r *ProductCenterRepository) ListExportPackages(scope Scope) ([]models.EcomExportPackage, error) {
	var items []models.EcomExportPackage
	if err := r.db.Where("organization_id = ?", scope.OrgID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetExportPackage 获取单个导出包
func (r *ProductCenterRepository) GetExportPackage(scope Scope, packageID string) (*models.EcomExportPackage, error) {
	var item models.EcomExportPackage
	if err := r.db.Where("organization_id = ? AND id = ?", scope.OrgID, packageID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateExportPackage 创建导出包
func (r *ProductCenterRepository) CreateExportPackage(scope Scope, item models.EcomExportPackage) (*models.EcomExportPackage, error) {
	item.OrganizationID = scope.OrgID
	item.CreatedBy = scope.UserID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateExportPackage 更新导出包
func (r *ProductCenterRepository) UpdateExportPackage(scope Scope, item models.EcomExportPackage) (*models.EcomExportPackage, error) {
	item.OrganizationID = scope.OrgID
	if err := r.db.Save(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetExportTask 获取单个导出任务
func (r *ProductCenterRepository) GetExportTask(scope Scope, taskID string) (*models.EcomExportTask, error) {
	var item models.EcomExportTask
	if err := r.db.Where("organization_id = ? AND id = ?", scope.OrgID, taskID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateExportTask 创建导出任务
func (r *ProductCenterRepository) CreateExportTask(scope Scope, item models.EcomExportTask) (*models.EcomExportTask, error) {
	item.OrganizationID = scope.OrgID
	item.CreatedBy = scope.UserID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateExportTask 更新导出任务
func (r *ProductCenterRepository) UpdateExportTask(scope Scope, item models.EcomExportTask) (*models.EcomExportTask, error) {
	item.OrganizationID = scope.OrgID
	if err := r.db.Save(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ==================== Product Activity ====================

// ListProductActivities 列出商品的活动记录
func (r *ProductCenterRepository) ListProductActivities(scope Scope, productID string) ([]models.EcomProductActivity, error) {
	var items []models.EcomProductActivity
	if err := r.db.Where("organization_id = ? AND product_id = ?", scope.OrgID, productID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// CreateProductActivity 创建商品活动记录
func (r *ProductCenterRepository) CreateProductActivity(scope Scope, item models.EcomProductActivity) (*models.EcomProductActivity, error) {
	item.OrganizationID = scope.OrgID
	item.PerformedBy = scope.UserID
	if err := r.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}
