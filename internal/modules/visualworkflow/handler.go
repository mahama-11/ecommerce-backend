package visualworkflow

import (
	"net/http"
	"strconv"
	"strings"

	"ecommerce-service/internal/observability"
	"ecommerce-service/internal/repository"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) CreateProductSession(c *gin.Context) {
	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid visual workflow session request")
		return
	}
	pathProductID := strings.TrimSpace(c.Param("product_id"))
	if strings.TrimSpace(req.ProductID) != "" && strings.TrimSpace(req.ProductID) != pathProductID {
		response.JSONError(c, response.CodeInvalidParameter, "product_id in request body must match path product_id")
		return
	}
	req.ProductID = pathProductID
	h.createSession(c, req)
}

func (h *Handler) CreateSession(c *gin.Context) {
	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid visual workflow session request")
		return
	}
	h.createSession(c, req)
}

func (h *Handler) createSession(c *gin.Context, req CreateSessionRequest) {
	lc := observability.StartGin(c, "ecommerce-service/visual-workflow-handler", "ecommerce.visual_workflow.session.create", "ecommerce.visual_workflow.session.create", "visual_workflow", "session.create", observability.Fields{"product_id": req.ProductID, "sku_code": req.SKUCode})
	item, err := h.service.CreateSession(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		lc.Fail(err, "invalid_parameter", nil)
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	lc.Finish(observability.Fields{"session_id": item.ID, "product_id": item.ProductID, "sku_code": item.SKUCode})
	response.JSONSuccessWithStatus(c, http.StatusCreated, sessionDTO(item))
}

func (h *Handler) ListSessions(c *gin.Context) {
	items, err := h.service.ListSessions(c.GetString("orgID"), repository.VisualWorkflowSessionFilter{ProductID: c.Query("product_id"), SKUCode: c.Query("sku_code"), Status: c.Query("status"), Limit: queryInt(c, "limit", 50), Offset: queryInt(c, "offset", 0)})
	if err != nil {
		response.JSONError(c, response.CodeDatabaseError, err.Error())
		return
	}
	dtos := sessionDTOs(items)
	for _, dto := range dtos {
		applySessionProjection(dto, c.Query("projection"))
	}
	response.JSONSuccess(c, gin.H{"items": dtos})
}

func (h *Handler) GetSession(c *gin.Context) {
	item, err := h.service.GetSession(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	dto := sessionDTO(item)
	applySessionProjection(dto, c.Query("projection"))
	response.JSONSuccess(c, dto)
}

func (h *Handler) UpdateSession(c *gin.Context) {
	var req UpdateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid visual workflow session update")
		return
	}
	item, err := h.service.UpdateSession(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	dto := sessionDTO(item)
	applySessionProjection(dto, c.Query("projection"))
	response.JSONSuccess(c, dto)
}

func (h *Handler) CancelSession(c *gin.Context) {
	item, err := h.service.CancelSession(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	dto := sessionDTO(item)
	applySessionProjection(dto, c.Query("projection"))
	response.JSONSuccess(c, dto)
}

func (h *Handler) CreateSourceReference(c *gin.Context) {
	var req CreateSourceReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid source reference request")
		return
	}
	lc := observability.StartGin(c, "ecommerce-service/visual-workflow-handler", "ecommerce.visual_workflow.source_reference.create", "ecommerce.visual_workflow.source_reference.create", "visual_workflow", "source_reference.create", observability.Fields{"session_id": c.Param("session_id"), "source_kind": req.SourceKind})
	item, err := h.service.CreateSourceReference(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		lc.Fail(err, "invalid_parameter", nil)
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	lc.Finish(observability.Fields{"session_id": item.SessionID, "source_reference_id": item.ID, "status": item.Status})
	response.JSONSuccessWithStatus(c, http.StatusCreated, sourceDTO(item))
}

func (h *Handler) ListSourceReferences(c *gin.Context) {
	items, err := h.service.ListSourceReferences(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	dtos := make([]*SourceReferenceDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, sourceDTO(&items[i]))
	}
	response.JSONSuccess(c, gin.H{"items": dtos})
}

func (h *Handler) UpdateSourceReference(c *gin.Context) {
	var req UpdateSourceReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid source reference update")
		return
	}
	item, err := h.service.UpdateSourceReference(c.GetString("orgID"), c.Param("session_id"), c.Param("source_reference_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, sourceDTO(item))
}

func (h *Handler) ArchiveSourceReference(c *gin.Context) {
	item, err := h.service.ArchiveSourceReference(c.GetString("orgID"), c.Param("session_id"), c.Param("source_reference_id"))
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, sourceDTO(item))
}

func (h *Handler) CreateDeconstructionJob(c *gin.Context) {
	var req CreateDeconstructionJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid deconstruction job request")
		return
	}
	lc := observability.StartGin(c, "ecommerce-service/visual-workflow-handler", "ecommerce.visual_workflow.deconstruction_job.create", "ecommerce.visual_workflow.deconstruction_job.create", "visual_workflow", "deconstruction_job.create", observability.Fields{"session_id": c.Param("session_id"), "source_reference_id": req.SourceReferenceID})
	item, err := h.service.CreateDeconstructionJob(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		lc.Fail(err, "invalid_parameter", nil)
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	lc.Finish(observability.Fields{"session_id": item.SessionID, "job_id": item.ID, "status": item.Status})
	response.JSONSuccessWithStatus(c, http.StatusCreated, jobDTO(item))
}

func (h *Handler) GetDeconstructionJob(c *gin.Context) {
	item, err := h.service.GetDeconstructionJob(c.GetString("orgID"), c.Param("session_id"), c.Param("job_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	response.JSONSuccess(c, jobDTO(item))
}

func (h *Handler) ListElements(c *gin.Context) {
	items, err := h.service.ListElements(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	response.JSONSuccess(c, gin.H{"items": items})
}

func (h *Handler) UpdateElement(c *gin.Context) {
	var req UpdateElementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid deconstruction element update")
		return
	}
	item, err := h.service.UpdateElement(c.GetString("orgID"), c.Param("session_id"), c.Param("element_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) ConfirmSelection(c *gin.Context) {
	var req ConfirmSelectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid deconstruction selection confirmation")
		return
	}
	lc := observability.StartGin(c, "ecommerce-service/visual-workflow-handler", "ecommerce.visual_workflow.element.confirm", "ecommerce.visual_workflow.element.confirm", "visual_workflow", "element.confirm", observability.Fields{"session_id": c.Param("session_id"), "element_count": len(req.ElementIDs)})
	items, err := h.service.ConfirmSelection(c.GetString("orgID"), c.Param("session_id"), req.ElementIDs)
	if err != nil {
		lc.Fail(err, "invalid_parameter", nil)
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	lc.Finish(observability.Fields{"session_id": c.Param("session_id"), "confirmed_count": len(items)})
	response.JSONSuccess(c, gin.H{"items": items})
}

func (h *Handler) ApplyAttentionTree(c *gin.Context) {
	var req ApplyAttentionTreeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid attention tree request")
		return
	}
	items, err := h.service.ApplyAttentionTree(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, items)
}

func (h *Handler) CreateIntentPlannerJob(c *gin.Context) {
	var req CreateIntentPlannerJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid intent planner job request")
		return
	}
	item, err := h.service.CreateIntentPlannerJob(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) CreatePromptPlannerJob(c *gin.Context) {
	var req CreatePromptPlannerJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid prompt planner job request")
		return
	}
	item, err := h.service.CreatePromptPlannerJob(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) CreateStrategyReportJob(c *gin.Context) {
	var req CreateStrategyReportJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid strategy report job request")
		return
	}
	item, err := h.service.CreateStrategyReportJob(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) StageView(c *gin.Context) {
	view, err := h.service.StageView(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	applyStageViewProjection(view, c.Query("projection"))
	response.JSONSuccess(c, view)
}

func applySessionProjection(session *SessionDTO, projection string) {
	if session == nil || isFullProjection(projection) {
		return
	}
	session.GenerationVersions = compactGenerationVersions(session.GenerationVersions)
}

func applyStageViewProjection(view *StageViewDTO, projection string) {
	if view == nil {
		return
	}
	switch strings.TrimSpace(projection) {
	case "sandbox":
		view.SourceReference = nil
		view.SourceReferences = nil
		view.DeconstructionJob = nil
		view.DeconstructionElements = nil
		view.GenerationVersions = nil
		view.BusinessFlow = nil
		view.IntegrationVerdict = nil
		view.RollbackSnapshot = nil
		view.ReleaseReadiness = nil
	case "sources":
		view.DeconstructionJob = nil
		view.DeconstructionElements = nil
		view.IntentSpec = IntentSpecDTO{}
		view.PromptPlan = PromptPlanDTO{}
		view.GenerationVersions = nil
		view.BusinessFlow = nil
		view.IntegrationVerdict = nil
		view.RollbackSnapshot = nil
		view.ReleaseReadiness = nil
	case "workshop":
		view.SourceReference = nil
		view.SourceReferences = nil
		view.DeconstructionJob = nil
		view.DeconstructionElements = nil
		view.IntentSpec = IntentSpecDTO{}
		view.PromptPlan = PromptPlanDTO{}
		view.GenerationVersions = compactGenerationVersions(view.GenerationVersions)
		view.BusinessFlow = nil
		view.IntegrationVerdict = nil
		view.RollbackSnapshot = nil
		view.ReleaseReadiness = nil
	}
}

func isFullProjection(projection string) bool {
	projection = strings.TrimSpace(projection)
	return projection == "full" || projection == "debug"
}

func compactGenerationVersions(items []GenerationVersionDTO) []GenerationVersionDTO {
	if items == nil {
		return nil
	}
	out := make([]GenerationVersionDTO, 0, len(items))
	for i := range items {
		out = append(out, compactGenerationVersion(items[i]))
	}
	return out
}

func compactGenerationVersion(item GenerationVersionDTO) GenerationVersionDTO {
	item.IntentSpecSnapshot = nil
	item.Metadata = compactGenerationMetadata(item.Metadata)
	item.ResultAssets = compactResultAssets(item.ResultAssets)
	return item
}

func compactGenerationMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	allowed := []string{"source", "fanout_batch_id", "fanout_run_id", "fanout_attempt", "fanout_wave", "template_id", "template_version_id", "slot_index", "scene_tag", "config", "ui_refinement_weights", "parent_version_id", "source_version_id", "prompt_id", "writeback", "idempotency_key"}
	out := map[string]any{}
	for _, key := range allowed {
		if value, ok := metadata[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactResultAssets(items []ResultAssetDTO) []ResultAssetDTO {
	if items == nil {
		return nil
	}
	out := make([]ResultAssetDTO, 0, len(items))
	for i := range items {
		asset := items[i]
		asset.Metadata = compactResultAssetMetadata(asset.Metadata)
		out = append(out, asset)
	}
	return out
}

func compactResultAssetMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	allowed := []string{"width", "height", "mime_type", "file_name", "checksum", "duration_ms", "slot_index", "template_id", "template_version_id", "scene_tag"}
	out := map[string]any{}
	for _, key := range allowed {
		if value, ok := metadata[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *Handler) CreateGenerationVersion(c *gin.Context) {
	var req CreateGenerationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid generation version request")
		return
	}
	lc := observability.StartGin(c, "ecommerce-service/visual-workflow-handler", "ecommerce.visual_workflow.generation_version.create", "ecommerce.visual_workflow.generation_version.create", "visual_workflow", "generation_version.create", observability.Fields{"session_id": c.Param("session_id"), "result_asset_count": len(req.ResultAssets)})
	item, err := h.service.CreateGenerationVersion(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		lc.Fail(err, "invalid_parameter", nil)
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	lc.Finish(observability.Fields{"session_id": c.Param("session_id"), "job_id": item.RuntimeJobID, "status": item.Status})
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) CreateGenerationFanout(c *gin.Context) {
	var req CreateGenerationFanoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid generation fan-out request")
		return
	}
	item, err := h.service.CreateGenerationFanout(c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) ListGenerationVersions(c *gin.Context) {
	items, err := h.service.ListGenerationVersions(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	if !isFullProjection(c.Query("projection")) {
		items = compactGenerationVersions(items)
	}
	response.JSONSuccess(c, gin.H{"items": items})
}

func (h *Handler) GetGenerationVersion(c *gin.Context) {
	item, err := h.service.GetGenerationVersion(c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	if !isFullProjection(c.Query("projection")) {
		compact := compactGenerationVersion(*item)
		item = &compact
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) UpdateGenerationVersion(c *gin.Context) {
	var req UpdateGenerationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid generation version update")
		return
	}
	item, err := h.service.UpdateGenerationVersion(c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) SelectGenerationVersion(c *gin.Context) {
	var req SelectGenerationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid generation version selection")
		return
	}
	item, err := h.service.SelectGenerationVersion(c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, item)
}

func (h *Handler) SaveGenerationVersionAsTemplate(c *gin.Context) {
	var req SaveGenerationTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid generation template save request")
		return
	}
	item, err := h.service.SaveGenerationVersionAsTemplate(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, item)
}

func (h *Handler) WritebackSelectedGenerationAsset(c *gin.Context) {
	var req WritebackSelectedGenerationAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid selected generation asset writeback")
		return
	}
	lc := observability.StartGin(c, "ecommerce-service/visual-workflow-handler", "ecommerce.visual_workflow.asset.writeback", "ecommerce.visual_workflow.asset.writeback", "visual_workflow", "asset.writeback", observability.Fields{"session_id": c.Param("session_id"), "version_id": c.Param("version_id"), "asset_id": req.AssetID})
	item, err := h.service.WritebackSelectedGenerationAsset(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"), req)
	if err != nil {
		lc.Fail(err, "invalid_parameter", nil)
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	lc.Finish(observability.Fields{"session_id": c.Param("session_id"), "version_id": c.Param("version_id"), "asset_id": req.AssetID, "product_id": item.ProductID})
	response.JSONSuccess(c, item)
}

func queryInt(c *gin.Context, key string, fallback int) int {
	out, err := strconv.Atoi(c.Query(key))
	if err != nil || out < 0 {
		return fallback
	}
	return out
}
