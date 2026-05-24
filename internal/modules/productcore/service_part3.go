package productcore

import (
	"bytes"
	"ecommerce-service/internal/billinggate"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// DeleteListingVersion 删除 Listing 版本
func (s *Service) DeleteListingVersion(orgID string, userID string, productID string, versionID string) error {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

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

	if err := s.repo.DeleteListingVersion(scope, productID, versionID); err != nil {
		return err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingDeleted,
		"Listing Deleted", "Listing version "+versionLabel+" deleted", nil)

	s.updateProductListingStatus(scope, productID)

	s.updateProductMainStatus(scope, productID)

	return nil
}

// validateStatusTransitionWithPreconditions 验证状态转换是否合法（包含前置条件检查）
func (s *Service) validateStatusTransitionWithPreconditions(scope repository.Scope, productID string, fromStatus string, toStatus string) error {

	if !isValidStatusTransition(fromStatus, toStatus) {
		return fmt.Errorf("invalid status transition from %s to %s", fromStatus, toStatus)
	}

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

// ListProfitSnapshots 列出商品的利润快照
func (s *Service) ListProfitSnapshots(orgID string, productID string) ([]models.EcomProfitSnapshot, error) {
	return s.repo.ListProfitSnapshots(repository.Scope{OrgID: orgID}, productID)
}

// CalculateProfit 计算利润并创建快照
func (s *Service) CalculateProfit(orgID string, userID string, productID string, input CalculateProfitInput) (*models.EcomProfitSnapshot, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	grossProfit := input.ListingPrice - input.CostPrice
	netProfit := grossProfit - input.LogisticsCost - input.PlatformFee - input.OtherFee
	var grossMargin, netMargin float64
	if input.ListingPrice > 0 {
		grossMargin = grossProfit / input.ListingPrice
		netMargin = netProfit / input.ListingPrice
	}

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
