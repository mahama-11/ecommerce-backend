package auth

import (
	"net/http"

	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/platform"
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

func (h *Handler) Register(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/auth-handler", "ecommerce.auth.register")
	defer span.End()

	var req RegisterInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid register request")
		return
	}
	result, err := h.service.Register(req)
	if err != nil {
		writePlatformError(c, err, "register failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_auth_register_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "auth.register", TargetType: "user", TargetID: result.User.ID, Status: "success", Details: "product register completed", AfterSnapshot: result.User})
	}
	response.JSONSuccessWithStatus(c, http.StatusCreated, result)
}

func (h *Handler) Login(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/auth-handler", "ecommerce.auth.login")
	defer span.End()

	var req LoginInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.JSONBindError(c, err, "invalid login request")
		return
	}
	result, err := h.service.Login(req)
	if err != nil {
		writePlatformError(c, err, "login failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_auth_login_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "auth.login", TargetType: "user", TargetID: result.User.ID, Status: "success", Details: "product login completed", AfterSnapshot: result.User})
	}
	response.JSONSuccess(c, result)
}

func (h *Handler) Session(c *gin.Context) {
	span := telemetry.StartGinSpan(c, "ecommerce-service/auth-handler", "ecommerce.auth.session")
	defer span.End()

	result, err := h.service.Session(c.GetString("userID"), c.GetString("orgID"))
	if err != nil {
		writePlatformError(c, err, "load session failed")
		return
	}
	response.JSONSuccess(c, result)
}

func writePlatformError(c *gin.Context, err error, fallback string) {
	if err == nil {
		response.JSONErrorSemantic(c, response.CodeInternalError, fallback, "INTERNAL_ERROR", "Please try again later.")
		return
	}
	status := http.StatusInternalServerError
	if platform.IsConflict(err) {
		status = http.StatusConflict
	} else if platform.IsUnauthorized(err) {
		status = http.StatusUnauthorized
	} else if platform.IsNotFound(err) {
		status = http.StatusNotFound
	}
	errorCode := platform.ErrorCode(err)
	if errorCode == "" {
		errorCode = "UPSTREAM_REQUEST_FAILED"
	}
	errorHint := platform.ErrorHint(err)
	if errorHint == "" {
		errorHint = "Please try again later."
	}
	responseCode := response.CodeExternalDependency
	switch status {
	case http.StatusUnauthorized:
		responseCode = response.CodeUnauthorized
	case http.StatusConflict:
		responseCode = response.CodeConflict
	case http.StatusNotFound:
		responseCode = response.CodeNotFound
	}
	response.JSONErrorWithStatusSemantic(c, responseCode, err.Error(), errorCode, errorHint, status)
}
