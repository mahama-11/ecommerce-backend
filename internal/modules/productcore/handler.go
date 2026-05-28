package productcore

import (
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/observability"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/logger"
	"ecommerce-service/pkg/response"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Handler struct {
	service *Service
}

type exportTaskPublicResponse struct {
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
	AssetManifest       string    `json:"asset_manifest,omitempty"`
	FileSize            string    `json:"file_size,omitempty"`
	ContentURL          string    `json:"content_url"`
	CreatedAt           time.Time `json:"created_at"`
}

func exportTaskPublic(task models.EcomExportTask) exportTaskPublicResponse {
	return exportTaskPublicResponse{
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
		ContentURL:          "/api/v1/ecommerce/downloads/" + task.ID + "/content",
		CreatedAt:           task.CreatedAt,
	}
}

func exportTasksPublic(tasks []models.EcomExportTask) []exportTaskPublicResponse {
	out := make([]exportTaskPublicResponse, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, exportTaskPublic(task))
	}
	return out
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func scopeFromGin(c *gin.Context) (orgID string, userID string) {
	return c.GetString("orgID"), c.GetString("userID")
}

// ==================== Product 基础操作 ====================

func (h *Handler) ListProducts(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.product_center.products.list")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	log := logger.With(
		"request_id", c.GetString("requestID"),
		"trace_id", c.GetString("traceID"),
		"module", "product_center",
		"operation", "products.list",
		"org_id", orgID,
	)
	span.SetAttributes(
		attribute.String("module", "product_center"),
		attribute.String("operation", "products.list"),
		attribute.String("ecommerce.org_id", orgID),
	)
	log.Info("ecommerce.product_center.products.list.started", "status", "started")
	items, err := h.service.ListProducts(orgID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list products failed")
		log.Error("ecommerce.product_center.products.list.failed", "status", "failed", "error_code", "product_list_failed", "error", err.Error())
		moduleutil.WritePlatformError(c, err, "list products failed")
		return
	}
	span.SetAttributes(attribute.Int("ecommerce.product_count", len(items)))
	log.Info("ecommerce.product_center.products.list.finished", "status", "finished", "product_count", len(items))
	response.JSONSuccess(c, items)
}

func (h *Handler) GetProduct(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.product_center.product.detail")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	observability.Event("ecommerce.product_center.product.detail.started", "product_center", "product.detail", observability.Fields{"product_id": c.Param("product_id"), "org_id": orgID})
	detail, err := h.service.GetProductDetail(orgID, c.Param("product_id"))
	if err != nil {
		observability.ErrorEvent("ecommerce.product_center.product.detail.failed", "product_center", "product.detail", err, "product_detail_failed", observability.Fields{"product_id": c.Param("product_id"), "org_id": orgID})
		span.RecordError(err)
		moduleutil.WritePlatformError(c, err, "get product failed")
		return
	}
	observability.Event("ecommerce.product_center.product.detail.finished", "product_center", "product.detail", observability.Fields{"product_id": c.Param("product_id"), "org_id": orgID})
	response.JSONSuccess(c, detail)
}

func (h *Handler) CreateProduct(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.product_center.product.create")
	defer span.End()

	var input CreateProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid create product request")
		return
	}

	orgID, userID := scopeFromGin(c)
	observability.Event("ecommerce.product_center.product.create.started", "product_center", "product.create", observability.Fields{"sku_code": input.SKUCode, "org_id": orgID})
	item, err := h.service.CreateProduct(orgID, userID, input)
	if err != nil {
		observability.ErrorEvent("ecommerce.product_center.product.create.failed", "product_center", "product.create", err, "product_create_failed", observability.Fields{"sku_code": input.SKUCode, "org_id": orgID})
		span.RecordError(err)
		moduleutil.WritePlatformError(c, err, "create product failed")
		return
	}
	observability.Event("ecommerce.product_center.product.create.finished", "product_center", "product.create", observability.Fields{"product_id": item.ID, "sku_code": item.SKUCode, "org_id": orgID})
	response.JSONSuccess(c, item)
}

func (h *Handler) UpdateProduct(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.product_center.product.update")
	defer span.End()

	var input UpdateProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid update product request")
		return
	}

	orgID, userID := scopeFromGin(c)
	observability.Event("ecommerce.product_center.product.update.started", "product_center", "product.update", observability.Fields{"product_id": c.Param("product_id"), "org_id": orgID})
	item, err := h.service.UpdateProduct(orgID, userID, c.Param("product_id"), input)
	if err != nil {
		observability.ErrorEvent("ecommerce.product_center.product.update.failed", "product_center", "product.update", err, "product_update_failed", observability.Fields{"product_id": c.Param("product_id"), "org_id": orgID})
		span.RecordError(err)
		moduleutil.WritePlatformError(c, err, "update product failed")
		return
	}
	observability.Event("ecommerce.product_center.product.update.finished", "product_center", "product.update", observability.Fields{"product_id": item.ID, "sku_code": item.SKUCode, "org_id": orgID})
	response.JSONSuccess(c, item)
}

func (h *Handler) UpdateProductStatus(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.update_product_status")
	defer span.End()

	var input struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid status update request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.UpdateProductStatus(orgID, userID, c.Param("product_id"), input.Status)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "update product status failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) DeleteProduct(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.delete_product")
	defer span.End()

	orgID, userID := scopeFromGin(c)
	err := h.service.DeleteProduct(orgID, userID, c.Param("product_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "delete product failed")
		return
	}
	response.JSONSuccess(c, gin.H{"success": true})
}

// ==================== Asset 操作 ====================

func (h *Handler) ListProductAssets(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.list_product_assets")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	items, err := h.service.ListProductAssets(orgID, c.Param("product_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list product assets failed")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) AddProductAsset(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.add_product_asset")
	defer span.End()

	var input AddProductAssetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid add product asset request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.AddProductAsset(orgID, userID, c.Param("product_id"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "add product asset failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) DeleteProductAsset(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.delete_product_asset")
	defer span.End()

	orgID, userID := scopeFromGin(c)
	err := h.service.DeleteProductAsset(orgID, userID, c.Param("product_id"), c.Param("asset_relation_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "delete product asset failed")
		return
	}
	response.JSONSuccess(c, gin.H{"success": true})
}

func (h *Handler) UpdateProductAsset(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.update_product_asset")
	defer span.End()

	var input UpdateProductAssetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid update product asset request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.UpdateProductAsset(orgID, userID, c.Param("product_id"), c.Param("asset_relation_id"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "update product asset failed")
		return
	}
	response.JSONSuccess(c, item)
}

// ==================== Listing Version 操作 ====================

func (h *Handler) ListListingVersions(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.list_listing_versions")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	items, err := h.service.ListListingVersions(orgID, c.Param("product_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list listing versions failed")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) CreateListingVersion(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.create_listing_version")
	defer span.End()

	var input CreateListingVersionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid create listing version request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.CreateListingVersion(orgID, userID, c.Param("product_id"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "create listing version failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) BatchCreateListingVersions(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.batch_create_listing_versions")
	defer span.End()

	var input BatchCreateListingVersionsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid batch create listing request")
		return
	}

	orgID, userID := scopeFromGin(c)
	result, err := h.service.BatchCreateListingVersions(orgID, userID, input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "batch create listing versions failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) AdoptListingVersion(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.adopt_listing_version")
	defer span.End()

	var input struct {
		VersionID string `json:"version_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid adopt listing request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.AdoptListingVersion(orgID, userID, c.Param("product_id"), input.VersionID)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "adopt listing version failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) BatchAdoptListingVersions(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.batch_adopt_listing_versions")
	defer span.End()

	var input BatchAdoptListingVersionsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid batch adopt listing request")
		return
	}

	orgID, userID := scopeFromGin(c)
	result, err := h.service.BatchAdoptListingVersions(orgID, userID, input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "batch adopt listing versions failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) UpdateListingVersion(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.update_listing_version")
	defer span.End()

	var input UpdateListingVersionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid update listing version request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.UpdateListingVersion(orgID, userID, c.Param("product_id"), c.Param("version_id"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "update listing version failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) DeleteListingVersion(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.delete_listing_version")
	defer span.End()

	orgID, userID := scopeFromGin(c)
	err := h.service.DeleteListingVersion(orgID, userID, c.Param("product_id"), c.Param("version_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "delete listing version failed")
		return
	}
	response.JSONSuccess(c, gin.H{"success": true})
}

// ==================== Profit Snapshot 操作 ====================

func (h *Handler) ListProfitSnapshots(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.list_profit_snapshots")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	items, err := h.service.ListProfitSnapshots(orgID, c.Param("product_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list profit snapshots failed")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) CalculateProfit(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.calculate_profit")
	defer span.End()

	var input CalculateProfitInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid calculate profit request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.CalculateProfit(orgID, userID, c.Param("product_id"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "calculate profit failed")
		return
	}
	response.JSONSuccess(c, item)
}

// ==================== Export Task 操作 ====================

func (h *Handler) ListExportTasks(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.list_export_tasks")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	items, err := h.service.ListExportTasks(orgID, c.Param("product_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list export tasks failed")
		return
	}
	response.JSONSuccess(c, exportTasksPublic(items))
}

func (h *Handler) CreateExportTask(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.create_export_task")
	defer span.End()

	var input CreateExportTaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid create export task request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.CreateExportTask(orgID, userID, c.Param("product_id"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "create export task failed")
		return
	}
	response.JSONSuccess(c, exportTaskPublic(*item))
}

func (h *Handler) CreateExportPackage(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.create_export_package")
	defer span.End()

	var input CreateExportPackageInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid create export package request")
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.CreateExportPackage(orgID, userID, input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "create export package failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) GetExportPackage(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.get_export_package")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	item, err := h.service.GetExportPackage(orgID, c.Param("package_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "get export package failed")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) RetryExportPackage(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.retry_export_package")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	item, err := h.service.GetExportPackage(orgID, c.Param("package_id"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "retry export package failed")
		return
	}
	// Retry foundation: expose deterministic blockers/current child rows without creating duplicate tasks.
	// A future worker can submit only failed items from this snapshot with an explicit idempotency key.
	response.JSONSuccess(c, gin.H{"retry_supported": false, "retry_contract": "resubmit failed items with POST /api/v1/ecommerce/export-packages", "package": item})
}

func (h *Handler) UpdateExportTaskStatus(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.update_export_task_status")
	defer span.End()

	var input struct {
		TaskID     string `json:"task_id" binding:"required"`
		Status     string `json:"status" binding:"required"`
		StorageKey string `json:"storage_key,omitempty"`
		PackageURL string `json:"package_url,omitempty"`
		FileSize   string `json:"file_size,omitempty"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid update export task request")
		return
	}
	// User-facing status updates must not accept arbitrary storage keys or external package URLs.
	// Package/file materialization is generated server-side and delivered through /downloads/:id/content.
	if input.StorageKey != "" || input.PackageURL != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "storage_key and package_url are managed by server-side export packaging"})
		return
	}

	orgID, userID := scopeFromGin(c)
	item, err := h.service.UpdateExportTaskStatus(
		orgID, userID, c.Param("product_id"),
		input.TaskID, input.Status, input.StorageKey, input.PackageURL, input.FileSize)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "update export task failed")
		return
	}
	response.JSONSuccess(c, exportTaskPublic(*item))
}

func (h *Handler) ListDownloads(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.list_downloads")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	items, err := h.service.ListDownloads(orgID)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list downloads failed")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) DownloadContent(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.download_content")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	_, body, headers, err := h.service.GetDownloadContent(orgID, c.Param("download_id"), c.Query("file"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "download content failed")
		return
	}
	defer body.Close()
	for key, values := range headers {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Status(http.StatusOK)
	if _, copyErr := io.Copy(c.Writer, body); copyErr != nil {
		span.RecordError(copyErr)
	}
}
