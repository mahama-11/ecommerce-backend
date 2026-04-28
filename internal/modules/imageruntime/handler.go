package imageruntime

import (
	"io"
	"net/http"
	"strconv"

	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterSourceAsset(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.asset.register")
	defer span.End()
	var req RegisterSourceAssetInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		response.JSONBindError(c, err, "invalid source asset payload")
		return
	}
	item, err := h.service.RegisterSourceAsset(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to register source asset", "ECOMMERCE_SOURCE_ASSET_REGISTER_FAILED", "Check source payload and try again.")
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) CreateImageJob(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.job.create")
	defer span.End()
	var req CreateImageJobInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		response.JSONBindError(c, err, "invalid create image job request")
		return
	}
	item, err := h.service.CreateImageJob(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to create ecommerce image job", "ECOMMERCE_IMAGE_JOB_CREATE_FAILED", "Check source asset and runtime parameters.")
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) GetJob(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.job.get")
	defer span.End()
	item, err := h.service.GetJob(c.GetString("orgID"), c.Param("jobID"))
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeNotFound, "Image job not found", "ECOMMERCE_IMAGE_JOB_NOT_FOUND", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) CancelJob(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.job.cancel")
	defer span.End()
	item, err := h.service.CancelJob(c.GetString("orgID"), c.Param("jobID"))
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to cancel ecommerce image job", "ECOMMERCE_IMAGE_JOB_CANCEL_FAILED", "Check current job state and try again.")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) ListJobs(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.job.list")
	defer span.End()
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "8"))
	items, err := h.service.ListJobs(c.GetString("orgID"), c.GetString("userID"), c.Query("sceneType"), limit)
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to list ecommerce image jobs", "ECOMMERCE_IMAGE_JOB_LIST_FAILED", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) InternalUpdateJobRuntime(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.internal.update")
	defer span.End()
	var req UpdateJobRuntimeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		response.JSONBindError(c, err, "invalid ecommerce runtime update request")
		return
	}
	item, err := h.service.UpdateJobRuntime(c.Param("jobID"), req)
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to update ecommerce image job runtime", "ECOMMERCE_IMAGE_JOB_RUNTIME_UPDATE_FAILED", "Check internal runtime payload and job state.")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) InternalRecordJobResults(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.internal.results")
	defer span.End()
	var req RecordJobResultsInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		response.JSONBindError(c, err, "invalid ecommerce result callback request")
		return
	}
	item, err := h.service.RecordJobResults(c.Param("jobID"), req)
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to record ecommerce image job results", "ECOMMERCE_IMAGE_JOB_RESULT_RECORD_FAILED", "Check internal result payload and asset metadata.")
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) GetAssetContent(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.asset.content")
	defer span.End()
	item, body, headers, err := h.service.GetAssetContent(c.GetString("orgID"), c.Param("assetID"))
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeNotFound, "Asset content not found", "ECOMMERCE_ASSET_CONTENT_NOT_FOUND", "Refresh and try again.")
		return
	}
	defer body.Close()
	if contentType := headers.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	} else if item.MimeType != "" {
		c.Header("Content-Type", item.MimeType)
	}
	c.Status(http.StatusOK)
	if _, copyErr := io.Copy(c.Writer, body); copyErr != nil {
		span.RecordError(copyErr)
	}
}
