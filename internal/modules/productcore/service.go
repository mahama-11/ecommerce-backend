package productcore

import (
	"archive/zip"
	"bytes"
	"ecommerce-service/internal/billinggate"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Service struct {
	repo      *repository.ProductCenterRepository
	assetRepo *repository.ImageRuntimeRepository
	platform  *platform.Client
}

const (
	listingTitleMaxRunes       = 200
	listingBulletMaxCount      = 5
	listingBulletMaxRunes      = 250
	listingDescriptionMaxRunes = 2000
)

var baselineSensitiveWords = []string{
	"counterfeit", "fake", "replica", "knockoff", "weapon", "gun", "drug", "cocaine", "heroin", "porn", "adult only",
	"假货", "仿牌", "高仿", "违禁", "毒品", "枪支", "色情",
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

func runeLen(value string) int { return len([]rune(value)) }

func containsBaselineSensitiveWord(values ...string) string {
	for _, value := range values {
		lower := strings.ToLower(value)
		for _, word := range baselineSensitiveWords {
			if strings.Contains(lower, strings.ToLower(word)) {
				return word
			}
		}
	}
	return ""
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
	ExportTasks     []ExportTaskSummary          `json:"export_tasks"`
	Activities      []models.EcomProductActivity `json:"activities"`
}

// ProductAssetWithDetail 资产关联加上资产详情
type ProductAssetWithDetail struct {
	Relation models.EcomAssetRelation `json:"relation"`
	Asset    *models.EcommerceAsset   `json:"asset,omitempty"`
}

type ExportTaskSummary struct {
	ID                  string    `json:"id"`
	ProductID           string    `json:"product_id"`
	PackageID           string    `json:"package_id,omitempty"`
	Status              string    `json:"status"`
	Platform            string    `json:"platform"`
	Site                string    `json:"site"`
	Locale              string    `json:"locale"`
	Format              string    `json:"format"`
	ListingVersionID    string    `json:"listing_version_id,omitempty"`
	ListingVersionLabel string    `json:"listing_version_label,omitempty"`
	PrimaryAssetRole    string    `json:"primary_asset_role,omitempty"`
	AssetCount          int       `json:"asset_count"`
	AssetManifest       string    `json:"-"`
	FileSize            string    `json:"file_size,omitempty"`
	ContentURL          string    `json:"content_url"`
	CreatedAt           time.Time `json:"created_at"`
}

func exportTaskSummary(task models.EcomExportTask) ExportTaskSummary {
	return ExportTaskSummary{
		ID:                  task.ID,
		ProductID:           task.ProductID,
		PackageID:           task.PackageID,
		Status:              task.Status,
		Platform:            task.Platform,
		Site:                task.Site,
		Locale:              task.Locale,
		Format:              task.Format,
		ListingVersionID:    task.ListingVersionID,
		ListingVersionLabel: task.ListingVersionLabel,
		PrimaryAssetRole:    task.PrimaryAssetRole,
		AssetCount:          task.AssetCount,
		AssetManifest:       task.AssetManifest,
		FileSize:            task.FileSize,
		ContentURL:          fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", task.ID),
		CreatedAt:           task.CreatedAt,
	}
}

func exportTaskSummaries(tasks []models.EcomExportTask) []ExportTaskSummary {
	out := make([]ExportTaskSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, exportTaskSummary(task))
	}
	return out
}

// DownloadListItem 下载中心聚合项
type DownloadListItem struct {
	ID                  string              `json:"id"`
	TaskID              string              `json:"task_id,omitempty"`
	PackageID           string              `json:"package_id,omitempty"`
	SourceType          string              `json:"source_type"`
	ProductID           string              `json:"product_id,omitempty"`
	ProductTitle        string              `json:"product_title,omitempty"`
	ProductSKU          string              `json:"product_sku,omitempty"`
	ProductStatus       string              `json:"product_status,omitempty"`
	ProductPath         string              `json:"product_path,omitempty"`
	SKUCount            int                 `json:"sku_count,omitempty"`
	SucceededCount      int                 `json:"succeeded_count,omitempty"`
	FailedCount         int                 `json:"failed_count,omitempty"`
	Platform            string              `json:"platform"`
	Site                string              `json:"site"`
	Locale              string              `json:"locale"`
	Schema              string              `json:"schema,omitempty"`
	Format              string              `json:"format"`
	Status              string              `json:"status"`
	FileSize            string              `json:"file_size,omitempty"`
	PackageURL          string              `json:"-"`
	ContentURL          string              `json:"content_url"`
	ManifestURL         string              `json:"manifest_url,omitempty"`
	Package             DownloadPackage     `json:"package"`
	ListingVersionID    string              `json:"listing_version_id,omitempty"`
	ListingVersionLabel string              `json:"listing_version_label,omitempty"`
	DownloadFileName    string              `json:"download_file_name"`
	Downloadable        bool                `json:"downloadable"`
	AssetCount          int                 `json:"asset_count"`
	PrimaryAssetRole    string              `json:"primary_asset_role,omitempty"`
	Assets              []DownloadAssetItem `json:"assets,omitempty"`
	CreatedAt           time.Time           `json:"created_at"`
}

type DownloadPackage struct {
	FileName          string `json:"file_name"`
	FileSize          string `json:"file_size,omitempty"`
	ContentType       string `json:"content_type"`
	ManifestAvailable bool   `json:"manifest_available"`
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
		TaskID:              task.ID,
		PackageID:           task.PackageID,
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
		ContentURL:          fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", task.ID),
		ListingVersionID:    task.ListingVersionID,
		ListingVersionLabel: task.ListingVersionLabel,
		Downloadable:        task.Status == models.ExportTaskStatusSucceeded && (strings.TrimSpace(task.PackageURL) != "" || strings.TrimSpace(task.StorageKey) != ""),
		DownloadFileName:    fmt.Sprintf("%s.%s", task.ID, strings.ToLower(task.Format)),
		AssetCount:          task.AssetCount,
		PrimaryAssetRole:    task.PrimaryAssetRole,
		CreatedAt:           task.CreatedAt,
	}

	if product, err := s.repo.GetProduct(scope, task.ProductID); err == nil && product != nil {
		item.ProductTitle = product.Title
		item.ProductSKU = product.SKUCode
		item.ProductStatus = product.Status
		item.DownloadFileName = buildDownloadFileName(product, &task)
	}
	item.Package = DownloadPackage{
		FileName:          item.DownloadFileName,
		FileSize:          item.FileSize,
		ContentType:       contentTypeForExportFormat(item.Format),
		ManifestAvailable: strings.TrimSpace(task.AssetManifest) != "",
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

func contentTypeForExportFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return "text/csv"
	case "json":
		return "application/json"
	case "zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
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
		if asset != nil {
			assetCopy := *asset
			assetCopy.StorageKey = ""
			assetCopy.Metadata = ""
			asset = &assetCopy
		}
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
		ExportTasks:     exportTaskSummaries(exports),
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
		Tags:          pq.StringArray(input.Tags),
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
		existing.Tags = pq.StringArray(input.Tags)
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
	product, err := s.repo.GetProduct(scope, strings.TrimSpace(productID))
	if err != nil {
		return nil, err
	}
	return s.createListingVersionForProduct(scope, product, input, false)
}

func (s *Service) createListingVersionForProduct(scope repository.Scope, product *models.EcomProductSKU, input CreateListingVersionInput, preview bool) (*models.EcomListingVersion, error) {
	if product == nil {
		return nil, fmt.Errorf("product not found")
	}
	input = normalizeCreateListingInput(input)
	assetReady := product.AssetStatus == models.AssetStatusReady
	if !assetReady {
		if relations, relErr := s.repo.ListProductAssets(scope, product.ID); relErr == nil && len(relations) > 0 {
			assetReady = true
		}
	}
	if err := validateListingInput(product, input, assetReady); err != nil {
		return nil, err
	}
	versionNo, _ := s.repo.GetNextListingVersionNo(scope, product.ID)
	versionID := uuid.New().String()
	item := models.EcomListingVersion{
		ID:           versionID,
		ProductID:    product.ID,
		VersionNo:    versionNo,
		VersionLabel: input.VersionLabel,
		Status:       models.ListingVersionStatusDraft,
		Title:        input.Title,
		Description:  input.Description,
		BulletPoints: pq.StringArray(input.BulletPoints),
		Keywords:     pq.StringArray(input.Keywords),
		Platform:     input.Platform,
		Site:         input.Site,
		Locale:       input.Locale,
	}
	if preview {
		item.OrganizationID = scope.OrgID
		item.CreatedBy = scope.UserID
		return &item, nil
	}

	chargeCtx, err := s.prepareListingChargeContext(scope, product, item)
	if err != nil {
		return nil, err
	}
	listing, err := s.repo.CreateListingVersion(scope, item)
	if err != nil {
		_ = billinggate.New(s.platform).Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "listing_create_failed"})
		return nil, err
	}
	gate := billinggate.New(s.platform)
	if err := gate.MarkReserved(chargeCtx, map[string]any{"listing_version_id": listing.ID, "product_id": product.ID}); err != nil {
		_ = gate.Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "listing_mark_reserved_failed"})
		return nil, err
	}
	if _, err := gate.Commit(billinggate.CommitInput{
		Context:      chargeCtx,
		SourceAction: "listing_version_created",
		EventID:      fmt.Sprintf("evt_%s", listing.ID),
		Dimensions: map[string]any{"platform": listing.Platform, "site": listing.Site, "locale": listing.Locale,
			"sku_code": product.SKUCode},
		Metadata: map[string]any{"listing_version_id": listing.ID, "product_id": product.ID},
	}); err != nil {
		_ = gate.Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "listing_commit_failed"})
		return nil, err
	}

	s.logActivity(scope, product.ID, models.ProductActivityTypeListingGenerated,
		"Listing Created", "Listing version "+input.VersionLabel+" created for "+input.Platform, nil)

	// 新建版本后至少应进入 partial，避免商品状态与实际版本数不一致。
	s.updateProductListingStatus(scope, product.ID)
	s.updateProductMainStatus(scope, product.ID)

	return listing, nil
}

func normalizeCreateListingInput(input CreateListingVersionInput) CreateListingVersionInput {
	input.VersionLabel = strings.TrimSpace(input.VersionLabel)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Site = strings.TrimSpace(input.Site)
	input.Locale = strings.TrimSpace(input.Locale)
	input.BulletPoints = sanitizeStringList(input.BulletPoints)
	input.Keywords = sanitizeStringList(input.Keywords)
	return input
}

func validateListingInput(product *models.EcomProductSKU, input CreateListingVersionInput, assetReady bool) error {
	if strings.TrimSpace(input.VersionLabel) == "" {
		return fmt.Errorf("version label is required")
	}
	if input.Title == "" {
		return fmt.Errorf("title is required")
	}
	if runeLen(input.Title) > listingTitleMaxRunes {
		return fmt.Errorf("title length must be <= %d characters", listingTitleMaxRunes)
	}
	if runeLen(input.Description) > listingDescriptionMaxRunes {
		return fmt.Errorf("description length must be <= %d characters", listingDescriptionMaxRunes)
	}
	if len(input.BulletPoints) > listingBulletMaxCount {
		return fmt.Errorf("bullet point count must be <= %d", listingBulletMaxCount)
	}
	for i, bullet := range input.BulletPoints {
		if runeLen(bullet) > listingBulletMaxRunes {
			return fmt.Errorf("bullet point %d length must be <= %d characters", i+1, listingBulletMaxRunes)
		}
	}
	if input.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	if input.Site == "" {
		return fmt.Errorf("site is required")
	}
	if input.Locale == "" {
		return fmt.Errorf("locale is required")
	}
	if product == nil || strings.TrimSpace(product.SKUCode) == "" {
		return fmt.Errorf("sku_code is required")
	}
	if !assetReady {
		return fmt.Errorf("assets are not ready for sku %s", product.SKUCode)
	}
	allText := []string{input.Title, input.Description}
	allText = append(allText, input.BulletPoints...)
	allText = append(allText, input.Keywords...)
	if word := containsBaselineSensitiveWord(allText...); word != "" {
		return fmt.Errorf("sensitive word detected: %s", word)
	}
	return nil
}

func (s *Service) prepareListingChargeContext(scope repository.Scope, product *models.EcomProductSKU, item models.EcomListingVersion) (*billinggate.Context, error) {
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("billing platform client is required for listing generation")
	}
	return billinggate.New(s.platform).Begin(billinggate.BeginInput{
		Action:           billinggate.ActionListing,
		SourceType:       billinggate.SourceTypeListingVersion,
		SourceID:         item.ID,
		ProductCode:      billinggate.DefaultProductCode,
		OrganizationID:   scope.OrgID,
		UserID:           scope.UserID,
		BillableItemCode: billinggate.BillableItemListingGenerate,
		ResourceType:     billinggate.DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   billinggate.IdempotencyKeyForAction(billinggate.ActionListing, item.ID),
		RouteSnapshot:    map[string]any{"platform": item.Platform, "site": item.Site, "locale": item.Locale},
		Metadata:         map[string]any{"product_id": product.ID, "sku_code": product.SKUCode, "version_no": item.VersionNo},
	})
}

func (s *Service) prepareExportChargeContext(scope repository.Scope, productID string, exportID string, input CreateExportTaskInput) (*billinggate.Context, error) {
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("billing platform client is required for export generation")
	}
	return billinggate.New(s.platform).Begin(billinggate.BeginInput{
		Action:           billinggate.ActionExport,
		SourceType:       billinggate.SourceTypeExportTask,
		SourceID:         exportID,
		ProductCode:      billinggate.DefaultProductCode,
		OrganizationID:   scope.OrgID,
		UserID:           scope.UserID,
		BillableItemCode: billinggate.BillableItemExportGenerate,
		ResourceType:     billinggate.DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   billinggate.IdempotencyKeyForAction(billinggate.ActionExport, exportID),
		RouteSnapshot:    map[string]any{"platform": input.Platform, "site": input.Site, "locale": input.Locale, "format": input.Format},
		Metadata:         map[string]any{"product_id": productID},
	})
}

func (s *Service) BatchCreateListingVersions(orgID string, userID string, input BatchCreateListingVersionsInput) (*BatchListingMutationResult, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("at least 1 listing item is required")
	}

	result := &BatchListingMutationResult{
		Total:   len(input.Items),
		Preview: input.Preview,
		Items:   make([]BatchListingMutationItem, 0, len(input.Items)),
	}
	seenProductIDs := make(map[string]struct{}, len(input.Items))

	for _, entry := range input.Items {
		entry.ProductID = strings.TrimSpace(entry.ProductID)
		entry.SKUCode = strings.TrimSpace(entry.SKUCode)
		item := BatchListingMutationItem{
			ProductID:    entry.ProductID,
			SKUCode:      entry.SKUCode,
			VersionLabel: strings.TrimSpace(entry.VersionLabel),
			Preview:      input.Preview,
		}

		product, err := s.productForBatchListingEntry(scope, entry)
		if err == nil && product != nil {
			item.ProductID = product.ID
			item.SKUCode = product.SKUCode
			item.ProductTitle = product.Title
		}

		switch {
		case entry.ProductID == "" && entry.SKUCode == "":
			item.Message = "product_id or sku_code is required"
		case entry.ProductID != "" && entry.SKUCode != "" && product != nil && !strings.EqualFold(product.SKUCode, entry.SKUCode):
			item.Message = "sku_code does not match product_id"
		case err != nil:
			item.Message = "product not found"
		default:
			if _, exists := seenProductIDs[product.ID]; exists {
				item.Message = "duplicate product in batch request"
			} else {
				seenProductIDs[product.ID] = struct{}{}
				listing, createErr := s.createListingVersionForProduct(scope, product, CreateListingVersionInput{
					VersionLabel: entry.VersionLabel,
					Title:        entry.Title,
					Description:  entry.Description,
					BulletPoints: entry.BulletPoints,
					Keywords:     entry.Keywords,
					Platform:     entry.Platform,
					Site:         entry.Site,
					Locale:       entry.Locale,
				}, input.Preview)
				if createErr != nil {
					item.Message = createErr.Error()
				} else {
					item.Success = true
					if input.Preview {
						item.Message = "listing version preview valid"
					} else {
						item.Message = "listing version created"
					}
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

func (s *Service) productForBatchListingEntry(scope repository.Scope, entry BatchCreateListingVersionItemInput) (*models.EcomProductSKU, error) {
	if strings.TrimSpace(entry.ProductID) != "" {
		return s.repo.GetProduct(scope, strings.TrimSpace(entry.ProductID))
	}
	return s.repo.GetProductBySKUCode(scope, strings.TrimSpace(entry.SKUCode))
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
	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
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

	inputVersionLabel := target.VersionLabel
	inputTitle := target.Title
	inputDescription := target.Description
	inputBulletPoints := []string(target.BulletPoints)
	inputKeywords := []string(target.Keywords)
	inputPlatform := target.Platform
	inputSite := target.Site
	inputLocale := target.Locale
	if input.VersionLabel != nil {
		inputVersionLabel = strings.TrimSpace(*input.VersionLabel)
	}
	if input.Title != nil {
		inputTitle = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		inputDescription = strings.TrimSpace(*input.Description)
	}
	if input.BulletPoints != nil {
		inputBulletPoints = sanitizeStringList(*input.BulletPoints)
	}
	if input.Keywords != nil {
		inputKeywords = sanitizeStringList(*input.Keywords)
	}
	if input.Platform != nil {
		inputPlatform = strings.TrimSpace(*input.Platform)
	}
	if input.Site != nil {
		inputSite = strings.TrimSpace(*input.Site)
	}
	if input.Locale != nil {
		inputLocale = strings.TrimSpace(*input.Locale)
	}

	created, err := s.createListingVersionForProduct(scope, product, CreateListingVersionInput{
		VersionLabel: inputVersionLabel,
		Title:        inputTitle,
		Description:  inputDescription,
		BulletPoints: inputBulletPoints,
		Keywords:     inputKeywords,
		Platform:     inputPlatform,
		Site:         inputSite,
		Locale:       inputLocale,
	}, false)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingGenerated,
		"Listing Edited", "New listing version "+created.VersionLabel+" created from "+target.VersionLabel, nil)

	return created, nil
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
	var versionStatus string
	for _, v := range versions {
		if v.ID == versionID {
			versionLabel = v.VersionLabel
			versionStatus = v.Status
			break
		}
	}
	if versionStatus == models.ListingVersionStatusAdopted {
		return fmt.Errorf("adopted listing version is immutable")
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
	adopted, adoptedErr := s.repo.GetAdoptedListingVersion(scope, productID, input.Platform, input.Site, input.Locale)
	if adoptedErr != nil || adopted == nil {
		return nil, fmt.Errorf("adopted listing version is required before export")
	}
	listingVersionID = adopted.ID
	listingVersionLabel = adopted.VersionLabel

	exportID := uuid.New().String()
	chargeCtx, err := s.prepareExportChargeContext(scope, productID, exportID, input)
	if err != nil {
		return nil, err
	}

	item := models.EcomExportTask{
		ID:                  exportID,
		ProductID:           productID,
		Status:              models.ExportTaskStatusSucceeded,
		Platform:            input.Platform,
		Site:                input.Site,
		Locale:              input.Locale,
		Format:              input.Format,
		ListingVersionID:    listingVersionID,
		ListingVersionLabel: listingVersionLabel,
		PrimaryAssetRole:    primaryAssetRole,
		AssetCount:          len(manifest),
		AssetManifest:       string(manifestJSON),
		StorageKey:          fmt.Sprintf("local-dev/ecommerce/exports/%s.%s", exportID, strings.ToLower(input.Format)),
		FileSize:            fmt.Sprintf("%d", len(string(manifestJSON))),
	}

	task, err := s.repo.CreateExportTask(scope, item)
	if err != nil {
		_ = billinggate.New(s.platform).Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "export_create_failed"})
		return nil, err
	}
	gate := billinggate.New(s.platform)
	if err := gate.MarkReserved(chargeCtx, map[string]any{"export_task_id": task.ID, "product_id": productID}); err != nil {
		_ = gate.Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "export_mark_reserved_failed"})
		return nil, err
	}
	if _, err := gate.Commit(billinggate.CommitInput{
		Context:      chargeCtx,
		SourceAction: "export_task_created",
		EventID:      fmt.Sprintf("evt_%s", task.ID),
		Dimensions:   map[string]any{"platform": task.Platform, "site": task.Site, "locale": task.Locale, "format": task.Format},
		Metadata:     map[string]any{"export_task_id": task.ID, "product_id": productID, "listing_version_id": listingVersionID},
	}); err != nil {
		_ = gate.Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "export_commit_failed"})
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeExportCreated,
		"Export Task Created", fmt.Sprintf("Export task created for %s/%s with %d selected assets", input.Platform, input.Format, len(manifest)), nil)

	// 更新商品的 export 状态
	product, _ := s.repo.GetProduct(scope, productID)
	if product != nil {
		product.ExportStatus = models.ExportStatusReady
		s.repo.UpdateProduct(scope, *product)
		s.updateProductMainStatus(scope, productID)
	}

	return task, nil
}

func exportPackageSchema(platformCode, site, format string) string {
	platformCode = strings.ToLower(strings.TrimSpace(platformCode))
	site = strings.ToLower(strings.TrimSpace(site))
	format = strings.ToLower(strings.TrimSpace(format))
	if platformCode == "" {
		platformCode = "marketplace"
	}
	if site == "" {
		site = "site"
	}
	if format == "" {
		format = "csv"
	}
	return fmt.Sprintf("%s/%s/%s/v1", platformCode, site, format)
}

func packageFileName(packageID string) string {
	return fmt.Sprintf("%s_export_package.zip", packageID)
}

func (s *Service) resolveExportPackageProduct(scope repository.Scope, item CreateExportPackageItemInput) (*models.EcomProductSKU, []ExportPackageBlocker) {
	productID := strings.TrimSpace(item.ProductID)
	skuCode := strings.TrimSpace(item.SKUCode)
	if productID == "" && skuCode == "" {
		return nil, []ExportPackageBlocker{{Code: "product_required", Message: "product_id or sku_code is required"}}
	}
	var product *models.EcomProductSKU
	var err error
	if productID != "" {
		product, err = s.repo.GetProduct(scope, productID)
	} else {
		product, err = s.repo.GetProductBySKUCode(scope, skuCode)
	}
	if err != nil || product == nil {
		return nil, []ExportPackageBlocker{{Code: "product_not_found", Message: "product not found in current organization"}}
	}
	return product, nil
}

func (s *Service) precheckExportPackageProduct(scope repository.Scope, product *models.EcomProductSKU, input CreateExportPackageInput) []ExportPackageBlocker {
	blockers := make([]ExportPackageBlocker, 0, 2)
	if _, _, err := s.buildProductAssetManifest(scope, product.ID, nil); err != nil {
		blockers = append(blockers, ExportPackageBlocker{Code: "asset_missing", Message: "at least 1 product asset is required"})
	}
	if adopted, err := s.repo.GetAdoptedListingVersion(scope, product.ID, input.Platform, input.Site, input.Locale); err != nil || adopted == nil {
		blockers = append(blockers, ExportPackageBlocker{Code: "listing_missing", Message: "adopted listing version is required"})
	}
	return blockers
}

func (s *Service) buildExportPackageManifest(scope repository.Scope, pkg models.EcomExportPackage, tasks []models.EcomExportTask, results []ExportPackageItemResult) ExportPackageManifest {
	contentURL := fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", pkg.ID)
	manifestURL := fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content?file=manifest", pkg.ID)
	csvURL := fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content?file=listing_csv", pkg.ID)
	manifest := ExportPackageManifest{
		ManifestVersion: "ecommerce.export.package.v1",
		PackageID:       pkg.ID,
		GroupID:         pkg.ID,
		Marketplace:     pkg.Platform,
		Site:            pkg.Site,
		Locale:          pkg.Locale,
		Schema:          pkg.Schema,
		Format:          pkg.Format,
		Status:          pkg.Status,
		CreatedAt:       pkg.CreatedAt,
		Total:           pkg.TotalCount,
		Succeeded:       pkg.SucceededCount,
		Failed:          pkg.FailedCount,
		Files: []ExportPackageManifestFile{
			{Role: "manifest", FileName: "manifest.json", ContentType: "application/json", ContentURL: manifestURL},
			{Role: "listing_csv", FileName: "listing.csv", ContentType: "text/csv", ContentURL: csvURL},
			{Role: "bundle", FileName: packageFileName(pkg.ID), ContentType: "application/zip", ContentURL: contentURL},
		},
		Products: make([]ExportPackageManifestProduct, 0, len(tasks)),
	}
	for _, task := range tasks {
		item := s.buildDownloadListItem(scope, task)
		manifest.Products = append(manifest.Products, ExportPackageManifestProduct{
			ProductID:            item.ProductID,
			SKUCode:              item.ProductSKU,
			TaskID:               item.TaskID,
			ListingVersionID:     item.ListingVersionID,
			ListingVersionLabel:  item.ListingVersionLabel,
			AssetCount:           item.AssetCount,
			ListingCSVContentURL: fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", item.ID),
		})
	}
	for _, result := range results {
		if !result.Success && len(result.Blockers) > 0 {
			manifest.Blockers = append(manifest.Blockers, ExportPackageManifestBlocker{ProductID: result.ProductID, SKUCode: result.SKUCode, Blockers: result.Blockers})
		}
	}
	return manifest
}

func (s *Service) GetExportPackage(orgID string, packageID string) (*ExportPackageResponse, error) {
	scope := repository.Scope{OrgID: orgID}
	pkg, err := s.repo.GetExportPackage(scope, packageID)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.ListExportTasksByPackage(scope, pkg.ID)
	if err != nil {
		return nil, err
	}
	results := make([]ExportPackageItemResult, 0, len(tasks))
	for _, task := range tasks {
		item := s.buildDownloadListItem(scope, task)
		results = append(results, ExportPackageItemResult{
			ProductID:  item.ProductID,
			SKUCode:    item.ProductSKU,
			Success:    task.Status == models.ExportTaskStatusSucceeded,
			TaskID:     task.ID,
			DownloadID: task.ID,
			ContentURL: item.ContentURL,
		})
	}
	manifest := s.buildExportPackageManifest(scope, *pkg, tasks, nil)
	if strings.TrimSpace(pkg.PackageManifest) != "" {
		_ = json.Unmarshal([]byte(pkg.PackageManifest), &manifest)
	}
	return &ExportPackageResponse{
		PackageID:   pkg.ID,
		GroupID:     pkg.ID,
		Status:      pkg.Status,
		Total:       pkg.TotalCount,
		Succeeded:   pkg.SucceededCount,
		Failed:      pkg.FailedCount,
		ContentURL:  fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", pkg.ID),
		ManifestURL: fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content?file=manifest", pkg.ID),
		Package: DownloadPackage{
			FileName:          packageFileName(pkg.ID),
			FileSize:          pkg.FileSize,
			ContentType:       "application/zip",
			ManifestAvailable: strings.TrimSpace(pkg.PackageManifest) != "",
		},
		Manifest: manifest,
		Items:    results,
	}, nil
}

func (s *Service) CreateExportPackage(orgID string, userID string, input CreateExportPackageInput) (*ExportPackageResponse, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("at least one export package item is required")
	}
	packageID := uuid.New().String()
	pkg := models.EcomExportPackage{
		ID:         packageID,
		Status:     models.ExportPackageStatusFailed,
		Platform:   input.Platform,
		Site:       input.Site,
		Locale:     input.Locale,
		Format:     input.Format,
		Schema:     exportPackageSchema(input.Platform, input.Site, input.Format),
		TotalCount: len(input.Items),
	}
	createdPkg, err := s.repo.CreateExportPackage(scope, pkg)
	if err != nil {
		return nil, err
	}
	pkg = *createdPkg

	results := make([]ExportPackageItemResult, 0, len(input.Items))
	createdTasks := make([]models.EcomExportTask, 0, len(input.Items))
	seen := map[string]struct{}{}
	for _, requestItem := range input.Items {
		product, blockers := s.resolveExportPackageProduct(scope, requestItem)
		result := ExportPackageItemResult{ProductID: strings.TrimSpace(requestItem.ProductID), SKUCode: strings.TrimSpace(requestItem.SKUCode)}
		if product != nil {
			result.ProductID = product.ID
			result.SKUCode = product.SKUCode
		}
		if product != nil {
			if _, ok := seen[product.ID]; ok {
				blockers = append(blockers, ExportPackageBlocker{Code: "duplicate_product", Message: "product appears more than once in package request"})
			} else {
				seen[product.ID] = struct{}{}
			}
		}
		if product != nil && len(blockers) == 0 {
			blockers = s.precheckExportPackageProduct(scope, product, input)
		}
		if len(blockers) > 0 {
			result.Blockers = blockers
			results = append(results, result)
			continue
		}

		task, createErr := s.CreateExportTask(orgID, userID, product.ID, CreateExportTaskInput{Platform: input.Platform, Site: input.Site, Locale: input.Locale, Format: input.Format})
		if createErr != nil {
			result.Blockers = []ExportPackageBlocker{{Code: "export_create_failed", Message: createErr.Error()}}
			results = append(results, result)
			continue
		}
		task.PackageID = packageID
		if updated, updateErr := s.repo.UpdateExportTask(scope, *task); updateErr == nil && updated != nil {
			task = updated
		}
		result.Success = true
		result.TaskID = task.ID
		result.DownloadID = task.ID
		result.ContentURL = fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", task.ID)
		results = append(results, result)
		createdTasks = append(createdTasks, *task)
	}

	pkg.SucceededCount = len(createdTasks)
	pkg.FailedCount = len(input.Items) - len(createdTasks)
	switch {
	case pkg.SucceededCount == 0:
		pkg.Status = models.ExportPackageStatusFailed
	case pkg.FailedCount > 0:
		pkg.Status = models.ExportPackageStatusPartialSucceeded
	default:
		pkg.Status = models.ExportPackageStatusSucceeded
	}
	manifest := s.buildExportPackageManifest(scope, pkg, createdTasks, results)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	pkg.PackageManifest = string(manifestBytes)
	pkg.FileSize = fmt.Sprintf("%d", len(manifestBytes))
	if updatedPkg, updateErr := s.repo.UpdateExportPackage(scope, pkg); updateErr == nil && updatedPkg != nil {
		pkg = *updatedPkg
	}

	return &ExportPackageResponse{
		PackageID:   pkg.ID,
		GroupID:     pkg.ID,
		Status:      pkg.Status,
		Total:       pkg.TotalCount,
		Succeeded:   pkg.SucceededCount,
		Failed:      pkg.FailedCount,
		ContentURL:  fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", pkg.ID),
		ManifestURL: fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content?file=manifest", pkg.ID),
		Package: DownloadPackage{
			FileName:          packageFileName(pkg.ID),
			FileSize:          pkg.FileSize,
			ContentType:       "application/zip",
			ManifestAvailable: true,
		},
		Manifest: manifest,
		Items:    results,
	}, nil
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
	packages, err := s.repo.ListExportPackages(scope)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.ListAllExportTasks(scope)
	if err != nil {
		return nil, err
	}

	items := make([]DownloadListItem, 0, len(packages)+len(tasks))
	for _, pkg := range packages {
		items = append(items, s.buildExportPackageListItem(pkg))
	}
	for _, task := range tasks {
		items = append(items, s.buildDownloadListItem(scope, task))
	}
	return items, nil
}

func (s *Service) buildExportPackageListItem(pkg models.EcomExportPackage) DownloadListItem {
	contentURL := fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content", pkg.ID)
	manifestURL := fmt.Sprintf("/api/v1/ecommerce/downloads/%s/content?file=manifest", pkg.ID)
	item := DownloadListItem{
		ID:               pkg.ID,
		PackageID:        pkg.ID,
		SourceType:       "export_package",
		SKUCount:         pkg.TotalCount,
		SucceededCount:   pkg.SucceededCount,
		FailedCount:      pkg.FailedCount,
		Platform:         pkg.Platform,
		Site:             pkg.Site,
		Locale:           pkg.Locale,
		Schema:           pkg.Schema,
		Format:           pkg.Format,
		Status:           pkg.Status,
		FileSize:         pkg.FileSize,
		ContentURL:       contentURL,
		ManifestURL:      manifestURL,
		DownloadFileName: packageFileName(pkg.ID),
		Downloadable:     pkg.SucceededCount > 0 && strings.TrimSpace(pkg.PackageManifest) != "",
		CreatedAt:        pkg.CreatedAt,
	}
	item.Package = DownloadPackage{FileName: item.DownloadFileName, FileSize: pkg.FileSize, ContentType: "application/zip", ManifestAvailable: strings.TrimSpace(pkg.PackageManifest) != ""}
	return item
}

// GetDownloadContent 获取导出包下载内容
func (s *Service) GetDownloadContent(orgID string, downloadID string, fileRole ...string) (*DownloadListItem, io.ReadCloser, http.Header, error) {
	scope := repository.Scope{OrgID: orgID}
	role := ""
	if len(fileRole) > 0 {
		role = strings.TrimSpace(fileRole[0])
	}
	if pkg, err := s.repo.GetExportPackage(scope, downloadID); err == nil && pkg != nil {
		return s.getExportPackageContent(scope, *pkg, role)
	}
	task, err := s.repo.GetExportTask(scope, downloadID)
	if err != nil {
		return nil, nil, nil, err
	}

	item := s.buildDownloadListItem(scope, *task)
	if !item.Downloadable {
		return &item, nil, nil, fmt.Errorf("download is not ready")
	}

	if strings.TrimSpace(task.StorageKey) != "" && strings.HasPrefix(task.StorageKey, "local-dev/") {
		content, err := buildExportCSVContent(item)
		if err != nil {
			return &item, nil, nil, err
		}
		headers := http.Header{}
		headers.Set("Content-Type", "text/csv; charset=utf-8")
		headers.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.DownloadFileName))
		return &item, io.NopCloser(bytes.NewReader(content)), headers, nil
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

func (s *Service) getExportPackageContent(scope repository.Scope, pkg models.EcomExportPackage, fileRole string) (*DownloadListItem, io.ReadCloser, http.Header, error) {
	item := s.buildExportPackageListItem(pkg)
	if !item.Downloadable {
		return &item, nil, nil, fmt.Errorf("download package is not ready")
	}
	tasks, err := s.repo.ListExportTasksByPackage(scope, pkg.ID)
	if err != nil {
		return &item, nil, nil, err
	}
	manifestJSON := strings.TrimSpace(pkg.PackageManifest)
	if manifestJSON == "" {
		manifest := s.buildExportPackageManifest(scope, pkg, tasks, nil)
		manifestBytes, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			return &item, nil, nil, marshalErr
		}
		manifestJSON = string(manifestBytes)
	}

	switch strings.ToLower(strings.TrimSpace(fileRole)) {
	case "manifest", "manifest.json":
		headers := http.Header{}
		headers.Set("Content-Type", "application/json; charset=utf-8")
		headers.Set("Content-Disposition", `attachment; filename="manifest.json"`)
		return &item, io.NopCloser(strings.NewReader(manifestJSON)), headers, nil
	case "listing_csv", "csv", "listing.csv":
		csvContent, csvErr := s.buildExportPackageCSVContent(scope, tasks)
		if csvErr != nil {
			return &item, nil, nil, csvErr
		}
		headers := http.Header{}
		headers.Set("Content-Type", "text/csv; charset=utf-8")
		headers.Set("Content-Disposition", `attachment; filename="listing.csv"`)
		return &item, io.NopCloser(bytes.NewReader(csvContent)), headers, nil
	default:
		csvContent, csvErr := s.buildExportPackageCSVContent(scope, tasks)
		if csvErr != nil {
			return &item, nil, nil, csvErr
		}
		var buf bytes.Buffer
		zipWriter := zip.NewWriter(&buf)
		manifestFile, err := zipWriter.Create("manifest.json")
		if err != nil {
			return &item, nil, nil, err
		}
		if _, err := manifestFile.Write([]byte(manifestJSON)); err != nil {
			return &item, nil, nil, err
		}
		csvFile, err := zipWriter.Create("listing.csv")
		if err != nil {
			return &item, nil, nil, err
		}
		if _, err := csvFile.Write(csvContent); err != nil {
			return &item, nil, nil, err
		}
		if err := zipWriter.Close(); err != nil {
			return &item, nil, nil, err
		}
		headers := http.Header{}
		headers.Set("Content-Type", "application/zip")
		headers.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.DownloadFileName))
		return &item, io.NopCloser(bytes.NewReader(buf.Bytes())), headers, nil
	}
}

func (s *Service) buildExportPackageCSVContent(scope repository.Scope, tasks []models.EcomExportTask) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"marketplace", "site", "locale", "schema_version", "sku", "title", "listing_version_id", "listing_version_label", "asset_count", "primary_asset_role", "download_task_id", "package_id"}); err != nil {
		return nil, err
	}
	for _, task := range tasks {
		item := s.buildDownloadListItem(scope, task)
		if err := writer.Write([]string{item.Platform, item.Site, item.Locale, exportPackageSchema(item.Platform, item.Site, item.Format), item.ProductSKU, item.ProductTitle, item.ListingVersionID, item.ListingVersionLabel, fmt.Sprintf("%d", item.AssetCount), item.PrimaryAssetRole, item.ID, task.PackageID}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildExportCSVContent(item DownloadListItem) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"marketplace", "site", "locale", "schema_version", "product_sku", "product_title", "listing_version_id", "listing_version_label", "asset_count", "primary_asset_role", "download_task_id"}); err != nil {
		return nil, err
	}
	if err := writer.Write([]string{item.Platform, item.Site, item.Locale, exportPackageSchema(item.Platform, item.Site, item.Format), item.ProductSKU, item.ProductTitle, item.ListingVersionID, item.ListingVersionLabel, fmt.Sprintf("%d", item.AssetCount), item.PrimaryAssetRole, item.ID}); err != nil {
		return nil, err
	}
	for _, asset := range item.Assets {
		if err := writer.Write([]string{item.Platform, item.Site, item.Locale, exportPackageSchema(item.Platform, item.Site, item.Format), "asset", asset.FileName, asset.AssetRole, asset.AssetID, asset.RelationID, asset.ContentURL, item.ID}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
	SKUCode      string   `json:"sku_code"`
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
	Preview bool                                 `json:"preview"`
	Items   []BatchCreateListingVersionItemInput `json:"items" binding:"required"`
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
	Preview      bool                       `json:"preview,omitempty"`
	Message      string                     `json:"message,omitempty"`
	Listing      *models.EcomListingVersion `json:"listing,omitempty"`
}

type BatchListingMutationResult struct {
	Total     int                        `json:"total"`
	Succeeded int                        `json:"succeeded"`
	Failed    int                        `json:"failed"`
	Preview   bool                       `json:"preview,omitempty"`
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

type CreateExportPackageInput struct {
	Items    []CreateExportPackageItemInput `json:"items" binding:"required"`
	Platform string                         `json:"platform" binding:"required"`
	Site     string                         `json:"site" binding:"required"`
	Locale   string                         `json:"locale" binding:"required"`
	Format   string                         `json:"format" binding:"required"`
	Mode     string                         `json:"mode,omitempty"`
}

type CreateExportPackageItemInput struct {
	ProductID string `json:"product_id,omitempty"`
	SKUCode   string `json:"sku_code,omitempty"`
}

type ExportPackageBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ExportPackageItemResult struct {
	ProductID  string                 `json:"product_id,omitempty"`
	SKUCode    string                 `json:"sku_code,omitempty"`
	Success    bool                   `json:"success"`
	TaskID     string                 `json:"task_id,omitempty"`
	DownloadID string                 `json:"download_id,omitempty"`
	ContentURL string                 `json:"content_url,omitempty"`
	Blockers   []ExportPackageBlocker `json:"blockers,omitempty"`
}

type ExportPackageResponse struct {
	PackageID   string                    `json:"package_id"`
	GroupID     string                    `json:"group_id"`
	Status      string                    `json:"status"`
	Total       int                       `json:"total"`
	Succeeded   int                       `json:"succeeded"`
	Failed      int                       `json:"failed"`
	ContentURL  string                    `json:"content_url"`
	ManifestURL string                    `json:"manifest_url"`
	Package     DownloadPackage           `json:"package"`
	Manifest    ExportPackageManifest     `json:"manifest"`
	Items       []ExportPackageItemResult `json:"items"`
}

type ExportPackageManifest struct {
	ManifestVersion string                         `json:"manifest_version"`
	PackageID       string                         `json:"package_id"`
	GroupID         string                         `json:"group_id"`
	Marketplace     string                         `json:"marketplace"`
	Site            string                         `json:"site"`
	Locale          string                         `json:"locale"`
	Schema          string                         `json:"schema"`
	Format          string                         `json:"format"`
	Status          string                         `json:"status"`
	CreatedAt       time.Time                      `json:"created_at"`
	Total           int                            `json:"total"`
	Succeeded       int                            `json:"succeeded"`
	Failed          int                            `json:"failed"`
	Files           []ExportPackageManifestFile    `json:"files"`
	Products        []ExportPackageManifestProduct `json:"products"`
	Blockers        []ExportPackageManifestBlocker `json:"blockers,omitempty"`
}

type ExportPackageManifestFile struct {
	Role        string `json:"role"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size,omitempty"`
	ContentURL  string `json:"content_url"`
}

type ExportPackageManifestProduct struct {
	ProductID            string `json:"product_id"`
	SKUCode              string `json:"sku_code"`
	TaskID               string `json:"task_id"`
	ListingVersionID     string `json:"listing_version_id,omitempty"`
	ListingVersionLabel  string `json:"listing_version_label,omitempty"`
	AssetCount           int    `json:"asset_count"`
	ListingCSVContentURL string `json:"listing_csv_content_url"`
}

type ExportPackageManifestBlocker struct {
	ProductID string                 `json:"product_id,omitempty"`
	SKUCode   string                 `json:"sku_code,omitempty"`
	Blockers  []ExportPackageBlocker `json:"blockers"`
}
