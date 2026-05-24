package productcore

import (
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"encoding/json"
	"fmt"
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

	if product.Status == models.ProductStatusPublished || product.Status == models.ProductStatusArchived {
		return
	}

	var newStatus string

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

	if newStatus != product.Status {
		oldStatus := product.Status
		product.Status = newStatus
		s.repo.UpdateProduct(scope, *product)

		s.logActivity(scope, productID, models.ProductActivityTypeStatusChanged,
			"Auto Status Updated", "Product status auto changed from "+oldStatus+" to "+newStatus, nil)
	}
}

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

		assets, _ := s.repo.ListProductAssets(scope, p.ID)
		item.AssetsCount = len(assets)

		for _, a := range assets {
			if a.IsPrimary {
				item.HasPrimaryAsset = true
				break
			}
		}

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
