package imageruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"ecommerce-service/internal/modules/moduleutil"
	visualworkflowmodule "ecommerce-service/internal/modules/visualworkflow"
	"ecommerce-service/internal/observability"
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
		moduleutil.WritePlatformError(c, err, "Failed to register source asset")
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
		observability.ErrorEvent("ecommerce.runtime.job.create.failed", "image_runtime", "job.create", err, "image_job_request_invalid", imageRuntimeFields(c, req, ""))
		response.JSONBindError(c, err, "invalid create image job request")
		return
	}
	observability.Event("ecommerce.runtime.job.create.started", "image_runtime", "job.create", imageRuntimeFields(c, req, ""))
	item, err := h.service.CreateImageJob(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.runtime.job.create.failed", "image_runtime", "job.create", err, "image_job_create_failed", imageRuntimeFields(c, req, ""))
		moduleutil.WritePlatformError(c, err, "Failed to create ecommerce image job")
		return
	}
	observability.Event("ecommerce.runtime.job.create.finished", "image_runtime", "job.create", imageRuntimeFields(c, req, item.JobID))
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
	callbackFields := imageCallbackFields(c, "runtime_update")
	observability.Event("ecommerce.runtime.callback.update.started", "image_runtime", "callback.update", callbackFields)
	if h.shouldRouteVisualDeconstructionCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_deconstruction", "runtime_update")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_deconstruction_update_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_deconstruction_update_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual workflow runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateDeconstructionRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_deconstruction_update_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_deconstruction_update_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to update visual deconstruction runtime", "ECOMMERCE_VISUAL_DECONSTRUCTION_RUNTIME_UPDATE_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.update.finished", "image_runtime", "callback.update", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualIntentPlannerCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_intent_planning", "runtime_update")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_intent_planner_update_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_intent_planner_update_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual intent planner runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateIntentPlannerRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_intent_planner_update_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_intent_planner_update_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to update visual intent planner runtime", "ECOMMERCE_VISUAL_INTENT_PLANNER_RUNTIME_UPDATE_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.update.finished", "image_runtime", "callback.update", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualPromptPlannerCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_prompt_planning", "runtime_update")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_prompt_planner_update_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_prompt_planner_update_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual prompt planner runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdatePromptPlannerRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_prompt_planner_update_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_prompt_planner_update_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to update visual prompt planner runtime", "ECOMMERCE_VISUAL_PROMPT_PLANNER_RUNTIME_UPDATE_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.update.finished", "image_runtime", "callback.update", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualStrategyReportCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_strategy_report", "runtime_update")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_strategy_report_update_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_strategy_report_update_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual strategy report runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateStrategyReportRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_strategy_report_update_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_strategy_report_update_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to update visual strategy report runtime", "ECOMMERCE_VISUAL_STRATEGY_REPORT_RUNTIME_UPDATE_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.update.finished", "image_runtime", "callback.update", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualGenerationCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_generation", "runtime_update")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRuntimeUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_generation_update_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_generation_update_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual generation runtime update request")
			return
		}
		item, err := h.visualWorkflow.InternalUpdateGenerationRuntime(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_generation_update_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "visual_generation_update_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to update visual generation runtime", "ECOMMERCE_VISUAL_GENERATION_RUNTIME_UPDATE_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": item.Status, "stage": item.Stage, "generation_version_id": item.VersionID, "runtime_job_id": item.RuntimeJobID}))
		observability.Event("ecommerce.runtime.callback.update.finished", "image_runtime", "callback.update", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	var req UpdateJobRuntimeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "image_job_update_bind_failed", callbackFields)
		response.JSONBindError(c, err, "invalid ecommerce runtime update request")
		return
	}
	item, err := h.service.UpdateJobRuntime(c.Param("jobID"), req)
	if err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.runtime.callback.update.failed", "image_runtime", "callback.update", err, "image_job_update_failed", callbackFields)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to update ecommerce image job runtime", "ECOMMERCE_IMAGE_JOB_RUNTIME_UPDATE_FAILED", "Check internal runtime payload and job state.")
		return
	}
	observability.Event("ecommerce.runtime.callback.update.finished", "image_runtime", "callback.update", callbackFields)
	response.JSONSuccess(c, item)
}

func (h *Handler) InternalRecordJobResults(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/image-runtime-handler", "ecommerce.image_runtime.internal.results")
	defer span.End()
	callbackFields := imageCallbackFields(c, "result")
	observability.Event("ecommerce.runtime.callback.results.started", "image_runtime", "callback.results", callbackFields)
	if h.shouldRouteVisualDeconstructionCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_deconstruction", "result")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_deconstruction_results_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_deconstruction_results_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual workflow result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordDeconstructionResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_deconstruction_results_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_deconstruction_results_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to record visual deconstruction results", "ECOMMERCE_VISUAL_DECONSTRUCTION_RESULT_RECORD_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.results.finished", "image_runtime", "callback.results", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualIntentPlannerCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_intent_planning", "result")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_intent_planner_results_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_intent_planner_results_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual intent planner result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordIntentPlannerResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_intent_planner_results_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_intent_planner_results_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to record visual intent planner results", "ECOMMERCE_VISUAL_INTENT_PLANNER_RESULT_RECORD_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.results.finished", "image_runtime", "callback.results", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualPromptPlannerCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_prompt_planning", "result")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_prompt_planner_results_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_prompt_planner_results_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual prompt planner result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordPromptPlannerResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_prompt_planner_results_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_prompt_planner_results_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to record visual prompt planner results", "ECOMMERCE_VISUAL_PROMPT_PLANNER_RESULT_RECORD_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.results.finished", "image_runtime", "callback.results", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualStrategyReportCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_strategy_report", "result")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(errors.New("visual strategy report callback bind failed"))
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_strategy_report_results_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_strategy_report_results_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual strategy report result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordStrategyReportResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(errors.New("visual strategy report callback processing failed"))
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_strategy_report_results_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_strategy_report_results_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to record visual strategy report results", "ECOMMERCE_VISUAL_STRATEGY_REPORT_RESULT_RECORD_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": "processed", "stage": "callback_received"}))
		observability.Event("ecommerce.runtime.callback.results.finished", "image_runtime", "callback.results", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	if h.shouldRouteVisualGenerationCallback(c) {
		visualFields := visualWorkflowCallbackFields(c, "visual_generation", "result")
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.started", "visual_workflow", "runtime.callback.received", visualFields)
		var req visualworkflowmodule.InternalRecordResultsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_generation_results_bind_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": "payload_invalid"}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_generation_results_bind_failed", callbackFields)
			response.JSONBindError(c, err, "invalid ecommerce visual generation result callback request")
			return
		}
		item, err := h.visualWorkflow.InternalRecordGenerationResults(c.Param("jobID"), req)
		if err != nil {
			span.RecordError(err)
			observability.ErrorEvent("ecommerce.visual_workflow.runtime.callback.received.failed", "visual_workflow", "runtime.callback.received", err, "visual_generation_results_failed", mergeCallbackFields(visualFields, observability.Fields{"failure_category": visualCallbackFailureCategory(err)}))
			observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "visual_generation_results_failed", callbackFields)
			h.writeVisualCallbackError(c, err, "Failed to record visual generation results", "ECOMMERCE_VISUAL_GENERATION_RESULT_RECORD_FAILED")
			return
		}
		observability.Event("ecommerce.visual_workflow.runtime.callback.received.finished", "visual_workflow", "runtime.callback.received", mergeCallbackFields(visualFields, observability.Fields{"status": item.Status, "stage": item.Stage, "generation_version_id": item.VersionID, "runtime_job_id": item.RuntimeJobID, "result_asset_count": len(item.ResultAssets)}))
		observability.Event("ecommerce.runtime.callback.results.finished", "image_runtime", "callback.results", callbackFields)
		response.JSONSuccess(c, item)
		return
	}
	var req RecordJobResultsInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "image_job_results_bind_failed", callbackFields)
		response.JSONBindError(c, err, "invalid ecommerce result callback request")
		return
	}
	item, err := h.service.RecordJobResults(c.Param("jobID"), req)
	if err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.runtime.callback.results.failed", "image_runtime", "callback.results", err, "image_job_results_failed", callbackFields)
		response.JSONErrorSemantic(c, response.CodeInternalError, "Failed to record ecommerce image job results", "ECOMMERCE_IMAGE_JOB_RESULT_RECORD_FAILED", "Check internal result payload and asset metadata.")
		return
	}
	observability.Event("ecommerce.runtime.callback.results.finished", "image_runtime", "callback.results", callbackFields)
	response.JSONSuccess(c, item)
}

func imageRuntimeFields(c *gin.Context, req CreateImageJobInput, jobID string) observability.Fields {
	fields := observability.Fields{"request_id": c.GetString("requestID"), "trace_id": c.GetString("traceID"), "org_id": c.GetString("orgID"), "user_id": c.GetString("userID"), "product_id": req.ProductID, "sku_code": req.SKUCode, "scene_type": req.SceneType, "input_mode": req.InputMode, "source_asset_id": req.SourceAssetID}
	if jobID != "" {
		fields["job_id"] = jobID
	}
	return fields
}

func imageCallbackFields(c *gin.Context, callbackType string) observability.Fields {
	return observability.Fields{"request_id": c.GetString("requestID"), "trace_id": c.GetString("traceID"), "job_id": c.Param("jobID"), "callback_type": callbackType, "source_type": c.Query("source_type")}
}

func visualWorkflowCallbackFields(c *gin.Context, sourceType, callbackType string) observability.Fields {
	return observability.Fields{"request_id": c.GetString("requestID"), "trace_id": c.GetString("traceID"), "job_id": c.Param("jobID"), "source_type": sourceType, "callback_type": callbackType}
}

func mergeCallbackFields(base observability.Fields, extra observability.Fields) observability.Fields {
	out := observability.Fields{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func visualCallbackFailureCategory(err error) string {
	switch {
	case visualworkflowmodule.IsInternalCallbackInvalid(err):
		return "payload_invalid"
	case visualworkflowmodule.IsInternalCallbackNotFound(err) || errors.Is(err, gorm.ErrRecordNotFound):
		return "source_mapping_missing"
	default:
		return "callback_processing_failed"
	}
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

func (h *Handler) callbackSourceType(c *gin.Context) string {
	if sourceType := c.Query("source_type"); sourceType != "" {
		return sourceType
	}
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	var envelope struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	if sourceType, ok := envelope.Metadata["source_type"].(string); ok {
		return sourceType
	}
	return ""
}

func (h *Handler) shouldRouteVisualDeconstructionCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if sourceType := h.callbackSourceType(c); sourceType != "" {
		return sourceType == "visual_deconstruction"
	}
	return h.visualWorkflow.HasDeconstructionJob(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualIntentPlannerCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if sourceType := h.callbackSourceType(c); sourceType != "" {
		return sourceType == "visual_intent_planning"
	}
	return h.visualWorkflow.HasIntentPlannerSession(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualPromptPlannerCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if sourceType := h.callbackSourceType(c); sourceType != "" {
		return sourceType == "visual_prompt_planning"
	}
	return h.visualWorkflow.HasPromptPlannerSession(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualStrategyReportCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if sourceType := h.callbackSourceType(c); sourceType != "" {
		return sourceType == "visual_strategy_report"
	}
	return h.visualWorkflow.HasStrategyReportSession(c.Param("jobID"))
}

func (h *Handler) shouldRouteVisualGenerationCallback(c *gin.Context) bool {
	if h.visualWorkflow == nil {
		return false
	}
	if sourceType := h.callbackSourceType(c); sourceType != "" {
		return sourceType == "visual_generation"
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
