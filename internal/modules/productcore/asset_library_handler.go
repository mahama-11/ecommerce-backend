package productcore

import (
	"ecommerce-service/internal/modules/moduleutil"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) ListAssetLibrary(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.asset_library.list")
	defer span.End()

	var input AssetLibraryFilterInput
	if err := c.ShouldBindQuery(&input); err != nil {
		response.JSONBindError(c, err, "invalid asset library query")
		return
	}
	orgID, _ := scopeFromGin(c)
	result, err := h.service.WithContext(c.Request.Context()).ListAssetLibrary(orgID, input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "list asset library failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) AssetLibraryStats(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.asset_library.stats")
	defer span.End()

	var input AssetLibraryFilterInput
	if err := c.ShouldBindQuery(&input); err != nil {
		response.JSONBindError(c, err, "invalid asset library stats query")
		return
	}
	orgID, _ := scopeFromGin(c)
	result, err := h.service.WithContext(c.Request.Context()).AssetLibraryStats(orgID, input, c.Query("group_by"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "get asset library stats failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) UpdateAssetLibraryGovernance(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.asset_library.governance")
	defer span.End()

	var input UpdateAssetGovernanceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid asset governance request")
		return
	}
	orgID, userID := scopeFromGin(c)
	result, err := h.service.WithContext(c.Request.Context()).UpdateAssetLibraryGovernance(orgID, userID, c.Param("relationId"), input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "update asset governance failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) BatchUpdateAssetLibraryGovernance(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.asset_library.batch_governance")
	defer span.End()

	var input BatchAssetGovernanceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.JSONBindError(c, err, "invalid batch asset governance request")
		return
	}
	orgID, userID := scopeFromGin(c)
	result, err := h.service.WithContext(c.Request.Context()).BatchUpdateAssetLibraryGovernance(orgID, userID, input)
	if err != nil {
		moduleutil.WritePlatformError(c, err, "batch update asset governance failed")
		return
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) GetAssetLibraryLineage(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/productcore-handler", "ecommerce.productcore.asset_library.lineage")
	defer span.End()

	orgID, _ := scopeFromGin(c)
	result, err := h.service.WithContext(c.Request.Context()).GetAssetLibraryLineage(orgID, c.Param("relationId"))
	if err != nil {
		moduleutil.WritePlatformError(c, err, "get asset lineage failed")
		return
	}
	response.JSONSuccess(c, result)
}
