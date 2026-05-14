package imageruntime

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"ecommerce-service/internal/modules/moduleutil"
	visualworkflowmodule "ecommerce-service/internal/modules/visualworkflow"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	service        *Service
	visualWorkflow *visualworkflowmodule.Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) WithVisualWorkflowService(service *visualworkflowmodule.Service) *Handler {
	h.visualWorkflow = service
	return h
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
		moduleutil.WritePlatformError(c, err, "Failed to create ecommerce image job")
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
	items, err := h.service.ListJobs(c.GetString("orgID"), c.GetString("userID"), c.Query("sceneType"), c.Query("productID"), limit)
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
	if h.shouldRouteVisualDeconstructionCallback(c) {
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual workflow runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateDeconstructionRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to update visual deconstruction runtime", "ECOMMERCE_VISUAL_DECONSTRUCTION_RUNTIME_UPDATE_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualIntentPlannerCallback(c) {
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual intent planner runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateIntentPlannerRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to update visual intent planner runtime", "ECOMMERCE_VISUAL_INTENT_PLANNER_RUNTIME_UPDATE_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualPromptPlannerCallback(c) {
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual prompt planner runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdatePromptPlannerRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to update visual prompt planner runtime", "ECOMMERCE_VISUAL_PROMPT_PLANNER_RUNTIME_UPDATE_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualPromptPlannerCallback(c) {
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual prompt planner result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordPromptPlannerResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to record visual prompt planner results", "ECOMMERCE_VISUAL_PROMPT_PLANNER_RESULT_RECORD_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualStrategyReportCallback(c) {
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual strategy report runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateStrategyReportRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to update visual strategy report runtime", "ECOMMERCE_VISUAL_STRATEGY_REPORT_RUNTIME_UPDATE_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualStrategyReportCallback(c) {
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual strategy report result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordStrategyReportResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to record visual strategy report results", "ECOMMERCE_VISUAL_STRATEGY_REPORT_RESULT_RECORD_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualGenerationCallback(c) {
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual generation runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateGenerationRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to update visual generation runtime", "ECOMMERCE_VISUAL_GENERATION_RUNTIME_UPDATE_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
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
	if h.shouldRouteVisualDeconstructionCallback(c) {
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual workflow result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordDeconstructionResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to record visual deconstruction results", "ECOMMERCE_VISUAL_DECONSTRUCTION_RESULT_RECORD_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualIntentPlannerCallback(c) {
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual intent planner result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordIntentPlannerResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to record visual intent planner results", "ECOMMERCE_VISUAL_INTENT_PLANNER_RESULT_RECORD_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualGenerationCallback(c) {
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			response.JSONBindError(c, err, "invalid ecommerce visual generation result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordGenerationResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			h.writeVisualCallbackError(c, err, "Failed to record visual generation results", "ECOMMERCE_VISUAL_GENERATION_RESULT_RECORD_FAILED")
			return
		}
		response.JSONSuccess(c, item)
		return
	}
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

func (h *Handler) shouldRouteVisualDeconstructionCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if c.Query("source_type") == "visual_deconstruction" {
		return true
	}
	return h.visualWorkflow.HasDeconstructionJob(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualIntentPlannerCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if c.Query("source_type") == "visual_intent_planning" {
		return true
	}
	return h.visualWorkflow.HasIntentPlannerSession(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualPromptPlannerCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if c.Query("source_type") == "visual_prompt_planning" {
		return true
	}
	return h.visualWorkflow.HasPromptPlannerSession(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualStrategyReportCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if c.Query("source_type") == "visual_strategy_report" {
		return true
	}
	return h.visualWorkflow.HasStrategyReportSession(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualGenerationCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if c.Query("source_type") == "visual_generation" {
		return true
	}
	return h.visualWorkflow.HasGenerationVersion(c.Param("jobID"))
}

func (h *Handler) writeVisualCallbackError(c *gin.Context, err error, message, fallbackCode string) {
	switch {
	case visualworkflowmodule.IsInternalCallbackInvalid(err):
		response.JSONErrorSemantic(c, response.CodeInvalidParameter, message, "ECOMMERCE_VISUAL_DECONSTRUCTION_CALLBACK_INVALID", "Callback payload is a permanent contract error; fix the normalized runtime result payload before retrying.")
	case visualworkflowmodule.IsInternalCallbackNotFound(err) || errors.Is(err, gorm.ErrRecordNotFound):
		response.JSONErrorSemantic(c, response.CodeNotFound, message, "ECOMMERCE_VISUAL_DECONSTRUCTION_JOB_NOT_FOUND", "Verify source_id/runtime job mapping; retrying with the same missing job id will not succeed.")
	default:
		response.JSONErrorSemantic(c, response.CodeInternalError, message, fallbackCode, "Check internal runtime payload and persistence state; retry may succeed after transient service recovery.")
	}
}
