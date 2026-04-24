package workspace

import (
	"net/http"

	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/repository"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/metrics"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
	audit   *auditmodule.Service
}

func NewHandler(service *Service, auditService *auditmodule.Service) *Handler {
	return &Handler{service: service, audit: auditService}
}

func scopeFromContext(c *gin.Context) repository.Scope {
	userID := c.GetString("userID")
	orgID := c.GetString("orgID")
	if userID == "" {
		userID = "anonymous"
	}
	if orgID == "" {
		orgID = "public-demo"
	}
	return repository.Scope{UserID: userID, OrgID: orgID}
}

func (h *Handler) ListSavedTemplates(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.templates.list")
	defer span.End()
	items, err := h.service.ListSavedTemplates(scopeFromContext(c))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "load saved templates failed", "SAVED_TEMPLATES_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}
func (h *Handler) SaveTemplate(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.template.save")
	defer span.End()
	var payload repository.SavedTemplateRecord
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.JSONBindError(c, err, "invalid template payload")
		return
	}
	items, err := h.service.SaveTemplate(scopeFromContext(c), payload)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "save template failed", "SAVED_TEMPLATE_SAVE_FAILED", "Please try again later.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_workspace_template_saved_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "workspace.template.save", TargetType: "saved_template", TargetID: payload.ID, Status: "success", Details: "saved template written", AfterSnapshot: payload})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, items)
}
func (h *Handler) ListWorkflowEvents(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.workflow_events.list")
	defer span.End()
	items, err := h.service.ListWorkflowEvents(scopeFromContext(c))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "load workflow events failed", "WORKFLOW_EVENTS_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}
func (h *Handler) SaveWorkflowEvent(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.workflow_event.save")
	defer span.End()
	var payload repository.WorkflowEventRecord
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.JSONBindError(c, err, "invalid workflow event payload")
		return
	}
	items, err := h.service.SaveWorkflowEvent(scopeFromContext(c), payload)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "save workflow event failed", "WORKFLOW_EVENT_SAVE_FAILED", "Please try again later.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_workspace_workflow_event_saved_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "workspace.workflow_event.save", TargetType: "workflow_event", TargetID: payload.ID, Status: "success", Details: "workflow event written", AfterSnapshot: payload})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, items)
}
func (h *Handler) ListLinkedAssets(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.linked_assets.list")
	defer span.End()
	items, err := h.service.ListLinkedDesignAssets(scopeFromContext(c))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "load linked design assets failed", "LINKED_ASSETS_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}
func (h *Handler) SaveLinkedAsset(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.linked_asset.save")
	defer span.End()
	var payload repository.LinkedDesignAssetRecord
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.JSONBindError(c, err, "invalid linked asset payload")
		return
	}
	items, err := h.service.SaveLinkedDesignAsset(scopeFromContext(c), payload)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "save linked design asset failed", "LINKED_ASSET_SAVE_FAILED", "Please try again later.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_workspace_linked_asset_saved_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "workspace.linked_asset.save", TargetType: "linked_design_asset", TargetID: payload.ID, Status: "success", Details: "linked design asset written", AfterSnapshot: payload})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, items)
}
func (h *Handler) ListLinkedDeliveries(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.linked_deliveries.list")
	defer span.End()
	items, err := h.service.ListLinkedDeliveries(scopeFromContext(c))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "load linked deliveries failed", "LINKED_DELIVERIES_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}
func (h *Handler) SaveLinkedDelivery(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.linked_delivery.save")
	defer span.End()
	var payload repository.LinkedDeliveryRecord
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.JSONBindError(c, err, "invalid linked delivery payload")
		return
	}
	items, err := h.service.SaveLinkedDelivery(scopeFromContext(c), payload)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "save linked delivery failed", "LINKED_DELIVERY_SAVE_FAILED", "Please try again later.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_workspace_linked_delivery_saved_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "workspace.linked_delivery.save", TargetType: "linked_delivery", TargetID: payload.ID, Status: "success", Details: "linked delivery written", AfterSnapshot: payload})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, items)
}
func (h *Handler) ListTemplateBridges(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.template_bridges.list")
	defer span.End()
	items, err := h.service.ListTemplateBridges(scopeFromContext(c))
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "load template bridges failed", "TEMPLATE_BRIDGES_LOAD_FAILED", "Please try again later.")
		return
	}
	response.JSONSuccess(c, items)
}
func (h *Handler) SaveTemplateBridge(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/workspace-handler", "ecommerce.workspace.template_bridge.save")
	defer span.End()
	var payload repository.LinkedTemplateBridgeRecord
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.JSONBindError(c, err, "invalid template bridge payload")
		return
	}
	items, err := h.service.SaveTemplateBridge(scopeFromContext(c), payload)
	if err != nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, "save template bridge failed", "TEMPLATE_BRIDGE_SAVE_FAILED", "Please try again later.")
		return
	}
	metrics.IncBusinessCounter("ecommerce_workspace_template_bridge_saved_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "workspace.template_bridge.save", TargetType: "template_bridge", TargetID: payload.ID, Status: "success", Details: "template bridge written", AfterSnapshot: payload})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, items)
}
