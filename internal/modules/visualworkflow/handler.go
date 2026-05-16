package visualworkflow

import (
	"net/http"
	"strconv"
	"strings"

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
	item, err := h.service.CreateSession(c.GetString("userID"), c.GetString("orgID"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, sessionDTO(item))
}

func (h *Handler) ListSessions(c *gin.Context) {
	items, err := h.service.ListSessions(c.GetString("orgID"), repository.VisualWorkflowSessionFilter{ProductID: c.Query("product_id"), SKUCode: c.Query("sku_code"), Status: c.Query("status"), Limit: queryInt(c, "limit", 50), Offset: queryInt(c, "offset", 0)})
	if err != nil {
		response.JSONError(c, response.CodeDatabaseError, err.Error())
		return
	}
	response.JSONSuccess(c, gin.H{"items": sessionDTOs(items)})
}

func (h *Handler) GetSession(c *gin.Context) {
	item, err := h.service.GetSession(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	response.JSONSuccess(c, sessionDTO(item))
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
	response.JSONSuccess(c, sessionDTO(item))
}

func (h *Handler) CancelSession(c *gin.Context) {
	item, err := h.service.CancelSession(c.GetString("orgID"), c.Param("session_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
	}
	response.JSONSuccess(c, sessionDTO(item))
}

func (h *Handler) CreateSourceReference(c *gin.Context) {
	var req CreateSourceReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid source reference request")
		return
	}
	item, err := h.service.CreateSourceReference(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
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

func (h *Handler) CreateDeconstructionJob(c *gin.Context) {
	var req CreateDeconstructionJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid deconstruction job request")
		return
	}
	item, err := h.service.CreateDeconstructionJob(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
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
	items, err := h.service.ConfirmSelection(c.GetString("orgID"), c.Param("session_id"), req.ElementIDs)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
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
	response.JSONSuccess(c, view)
}

func (h *Handler) CreateGenerationVersion(c *gin.Context) {
	var req CreateGenerationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid generation version request")
		return
	}
	item, err := h.service.CreateGenerationVersion(c.GetString("orgID"), c.Param("session_id"), req)
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
	response.JSONSuccess(c, gin.H{"items": items})
}

func (h *Handler) GetGenerationVersion(c *gin.Context) {
	item, err := h.service.GetGenerationVersion(c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"))
	if err != nil {
		response.JSONError(c, response.CodeNotFound, err.Error())
		return
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
	item, err := h.service.WritebackSelectedGenerationAsset(c.GetString("userID"), c.GetString("orgID"), c.Param("session_id"), c.Param("version_id"), req)
	if err != nil {
		response.JSONError(c, response.CodeInvalidParameter, err.Error())
		return
	}
	response.JSONSuccess(c, item)
}

func queryInt(c *gin.Context, key string, fallback int) int {
	out, err := strconv.Atoi(c.Query(key))
	if err != nil || out < 0 {
		return fallback
	}
	return out
}
