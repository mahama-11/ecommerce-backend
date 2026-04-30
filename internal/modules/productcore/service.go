package productcore

import (
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo      *repository.ProductCenterRepository
	assetRepo *repository.ImageRuntimeRepository
	platform  *platform.Client
}

func NewService(repo *repository.ProductCenterRepository, assetRepo *repository.ImageRuntimeRepository, platformClient *platform.Client) *Service {
	return &Service{repo: repo, assetRepo: assetRepo, platform: platformClient}
}

// ==================== 辅助函数和状态机 ====================

// formatPercent 格式化百分比
func formatPercent(p float64) string {
	pct := p * 100
	if pct == float64(int(pct)) {
		return fmt.Sprintf("%d%%", int(pct))
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func sanitizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func isValidAssetRelationType(value string) bool {
	switch value {
	case models.AssetRelationTypeSource, models.AssetRelationTypeResult, models.AssetRelationTypePrimary, models.AssetRelationTypePackageItem:
		return true
	default:
		return false
	}
}

func isValidAssetRole(value string) bool {
	switch value {
	case models.AssetRoleHero, models.AssetRoleModelShot, models.AssetRoleSceneShot, models.AssetRoleDetailShot, models.AssetRoleListingAttach:
		return true
	default:
		return false
	}
}

// isValidStatusTransition 验证状态转换是否合法
func isValidStatusTransition(from, to string) bool {
	validTransitions := map[string][]string{
		models.ProductStatusDraft: {
			models.ProductStatusAssetsReady,
			models.ProductStatusArchived,
		},
		models.ProductStatusAssetsReady: {
			models.ProductStatusDraft,
			models.ProductStatusListingReady,
			models.ProductStatusArchived,
		},
		models.ProductStatusListingReady: {
			models.ProductStatusDraft,
			models.ProductStatusAssetsReady,
			models.ProductStatusExportReady,
			models.ProductStatusArchived,
		},
		models.ProductStatusExportReady: {
			models.ProductStatusDraft,
			models.ProductStatusAssetsReady,
			models.ProductStatusListingReady,
			models.ProductStatusPublished,
			models.ProductStatusArchived,
		},
		models.ProductStatusPublished: {
			models.ProductStatusDraft,
			models.ProductStatusAssetsReady,
			models.ProductStatusListingReady,
			models.ProductStatusExportReady,
			models.ProductStatusArchived,
		},
		models.ProductStatusArchived: {
			models.ProductStatusDraft,
		},
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}

	return slices.Contains(allowed, to)
}

// updateProductMainStatus 根据子状态自动更新主状态
func (s *Service) updateProductMainStatus(scope repository.Scope, productID string) {
	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
		return
	}

	// 如果已经是 Published 或 Archived，不自动更新
	if product.Status == models.ProductStatusPublished || product.Status == models.ProductStatusArchived {
		return
	}

	var newStatus string

	// 根据子状态确定主状态
	if product.AssetStatus == models.AssetStatusReady &&
		product.ListingStatus == models.ListingStatusReady &&
		product.ExportStatus == models.ExportStatusReady {
		newStatus = models.ProductStatusExportReady
	} else if product.AssetStatus == models.AssetStatusReady &&
		product.ListingStatus == models.ListingStatusReady {
		newStatus = models.ProductStatusListingReady
	} else if product.AssetStatus == models.AssetStatusReady {
		newStatus = models.ProductStatusAssetsReady
	} else {
		newStatus = models.ProductStatusDraft
	}

	// 只有状态变化时才更新
	if newStatus != product.Status {
		oldStatus := product.Status
		product.Status = newStatus
		s.repo.UpdateProduct(scope, *product)

		s.logActivity(scope, productID, models.ProductActivityTypeStatusChanged,
			"Auto Status Updated", "Product status auto changed from "+oldStatus+" to "+newStatus, nil)
	}
}

// ==================== Product 聚合查询 ====================

// ProductDetail 完整的商品详情聚合响应
type ProductDetail struct {
	Product         models.EcomProductSKU        `json:"product"`
	Assets          []ProductAssetWithDetail     `json:"assets"`
	ListingVersions []models.EcomListingVersion  `json:"listing_versions"`
	ProfitSnapshots []models.EcomProfitSnapshot  `json:"profit_snapshots"`
	ExportTasks     []models.EcomExportTask      `json:"export_tasks"`
	Activities      []models.EcomProductActivity `json:"activities"`
}

// ProductAssetWithDetail 资产关联加上资产详情
type ProductAssetWithDetail struct {
	Relation models.EcomAssetRelation `json:"relation"`
	Asset    *models.EcommerceAsset   `json:"asset,omitempty"`
}

// DownloadListItem 下载中心聚合项
type DownloadListItem struct {
	ID                  string              `json:"id"`
	SourceType          string              `json:"source_type"`
	ProductID           string              `json:"product_id"`
	ProductTitle        string              `json:"product_title"`
	ProductSKU          string              `json:"product_sku"`
	ProductStatus       string              `json:"product_status"`
	ProductPath         string              `json:"product_path"`
	Platform            string              `json:"platform"`
	Site                string              `json:"site"`
	Locale              string              `json:"locale"`
	Format              string              `json:"format"`
	Status              string              `json:"status"`
	FileSize            string              `json:"file_size,omitempty"`
	PackageURL          string              `json:"package_url,omitempty"`
	ListingVersionID    string              `json:"listing_version_id,omitempty"`
	ListingVersionLabel string              `json:"listing_version_label,omitempty"`
	DownloadFileName    string              `json:"download_file_name"`
	Downloadable        bool                `json:"downloadable"`
	AssetCount          int                 `json:"asset_count"`
	PrimaryAssetRole    string              `json:"primary_asset_role,omitempty"`
	Assets              []DownloadAssetItem `json:"assets,omitempty"`
	CreatedAt           time.Time           `json:"created_at"`
}

type DownloadAssetItem struct {
	RelationID string `json:"relation_id"`
	AssetID    string `json:"asset_id"`
	AssetRole  string `json:"asset_role"`
	IsPrimary  bool   `json:"is_primary"`
	AssetType  string `json:"asset_type,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	ContentURL string `json:"content_url,omitempty"`
}

func decodeDownloadAssetManifest(raw string) ([]DownloadAssetItem, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var items []DownloadAssetItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) buildProductAssetManifest(scope repository.Scope, productID string, assetRelationIDs []string) ([]DownloadAssetItem, string, error) {
	assets, err := s.repo.ListProductAssets(scope, productID)
	if err != nil {
		return nil, "", err
	}
	if len(assetRelationIDs) > 0 {
		allowed := make(map[string]struct{}, len(assetRelationIDs))
		for _, relationID := range assetRelationIDs {
			trimmed := strings.TrimSpace(relationID)
			if trimmed == "" {
				continue
			}
			allowed[trimmed] = struct{}{}
		}
		filtered := make([]models.EcomAssetRelation, 0, len(allowed))
		for _, asset := range assets {
			if _, ok := allowed[asset.ID]; ok {
				filtered = append(filtered, asset)
				delete(allowed, asset.ID)
			}
		}
		if len(allowed) > 0 {
			return nil, "", fmt.Errorf("some selected assets do not belong to product")
		}
		assets = filtered
	}
	if len(assets) == 0 {
		return nil, "", fmt.Errorf("need at least 1 asset to create export task")
	}
	manifest := make([]DownloadAssetItem, 0, len(assets))
	primaryAssetRole := ""
	for _, asset := range assets {
		if asset.IsPrimary && primaryAssetRole == "" {
			primaryAssetRole = asset.AssetRole
		}
		manifestItem := DownloadAssetItem{
			RelationID: asset.ID,
			AssetID:    asset.AssetID,
			AssetRole:  asset.AssetRole,
			IsPrimary:  asset.IsPrimary,
		}
		assetDetail, detailErr := s.assetRepo.FindAssetByID(scope.OrgID, asset.AssetID)
		if detailErr == nil && assetDetail != nil {
			manifestItem.AssetType = assetDetail.AssetType
			manifestItem.FileName = assetDetail.FileName
			manifestItem.MimeType = assetDetail.MimeType
			manifestItem.ContentURL = fmt.Sprintf("/api/v1/ecommerce/assets/%s/content", asset.AssetID)
		}
		manifest = append(manifest, manifestItem)
	}
	return manifest, primaryAssetRole, nil
}

func (s *Service) ensureExportTaskSnapshot(scope repository.Scope, task models.EcomExportTask) models.EcomExportTask {
	changed := false
	if strings.TrimSpace(task.AssetManifest) == "" || task.AssetCount == 0 || strings.TrimSpace(task.PrimaryAssetRole) == "" {
		manifest, primaryAssetRole, err := s.buildProductAssetManifest(scope, task.ProductID, nil)
		if err == nil {
			if manifestJSON, marshalErr := json.Marshal(manifest); marshalErr == nil {
				task.AssetManifest = string(manifestJSON)
				task.AssetCount = len(manifest)
				task.PrimaryAssetRole = primaryAssetRole
				changed = true
			}
		}
	}
	if strings.TrimSpace(task.ListingVersionID) == "" && strings.TrimSpace(task.ListingVersionLabel) == "" {
		if adopted, err := s.repo.GetAdoptedListingVersion(scope, task.ProductID, task.Platform, task.Site, task.Locale); err == nil && adopted != nil {
			task.ListingVersionID = adopted.ID
			task.ListingVersionLabel = adopted.VersionLabel
			changed = true
		}
	}
	if changed {
		if updated, err := s.repo.UpdateExportTask(scope, task); err == nil && updated != nil {
			return *updated
		}
	}
	return task
}

func buildDownloadFileName(product *models.EcomProductSKU, task *models.EcomExportTask) string {
	base := "export"
	if product != nil {
		if strings.TrimSpace(product.SKUCode) != "" {
			base = strings.TrimSpace(product.SKUCode)
		} else if strings.TrimSpace(product.Title) != "" {
			base = strings.ReplaceAll(strings.TrimSpace(product.Title), " ", "_")
		}
	}
	ext := strings.ToLower(strings.TrimSpace(task.Format))
	if ext == "" {
		ext = "zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s.%s", base, strings.ToLower(task.Platform), strings.ToLower(task.Site), strings.ToLower(task.Locale), ext)
}

func (s *Service) buildDownloadListItem(scope repository.Scope, task models.EcomExportTask) DownloadListItem {
	task = s.ensureExportTaskSnapshot(scope, task)
	item := DownloadListItem{
		ID:                  task.ID,
		SourceType:          "product_export",
		ProductID:           task.ProductID,
		ProductPath:         fmt.Sprintf("/products/%s", task.ProductID),
		Platform:            task.Platform,
		Site:                task.Site,
		Locale:              task.Locale,
		Format:              task.Format,
		Status:              task.Status,
		FileSize:            task.FileSize,
		PackageURL:          task.PackageURL,
		ListingVersionID:    task.ListingVersionID,
		ListingVersionLabel: task.ListingVersionLabel,
		Downloadable:        task.Status == models.ExportTaskStatusSucceeded && (strings.TrimSpace(task.PackageURL) != "" || strings.TrimSpace(task.StorageKey) != ""),
		DownloadFileName:    fmt.Sprintf("%s.%s", task.ID, strings.ToLower(task.Format)),
		AssetCount:          task.AssetCount,
		PrimaryAssetRole:    task.PrimaryAssetRole,
		CreatedAt:           task.CreatedAt,
	}

	product, err := s.repo.GetProduct(scope, task.ProductID)
	if err == nil && product != nil {
		item.ProductTitle = product.Title
		item.ProductSKU = product.SKUCode
		item.ProductStatus = product.Status
		item.DownloadFileName = buildDownloadFileName(product, &task)
	}

	if manifest, err := decodeDownloadAssetManifest(task.AssetManifest); err == nil && manifest != nil {
		item.Assets = manifest
		if item.AssetCount == 0 {
			item.AssetCount = len(manifest)
		}
	} else if manifest, primaryAssetRole, buildErr := s.buildProductAssetManifest(scope, task.ProductID, nil); buildErr == nil {
		item.Assets = manifest
		item.AssetCount = len(manifest)
		item.PrimaryAssetRole = primaryAssetRole
	}

	return item
}

// GetProductDetail 获取完整的商品详情
func (s *Service) GetProductDetail(orgID string, productID string) (*ProductDetail, error) {
	scope := repository.Scope{OrgID: orgID}

	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
		return nil, err
	}

	relations, err := s.repo.ListProductAssets(scope, productID)
	if err != nil {
		return nil, err
	}

	assetsWithDetail := make([]ProductAssetWithDetail, 0, len(relations))
	for _, rel := range relations {
		var asset *models.EcommerceAsset
		asset, _ = s.assetRepo.FindAssetByIDGlobal(rel.AssetID)
		assetsWithDetail = append(assetsWithDetail, ProductAssetWithDetail{
			Relation: rel,
			Asset:    asset,
		})
	}

	listings, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return nil, err
	}

	profits, err := s.repo.ListProfitSnapshots(scope, productID)
	if err != nil {
		return nil, err
	}

	exports, err := s.repo.ListExportTasks(scope, productID)
	if err != nil {
		return nil, err
	}

	activities, err := s.repo.ListProductActivities(scope, productID)
	if err != nil {
		return nil, err
	}

	return &ProductDetail{
		Product:         *product,
		Assets:          assetsWithDetail,
		ListingVersions: listings,
		ProfitSnapshots: profits,
		ExportTasks:     exports,
		Activities:      activities,
	}, nil
}

// ==================== Product SKU 基础操作 ====================

// ProductListItem 商品列表项聚合响应
type ProductListItem struct {
	models.EcomProductSKU
	AssetsCount          int  `json:"assets_count"`
	ListingVersionsCount int  `json:"listing_versions_count"`
	HasPrimaryAsset      bool `json:"has_primary_asset"`
}

// ListProducts 列出商品（聚合信息）
func (s *Service) ListProducts(orgID string) ([]ProductListItem, error) {
	scope := repository.Scope{OrgID: orgID}
	products, err := s.repo.ListProducts(scope)
	if err != nil {
		return nil, err
	}

	items := make([]ProductListItem, 0, len(products))
	for _, p := range products {
		item := ProductListItem{
			EcomProductSKU: p,
		}

		// 获取资产计数
		assets, _ := s.repo.ListProductAssets(scope, p.ID)
		item.AssetsCount = len(assets)

		// 检查是否有主资产
		for _, a := range assets {
			if a.IsPrimary {
				item.HasPrimaryAsset = true
				break
			}
		}

		// 获取 listing 版本计数
		listings, _ := s.repo.ListListingVersions(scope, p.ID)
		item.ListingVersionsCount = len(listings)

		items = append(items, item)
	}

	return items, nil
}

// GetProduct 获取单个商品
func (s *Service) GetProduct(orgID string, productID string) (*models.EcomProductSKU, error) {
	return s.repo.GetProduct(repository.Scope{OrgID: orgID}, productID)
}

// CreateProduct 创建商品
func (s *Service) CreateProduct(orgID string, userID string, input CreateProductInput) (*models.EcomProductSKU, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	item := models.EcomProductSKU{
		ID:            uuid.New().String(),
		SKUCode:       input.SKUCode,
		Title:         input.Title,
		SPUID:         input.SPUID,
		CategoryID:    input.CategoryID,
		BrandID:       input.BrandID,
		Tags:          input.Tags,
		Status:        models.ProductStatusDraft,
		AssetStatus:   models.AssetStatusMissing,
		ListingStatus: models.ListingStatusMissing,
		ExportStatus:  models.ExportStatusPending,
	}
	if input.SpecJSON != "" {
		item.SpecJSON = input.SpecJSON
	}
	if input.CostJSON != "" {
		item.CostJSON = input.CostJSON
	}
	if input.CostCurrency != "" {
		item.CostCurrency = input.CostCurrency
	}
	product, err := s.repo.CreateProduct(scope, item)
	if err != nil {
		return nil, err
	}

	// 记录创建活动
	s.logActivity(scope, product.ID, models.ProductActivityTypeStatusChanged,
		"Product Created", "Product created in draft status", nil)
	return product, nil
}

// UpdateProduct 更新商品
func (s *Service) UpdateProduct(orgID string, userID string, productID string, input UpdateProductInput) (*models.EcomProductSKU, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	existing, err := s.GetProduct(orgID, productID)
	if err != nil {
		return nil, err
	}

	if input.Title != "" {
		existing.Title = input.Title
	}
	if input.SKUCode != "" {
		existing.SKUCode = input.SKUCode
	}
	if input.SPUID != "" {
		existing.SPUID = input.SPUID
	}
	if input.CategoryID != "" {
		existing.CategoryID = input.CategoryID
	}
	if input.BrandID != "" {
		existing.BrandID = input.BrandID
	}
	if input.SpecJSON != "" {
		existing.SpecJSON = input.SpecJSON
	}
	if input.CostJSON != "" {
		existing.CostJSON = input.CostJSON
	}
	if input.CostCurrency != "" {
		existing.CostCurrency = input.CostCurrency
	}
	if len(input.Tags) > 0 {
		existing.Tags = input.Tags
	}

	updated, err := s.repo.UpdateProduct(scope, *existing)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UpdateProductStatus 更新商品状态（状态机转换）
func (s *Service) UpdateProductStatus(orgID string, userID string, productID string, newStatus string) (*models.EcomProductSKU, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	product, err := s.GetProduct(orgID, productID)
	if err != nil {
		return nil, err
	}

	// 验证状态转换是否合法（包含前置条件检查）
	if transitionErr := s.validateStatusTransitionWithPreconditions(scope, productID, product.Status, newStatus); transitionErr != nil {
		return nil, transitionErr
	}

	oldStatus := product.Status
	product.Status = newStatus
	product.UpdatedBy = userID
	updated, err := s.repo.UpdateProduct(scope, *product)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeStatusChanged,
		"Status Changed", "Product status changed from "+oldStatus+" to "+newStatus, nil)
	return updated, nil
}

// DeleteProduct 删除商品
func (s *Service) DeleteProduct(orgID string, userID string, productID string) error {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	s.logActivity(scope, productID, models.ProductActivityTypeStatusChanged,
		"Product Deleted", "Product and all related data deleted", nil)

	return s.repo.DeleteProduct(scope, productID)
}

// ==================== Asset 操作 ====================

// ListProductAssets 列出商品关联的资产
func (s *Service) ListProductAssets(orgID string, productID string) ([]ProductAssetWithDetail, error) {
	scope := repository.Scope{OrgID: orgID}
	relations, err := s.repo.ListProductAssets(scope, productID)
	if err != nil {
		return nil, err
	}

	assetsWithDetail := make([]ProductAssetWithDetail, 0, len(relations))
	for _, rel := range relations {
		var asset *models.EcommerceAsset
		asset, _ = s.assetRepo.FindAssetByIDGlobal(rel.AssetID)
		assetsWithDetail = append(assetsWithDetail, ProductAssetWithDetail{
			Relation: rel,
			Asset:    asset,
		})
	}
	return assetsWithDetail, nil
}

// AddProductAsset 添加商品资产关联
func (s *Service) AddProductAsset(orgID string, userID string, productID string, input AddProductAssetInput) (*models.EcomAssetRelation, error) {
	// 输入验证
	if input.AssetID == "" {
		return nil, fmt.Errorf("asset ID is required")
	}
	if !isValidAssetRelationType(input.RelationType) {
		return nil, fmt.Errorf("invalid asset relation type")
	}
	if input.AssetRole == "" {
		return nil, fmt.Errorf("asset role is required")
	}
	if !isValidAssetRole(input.AssetRole) {
		return nil, fmt.Errorf("invalid asset role")
	}

	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}
	if _, err := s.assetRepo.FindAssetByID(orgID, input.AssetID); err != nil {
		return nil, fmt.Errorf("asset not found")
	}
	if _, err := s.repo.FindProductAssetRelation(scope, productID, input.AssetID); err == nil {
		return nil, fmt.Errorf("asset is already linked to product")
	}
	if input.IsPrimary {
		if err := s.repo.ClearPrimaryProductAssets(scope, productID, ""); err != nil {
			return nil, err
		}
	}
	item := models.EcomAssetRelation{
		ID:           uuid.New().String(),
		AssetID:      input.AssetID,
		OwnerType:    models.AssetRelationOwnerTypeProduct,
		OwnerID:      productID,
		RelationType: input.RelationType,
		AssetRole:    input.AssetRole,
		IsPrimary:    input.IsPrimary,
		PlatformCode: input.PlatformCode,
		SiteCode:     input.SiteCode,
		LocaleCode:   input.LocaleCode,
		SortOrder:    input.SortOrder,
	}
	relation, err := s.repo.AddProductAsset(scope, item)
	if err != nil {
		return nil, err
	}

	// 记录活动
	s.logActivity(scope, productID, models.ProductActivityTypeAssetCreated,
		"Asset Added", "Asset role: "+input.AssetRole, nil)

	// 更新商品的资产状态
	s.updateProductAssetStatus(scope, productID)

	// 自动更新主状态
	s.updateProductMainStatus(scope, productID)

	return relation, nil
}

// UpdateProductAsset 更新商品资产关联
func (s *Service) UpdateProductAsset(orgID string, userID string, productID string, assetRelationID string, input UpdateProductAssetInput) (*models.EcomAssetRelation, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}
	relation, err := s.repo.GetProductAssetRelation(scope, productID, assetRelationID)
	if err != nil {
		return nil, err
	}
	if input.RelationType != nil {
		if strings.TrimSpace(*input.RelationType) == "" || !isValidAssetRelationType(*input.RelationType) {
			return nil, fmt.Errorf("invalid asset relation type")
		}
		relation.RelationType = strings.TrimSpace(*input.RelationType)
	}
	if input.AssetRole != nil {
		if strings.TrimSpace(*input.AssetRole) == "" || !isValidAssetRole(*input.AssetRole) {
			return nil, fmt.Errorf("invalid asset role")
		}
		relation.AssetRole = strings.TrimSpace(*input.AssetRole)
	}
	if input.PlatformCode != nil {
		relation.PlatformCode = strings.TrimSpace(*input.PlatformCode)
	}
	if input.SiteCode != nil {
		relation.SiteCode = strings.TrimSpace(*input.SiteCode)
	}
	if input.LocaleCode != nil {
		relation.LocaleCode = strings.TrimSpace(*input.LocaleCode)
	}
	if input.SortOrder != nil {
		relation.SortOrder = *input.SortOrder
	}
	if input.IsPrimary != nil {
		relation.IsPrimary = *input.IsPrimary
		if *input.IsPrimary {
			if clearErr := s.repo.ClearPrimaryProductAssets(scope, productID, relation.ID); clearErr != nil {
				return nil, clearErr
			}
		}
	}
	updated, err := s.repo.UpdateProductAssetRelation(scope, *relation)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeAssetCreated,
		"Asset Relation Updated", "Asset relation metadata updated for role "+updated.AssetRole, nil)
	s.updateProductAssetStatus(scope, productID)
	s.updateProductMainStatus(scope, productID)
	return updated, nil
}

// DeleteProductAsset 删除商品资产关联
func (s *Service) DeleteProductAsset(orgID string, userID string, productID string, assetRelationID string) error {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	// 删除关联
	if err := s.repo.DeleteProductAsset(scope, assetRelationID); err != nil {
		return err
	}

	// 记录活动
	s.logActivity(scope, productID, models.ProductActivityTypeAssetDeleted,
		"Asset Deleted", "Asset relation ID: "+assetRelationID, nil)

	// 更新商品的资产状态
	s.updateProductAssetStatus(scope, productID)

	// 自动更新主状态
	s.updateProductMainStatus(scope, productID)

	return nil
}

// updateProductAssetStatus 更新商品的资产状态
func (s *Service) updateProductAssetStatus(scope repository.Scope, productID string) {
	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
		return
	}

	assets, err := s.repo.ListProductAssets(scope, productID)
	if err != nil {
		return
	}

	var newStatus string
	if len(assets) == 0 {
		newStatus = models.AssetStatusMissing
	} else {
		// 检查是否有 Hero、Model Shot、Scene Shot、Detail Shot 等关键角色
		hasHero := false
		hasModel := false
		for _, a := range assets {
			if a.AssetRole == models.AssetRoleHero {
				hasHero = true
			}
			if a.AssetRole == models.AssetRoleModelShot {
				hasModel = true
			}
		}
		if hasHero && hasModel {
			newStatus = models.AssetStatusReady
		} else {
			newStatus = models.AssetStatusPartial
		}
	}
	product.AssetStatus = newStatus
	s.repo.UpdateProduct(scope, *product)
}

// ==================== Listing Version 操作 ====================

// ListListingVersions 列出商品的 Listing 版本
func (s *Service) ListListingVersions(orgID string, productID string) ([]models.EcomListingVersion, error) {
	return s.repo.ListListingVersions(repository.Scope{OrgID: orgID}, productID)
}

// CreateListingVersion 创建 Listing 版本
func (s *Service) CreateListingVersion(orgID string, userID string, productID string, input CreateListingVersionInput) (*models.EcomListingVersion, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}

	versionNo, _ := s.repo.GetNextListingVersionNo(scope, productID)

	item := models.EcomListingVersion{
		ID:           uuid.New().String(),
		ProductID:    productID,
		VersionNo:    versionNo,
		VersionLabel: input.VersionLabel,
		Status:       models.ListingVersionStatusDraft,
		Title:        input.Title,
		Description:  input.Description,
		BulletPoints: input.BulletPoints,
		Keywords:     input.Keywords,
		Platform:     input.Platform,
		Site:         input.Site,
		Locale:       input.Locale,
	}
	listing, err := s.repo.CreateListingVersion(scope, item)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingGenerated,
		"Listing Created", "Listing version "+input.VersionLabel+" created for "+input.Platform, nil)

	// 新建版本后至少应进入 partial，避免商品状态与实际版本数不一致。
	s.updateProductListingStatus(scope, productID)
	s.updateProductMainStatus(scope, productID)

	return listing, nil
}

func (s *Service) BatchCreateListingVersions(orgID string, userID string, input BatchCreateListingVersionsInput) (*BatchListingMutationResult, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("at least 1 listing item is required")
	}

	result := &BatchListingMutationResult{
		Total: len(input.Items),
		Items: make([]BatchListingMutationItem, 0, len(input.Items)),
	}
	seenProductIDs := make(map[string]struct{}, len(input.Items))

	for _, entry := range input.Items {
		item := BatchListingMutationItem{
			ProductID:    entry.ProductID,
			VersionLabel: strings.TrimSpace(entry.VersionLabel),
		}

		product, err := s.repo.GetProduct(scope, entry.ProductID)
		if err == nil && product != nil {
			item.SKUCode = product.SKUCode
			item.ProductTitle = product.Title
		}

		switch {
		case strings.TrimSpace(entry.ProductID) == "":
			item.Message = "product_id is required"
		case strings.TrimSpace(entry.VersionLabel) == "":
			item.Message = "version_label is required"
		case strings.TrimSpace(entry.Title) == "":
			item.Message = "title is required"
		case strings.TrimSpace(entry.Platform) == "":
			item.Message = "platform is required"
		case strings.TrimSpace(entry.Site) == "":
			item.Message = "site is required"
		case strings.TrimSpace(entry.Locale) == "":
			item.Message = "locale is required"
		case err != nil:
			item.Message = "product not found"
		default:
			if _, exists := seenProductIDs[entry.ProductID]; exists {
				item.Message = "duplicate product in batch request"
			} else {
				seenProductIDs[entry.ProductID] = struct{}{}
				listing, createErr := s.CreateListingVersion(orgID, userID, entry.ProductID, CreateListingVersionInput{
					VersionLabel: strings.TrimSpace(entry.VersionLabel),
					Title:        strings.TrimSpace(entry.Title),
					Description:  strings.TrimSpace(entry.Description),
					BulletPoints: entry.BulletPoints,
					Keywords:     entry.Keywords,
					Platform:     strings.TrimSpace(entry.Platform),
					Site:         strings.TrimSpace(entry.Site),
					Locale:       strings.TrimSpace(entry.Locale),
				})
				if createErr != nil {
					item.Message = createErr.Error()
				} else {
					item.Success = true
					item.Message = "listing version created"
					item.VersionID = listing.ID
					item.Listing = listing
					result.Succeeded++
				}
			}
		}

		if !item.Success {
			result.Failed++
		}
		result.Items = append(result.Items, item)
	}

	return result, nil
}

// AdoptListingVersion 采用 Listing 版本
func (s *Service) AdoptListingVersion(orgID string, userID string, productID string, versionID string) (*models.EcomListingVersion, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	versions, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return nil, err
	}

	var target *models.EcomListingVersion
	for _, v := range versions {
		if v.ID == versionID {
			target = &v
			break
		}
	}
	if target == nil {
		return nil, nil
	}

	// 将其他版本的状态改为非 adopted
	now := time.Now()
	for _, v := range versions {
		if v.ID != versionID && v.Status == models.ListingVersionStatusAdopted {
			v.Status = models.ListingVersionStatusReady
			s.repo.UpdateListingVersion(scope, v)
		}
	}

	target.Status = models.ListingVersionStatusAdopted
	target.AdoptedAt = &now
	updated, err := s.repo.UpdateListingVersion(scope, *target)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingAdopted,
		"Listing Adopted", "Listing version "+target.VersionLabel+" adopted", nil)

	// 更新商品的 listing 状态
	s.updateProductListingStatus(scope, productID)

	// 自动更新主状态
	s.updateProductMainStatus(scope, productID)

	return updated, nil
}

func (s *Service) BatchAdoptListingVersions(orgID string, userID string, input BatchAdoptListingVersionsInput) (*BatchListingMutationResult, error) {
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("at least 1 adopt item is required")
	}

	result := &BatchListingMutationResult{
		Total: len(input.Items),
		Items: make([]BatchListingMutationItem, 0, len(input.Items)),
	}
	seenProductIDs := make(map[string]struct{}, len(input.Items))

	for _, entry := range input.Items {
		item := BatchListingMutationItem{
			ProductID: entry.ProductID,
			VersionID: entry.VersionID,
		}

		product, err := s.repo.GetProduct(repository.Scope{OrgID: orgID}, entry.ProductID)
		if err == nil && product != nil {
			item.SKUCode = product.SKUCode
			item.ProductTitle = product.Title
		}

		switch {
		case strings.TrimSpace(entry.ProductID) == "":
			item.Message = "product_id is required"
		case strings.TrimSpace(entry.VersionID) == "":
			item.Message = "version_id is required"
		case err != nil:
			item.Message = "product not found"
		default:
			if _, exists := seenProductIDs[entry.ProductID]; exists {
				item.Message = "duplicate product in batch request"
			} else {
				seenProductIDs[entry.ProductID] = struct{}{}
				listing, adoptErr := s.AdoptListingVersion(orgID, userID, entry.ProductID, entry.VersionID)
				if adoptErr != nil {
					item.Message = adoptErr.Error()
				} else if listing == nil {
					item.Message = "listing version not found"
				} else {
					item.Success = true
					item.Message = "listing version adopted"
					item.VersionID = listing.ID
					item.VersionLabel = listing.VersionLabel
					item.Listing = listing
					result.Succeeded++
				}
			}
		}

		if !item.Success {
			result.Failed++
		}
		result.Items = append(result.Items, item)
	}

	return result, nil
}

func (s *Service) UpdateListingVersion(orgID string, userID string, productID string, versionID string, input UpdateListingVersionInput) (*models.EcomListingVersion, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}

	versions, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return nil, err
	}

	var target *models.EcomListingVersion
	for _, version := range versions {
		if version.ID == versionID {
			current := version
			target = &current
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("listing version not found")
	}

	if input.VersionLabel != nil {
		target.VersionLabel = strings.TrimSpace(*input.VersionLabel)
	}
	if input.Title != nil {
		target.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		target.Description = strings.TrimSpace(*input.Description)
	}
	if input.BulletPoints != nil {
		target.BulletPoints = sanitizeStringList(*input.BulletPoints)
	}
	if input.Keywords != nil {
		target.Keywords = sanitizeStringList(*input.Keywords)
	}
	if input.Platform != nil {
		target.Platform = strings.TrimSpace(*input.Platform)
	}
	if input.Site != nil {
		target.Site = strings.TrimSpace(*input.Site)
	}
	if input.Locale != nil {
		target.Locale = strings.TrimSpace(*input.Locale)
	}

	if target.VersionLabel == "" {
		return nil, fmt.Errorf("version label is required")
	}
	if target.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if target.Platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	if target.Site == "" {
		return nil, fmt.Errorf("site is required")
	}
	if target.Locale == "" {
		return nil, fmt.Errorf("locale is required")
	}

	updated, err := s.repo.UpdateListingVersion(scope, *target)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingGenerated,
		"Listing Updated", "Listing version "+target.VersionLabel+" updated", nil)

	s.updateProductListingStatus(scope, productID)
	s.updateProductMainStatus(scope, productID)

	return updated, nil
}

// DeleteListingVersion 删除 Listing 版本
func (s *Service) DeleteListingVersion(orgID string, userID string, productID string, versionID string) error {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	// 先获取版本信息用于记录
	versions, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return err
	}

	var versionLabel string
	for _, v := range versions {
		if v.ID == versionID {
			versionLabel = v.VersionLabel
			break
		}
	}

	// 删除版本
	if err := s.repo.DeleteListingVersion(scope, productID, versionID); err != nil {
		return err
	}

	// 记录活动
	s.logActivity(scope, productID, models.ProductActivityTypeListingDeleted,
		"Listing Deleted", "Listing version "+versionLabel+" deleted", nil)

	// 更新商品的 listing 状态
	s.updateProductListingStatus(scope, productID)

	// 自动更新主状态
	s.updateProductMainStatus(scope, productID)

	return nil
}

// validateStatusTransitionWithPreconditions 验证状态转换是否合法（包含前置条件检查）
func (s *Service) validateStatusTransitionWithPreconditions(scope repository.Scope, productID string, fromStatus string, toStatus string) error {
	// 先检查基本转换规则
	if !isValidStatusTransition(fromStatus, toStatus) {
		return fmt.Errorf("invalid status transition from %s to %s", fromStatus, toStatus)
	}

	// 根据目标状态检查前置条件
	switch toStatus {
	case models.ProductStatusAssetsReady:
		assetCount, err := s.repo.GetProductAssetCount(scope, productID)
		if err != nil {
			return err
		}
		if assetCount < 1 {
			return fmt.Errorf("need at least 1 asset to move to assets_ready status")
		}
	case models.ProductStatusListingReady:
		hasAdopted, err := s.repo.HasAdoptedListingVersion(scope, productID)
		if err != nil {
			return err
		}
		if !hasAdopted {
			return fmt.Errorf("need at least 1 adopted listing to move to listing_ready status")
		}
	case models.ProductStatusExportReady:
		hasExport, err := s.repo.HasSuccessfulExportTask(scope, productID)
		if err != nil {
			return err
		}
		if !hasExport {
			return fmt.Errorf("need at least 1 successful export to move to export_ready status")
		}
	}

	return nil
}

// updateProductListingStatus 更新商品的 listing 状态
func (s *Service) updateProductListingStatus(scope repository.Scope, productID string) {
	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
		return
	}

	listings, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return
	}

	hasAdopted := false
	for _, l := range listings {
		if l.Status == models.ListingVersionStatusAdopted {
			hasAdopted = true
			break
		}
	}

	var newStatus string
	if hasAdopted {
		newStatus = models.ListingStatusReady
	} else if len(listings) > 0 {
		newStatus = models.ListingStatusPartial
	} else {
		newStatus = models.ListingStatusMissing
	}
	product.ListingStatus = newStatus
	s.repo.UpdateProduct(scope, *product)
}

// ==================== Profit Snapshot 操作 ====================

// ListProfitSnapshots 列出商品的利润快照
func (s *Service) ListProfitSnapshots(orgID string, productID string) ([]models.EcomProfitSnapshot, error) {
	return s.repo.ListProfitSnapshots(repository.Scope{OrgID: orgID}, productID)
}

// CalculateProfit 计算利润并创建快照
func (s *Service) CalculateProfit(orgID string, userID string, productID string, input CalculateProfitInput) (*models.EcomProfitSnapshot, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	// 计算利润
	grossProfit := input.ListingPrice - input.CostPrice
	netProfit := grossProfit - input.LogisticsCost - input.PlatformFee - input.OtherFee
	var grossMargin, netMargin float64
	if input.ListingPrice > 0 {
		grossMargin = grossProfit / input.ListingPrice
		netMargin = netProfit / input.ListingPrice
	}
	// 计算盈亏平衡价格
	breakevenPrice := input.CostPrice + input.LogisticsCost + input.PlatformFee + input.OtherFee

	item := models.EcomProfitSnapshot{
		ID:             uuid.New().String(),
		ProductID:      productID,
		Platform:       input.Platform,
		Site:           input.Site,
		CostPrice:      input.CostPrice,
		ListingPrice:   input.ListingPrice,
		LogisticsCost:  input.LogisticsCost,
		PlatformFee:    input.PlatformFee,
		OtherFee:       input.OtherFee,
		GrossProfit:    grossProfit,
		NetProfit:      netProfit,
		GrossMargin:    grossMargin,
		NetMargin:      netMargin,
		BreakevenPrice: breakevenPrice,
	}

	snapshot, err := s.repo.CreateProfitSnapshot(scope, item)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeProfitCalculated,
		"Profit Calculated", "Profit snapshot created with net margin of "+formatPercent(netMargin), nil)

	return snapshot, nil
}

// ==================== Export Task 操作 ====================

// ListExportTasks 列出商品的导出任务
func (s *Service) ListExportTasks(orgID string, productID string) ([]models.EcomExportTask, error) {
	scope := repository.Scope{OrgID: orgID}
	tasks, err := s.repo.ListExportTasks(scope, productID)
	if err != nil {
		return nil, err
	}
	items := make([]models.EcomExportTask, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, s.ensureExportTaskSnapshot(scope, task))
	}
	return items, nil
}

// CreateExportTask 创建导出任务
func (s *Service) CreateExportTask(orgID string, userID string, productID string, input CreateExportTaskInput) (*models.EcomExportTask, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}
	manifest, primaryAssetRole, err := s.buildProductAssetManifest(scope, productID, input.AssetRelationIDs)
	if err != nil {
		return nil, err
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	var listingVersionID string
	var listingVersionLabel string
	if adopted, adoptedErr := s.repo.GetAdoptedListingVersion(scope, productID, input.Platform, input.Site, input.Locale); adoptedErr == nil && adopted != nil {
		listingVersionID = adopted.ID
		listingVersionLabel = adopted.VersionLabel
	}

	item := models.EcomExportTask{
		ID:                  uuid.New().String(),
		ProductID:           productID,
		Status:              models.ExportTaskStatusPending,
		Platform:            input.Platform,
		Site:                input.Site,
		Locale:              input.Locale,
		Format:              input.Format,
		ListingVersionID:    listingVersionID,
		ListingVersionLabel: listingVersionLabel,
		PrimaryAssetRole:    primaryAssetRole,
		AssetCount:          len(manifest),
		AssetManifest:       string(manifestJSON),
	}

	task, err := s.repo.CreateExportTask(scope, item)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeExportCreated,
		"Export Task Created", fmt.Sprintf("Export task created for %s/%s with %d selected assets", input.Platform, input.Format, len(manifest)), nil)

	// 更新商品的 export 状态
	product, _ := s.repo.GetProduct(scope, productID)
	if product != nil {
		product.ExportStatus = models.ExportStatusPending
		s.repo.UpdateProduct(scope, *product)
		s.updateProductMainStatus(scope, productID)
	}

	return task, nil
}

// UpdateExportTaskStatus 更新导出任务状态
func (s *Service) UpdateExportTaskStatus(orgID string, userID string, productID string, taskID string, status string, storageKey string, packageURL string, fileSize string) (*models.EcomExportTask, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	tasks, err := s.repo.ListExportTasks(scope, productID)
	if err != nil {
		return nil, err
	}

	var target *models.EcomExportTask
	for _, t := range tasks {
		if t.ID == taskID {
			target = &t
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("export task not found")
	}

	oldStatus := target.Status
	target.Status = status
	if storageKey != "" {
		target.StorageKey = storageKey
	}
	if packageURL != "" {
		target.PackageURL = packageURL
	}
	if fileSize != "" {
		target.FileSize = fileSize
	}

	updated, err := s.repo.UpdateExportTask(scope, *target)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeStatusChanged,
		"Export Task Updated", "Export task status changed from "+oldStatus+" to "+status, nil)

	// 更新商品的 export 状态
	if status == models.ExportTaskStatusSucceeded {
		product, _ := s.repo.GetProduct(scope, productID)
		if product != nil {
			product.ExportStatus = models.ExportStatusReady
			s.repo.UpdateProduct(scope, *product)
			s.updateProductMainStatus(scope, productID)
		}
	}

	return updated, nil
}

// ListDownloads 列出组织下的下载中心聚合记录
func (s *Service) ListDownloads(orgID string) ([]DownloadListItem, error) {
	scope := repository.Scope{OrgID: orgID}
	tasks, err := s.repo.ListAllExportTasks(scope)
	if err != nil {
		return nil, err
	}

	items := make([]DownloadListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, s.buildDownloadListItem(scope, task))
	}
	return items, nil
}

// GetDownloadContent 获取导出包下载内容
func (s *Service) GetDownloadContent(orgID string, downloadID string) (*DownloadListItem, io.ReadCloser, http.Header, error) {
	scope := repository.Scope{OrgID: orgID}
	task, err := s.repo.GetExportTask(scope, downloadID)
	if err != nil {
		return nil, nil, nil, err
	}

	item := s.buildDownloadListItem(scope, *task)
	if !item.Downloadable {
		return &item, nil, nil, fmt.Errorf("download is not ready")
	}

	if strings.TrimSpace(task.StorageKey) != "" {
		if s.platform == nil {
			return &item, nil, nil, fmt.Errorf("platform client is required")
		}
		body, headers, err := s.platform.DownloadAsset(task.StorageKey)
		if err != nil {
			return &item, nil, nil, err
		}
		if headers == nil {
			headers = http.Header{}
		}
		headers.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.DownloadFileName))
		return &item, body, headers, nil
	}

	return &item, nil, nil, fmt.Errorf("download content is not available")
}

// ==================== 活动记录 ====================

// logActivity 记录商品活动
func (s *Service) logActivity(scope repository.Scope, productID string, activityType string, title string, summary string, _ map[string]string) {
	item := models.EcomProductActivity{
		ID:        uuid.New().String(),
		ProductID: productID,
		Type:      activityType,
		Title:     title,
		Summary:   summary,
	}
	s.repo.CreateProductActivity(scope, item)
}

// ==================== 请求/响应类型 ====================

type CreateProductInput struct {
	SKUCode      string   `json:"sku_code" binding:"required"`
	Title        string   `json:"title" binding:"required"`
	SPUID        string   `json:"spu_id,omitempty"`
	CategoryID   string   `json:"category_id,omitempty"`
	BrandID      string   `json:"brand_id,omitempty"`
	SpecJSON     string   `json:"spec_json,omitempty"`
	CostJSON     string   `json:"cost_json,omitempty"`
	CostCurrency string   `json:"cost_currency,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type UpdateProductInput struct {
	SKUCode      string   `json:"sku_code,omitempty"`
	Title        string   `json:"title,omitempty"`
	SPUID        string   `json:"spu_id,omitempty"`
	CategoryID   string   `json:"category_id,omitempty"`
	BrandID      string   `json:"brand_id,omitempty"`
	SpecJSON     string   `json:"spec_json,omitempty"`
	CostJSON     string   `json:"cost_json,omitempty"`
	CostCurrency string   `json:"cost_currency,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type AddProductAssetInput struct {
	AssetID      string `json:"asset_id" binding:"required"`
	RelationType string `json:"relation_type" binding:"required"`
	AssetRole    string `json:"asset_role" binding:"required"`
	IsPrimary    bool   `json:"is_primary"`
	PlatformCode string `json:"platform_code,omitempty"`
	SiteCode     string `json:"site_code,omitempty"`
	LocaleCode   string `json:"locale_code,omitempty"`
	SortOrder    int    `json:"sort_order,omitempty"`
}

type UpdateProductAssetInput struct {
	RelationType *string `json:"relation_type,omitempty"`
	AssetRole    *string `json:"asset_role,omitempty"`
	IsPrimary    *bool   `json:"is_primary,omitempty"`
	PlatformCode *string `json:"platform_code,omitempty"`
	SiteCode     *string `json:"site_code,omitempty"`
	LocaleCode   *string `json:"locale_code,omitempty"`
	SortOrder    *int    `json:"sort_order,omitempty"`
}

type BatchCreateListingVersionItemInput struct {
	ProductID    string   `json:"product_id"`
	VersionLabel string   `json:"version_label"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	BulletPoints []string `json:"bullet_points"`
	Keywords     []string `json:"keywords"`
	Platform     string   `json:"platform"`
	Site         string   `json:"site"`
	Locale       string   `json:"locale"`
}

type BatchCreateListingVersionsInput struct {
	Items []BatchCreateListingVersionItemInput `json:"items" binding:"required"`
}

type BatchAdoptListingVersionItemInput struct {
	ProductID string `json:"product_id"`
	VersionID string `json:"version_id"`
}

type BatchAdoptListingVersionsInput struct {
	Items []BatchAdoptListingVersionItemInput `json:"items" binding:"required"`
}

type BatchListingMutationItem struct {
	ProductID    string                     `json:"product_id"`
	SKUCode      string                     `json:"sku_code,omitempty"`
	ProductTitle string                     `json:"product_title,omitempty"`
	VersionID    string                     `json:"version_id,omitempty"`
	VersionLabel string                     `json:"version_label,omitempty"`
	Success      bool                       `json:"success"`
	Message      string                     `json:"message,omitempty"`
	Listing      *models.EcomListingVersion `json:"listing,omitempty"`
}

type BatchListingMutationResult struct {
	Total     int                        `json:"total"`
	Succeeded int                        `json:"succeeded"`
	Failed    int                        `json:"failed"`
	Items     []BatchListingMutationItem `json:"items"`
}

type CreateListingVersionInput struct {
	VersionLabel string   `json:"version_label" binding:"required"`
	Title        string   `json:"title" binding:"required"`
	Description  string   `json:"description"`
	BulletPoints []string `json:"bullet_points"`
	Keywords     []string `json:"keywords"`
	Platform     string   `json:"platform" binding:"required"`
	Site         string   `json:"site" binding:"required"`
	Locale       string   `json:"locale" binding:"required"`
}

type UpdateListingVersionInput struct {
	VersionLabel *string   `json:"version_label,omitempty"`
	Title        *string   `json:"title,omitempty"`
	Description  *string   `json:"description,omitempty"`
	BulletPoints *[]string `json:"bullet_points,omitempty"`
	Keywords     *[]string `json:"keywords,omitempty"`
	Platform     *string   `json:"platform,omitempty"`
	Site         *string   `json:"site,omitempty"`
	Locale       *string   `json:"locale,omitempty"`
}

type CalculateProfitInput struct {
	Platform      string  `json:"platform" binding:"required"`
	Site          string  `json:"site" binding:"required"`
	CostPrice     float64 `json:"cost_price" binding:"required"`
	ListingPrice  float64 `json:"listing_price" binding:"required"`
	LogisticsCost float64 `json:"logistics_cost"`
	PlatformFee   float64 `json:"platform_fee"`
	OtherFee      float64 `json:"other_fee"`
}

type CreateExportTaskInput struct {
	Platform         string   `json:"platform" binding:"required"`
	Site             string   `json:"site" binding:"required"`
	Locale           string   `json:"locale" binding:"required"`
	Format           string   `json:"format" binding:"required"`
	AssetRelationIDs []string `json:"asset_relation_ids,omitempty"`
}
