package promptcenter

import (
	"net/http"
	"strings"

	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/observability"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Preview(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/prompt-center-handler", "ecommerce.prompt_center.preview")
	defer span.End()
	var req PreviewPromptInput
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.prompt_center.preview.failed", "prompt_center", "preview", err, "prompt_preview_request_invalid", previewFields(c, req, "request_validation"))
		response.JSONBindError(c, err, "invalid prompt preview request")
		return
	}
	fields := previewFields(c, req, "")
	observability.Event("ecommerce.prompt_center.preview.started", "prompt_center", "preview", fields)
	item, err := h.service.Preview(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		span.RecordError(err)
		observability.ErrorEvent("ecommerce.prompt_center.preview.failed", "prompt_center", "preview", err, "prompt_preview_failed", previewFields(c, req, classifyPromptPreviewFailure(err)))
		moduleutil.WritePlatformError(c, err, "Failed to preview ecommerce prompt")
		return
	}
	observability.Event("ecommerce.prompt_center.preview.finished", "prompt_center", "preview", mergePreviewFields(fields, observability.Fields{"prompt_run_id": item.PromptID, "status": item.Status, "template_version_id": item.TemplateVersionID, "validation_valid": item.Validation.Valid, "validation_error_count": len(item.Validation.Errors)}))
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) Get(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/prompt-center-handler", "ecommerce.prompt_center.get")
	defer span.End()
	item, err := h.service.Get(c.GetString("orgID"), c.Param("promptId"))
	if err != nil {
		span.RecordError(err)
		response.JSONErrorSemantic(c, response.CodeNotFound, "Prompt not found", "ECOMMERCE_PROMPT_NOT_FOUND", "Refresh and try again.")
		return
	}
	response.JSONSuccess(c, item)
}

func previewFields(c *gin.Context, req PreviewPromptInput, failureCategory string) observability.Fields {
	fields := observability.Fields{"request_id": c.GetString("requestID"), "trace_id": c.GetString("traceID"), "org_id": c.GetString("orgID"), "user_id": c.GetString("userID"), "product_id": req.ProductID, "sku_code": req.SKUCode, "template_id": req.TemplateID, "template_version_id": req.TemplateVersionID, "template_code": req.TemplateCode, "tool_slug": req.ToolSlug, "scene_type": req.SceneType, "source_asset_count": len(req.SourceAssets), "variable_count": len(req.Variables)}
	if failureCategory != "" {
		fields["failure_category"] = failureCategory
	}
	return fields
}

func mergePreviewFields(base observability.Fields, extra observability.Fields) observability.Fields {
	out := observability.Fields{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func classifyPromptPreviewFailure(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "product") || strings.Contains(msg, "sku"):
		return "product_precondition"
	case strings.Contains(msg, "template"):
		return "template_precondition"
	case strings.Contains(msg, "source asset") || strings.Contains(msg, "asset"):
		return "source_asset_precondition"
	case strings.Contains(msg, "dependency") || strings.Contains(msg, "dependencies"):
		return "dependency_precondition"
	default:
		return "preview_operation"
	}
}
