package repository

import (
	"ecommerce-service/internal/models"
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
