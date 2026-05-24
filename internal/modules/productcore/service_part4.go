package productcore

import (
	"archive/zip"
	"bytes"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

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
