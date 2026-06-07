package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/observability"
	"ecommerce-service/internal/platform"
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
	var req RegisterInput
	if err := c.ShouldBindJSON(&req); err != nil {
		lc := startAuthLifecycle(c, "register", observability.Fields{"has_email": false})
		lc.Fail(err, "request_validation", observability.Fields{"failure_category": "request_validation"})
		response.JSONBindError(c, err, "invalid register request")
		return
	}
	lc := startAuthLifecycle(c, "register", observability.Fields{"has_email": strings.TrimSpace(req.Email) != ""})
	result, err := h.service.Register(req)
	if err != nil {
		category := classifyAuthFailure("register", err)
		lc.Fail(err, category, observability.Fields{"failure_category": category})
		writePlatformError(c, err, "register failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_auth_register_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "auth.register", TargetType: "user", TargetID: result.User.ID, Status: "success", Details: "product register completed", AfterSnapshot: result.User})
	}
	lc.Finish(observability.Fields{"user_id": result.User.ID, "org_id": result.User.OrgID, "has_email": result.User.Email != ""})
	response.JSONSuccessWithStatus(c, http.StatusCreated, result)
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginInput
	if err := c.ShouldBindJSON(&req); err != nil {
		lc := startAuthLifecycle(c, "login", observability.Fields{"has_email": false})
		lc.Fail(err, "request_validation", observability.Fields{"failure_category": "request_validation"})
		response.JSONBindError(c, err, "invalid login request")
		return
	}
	lc := startAuthLifecycle(c, "login", observability.Fields{"has_email": strings.TrimSpace(req.Email) != ""})
	result, err := h.service.Login(req)
	if err != nil {
		category := classifyAuthFailure("login", err)
		lc.Fail(err, category, observability.Fields{"failure_category": category})
		writePlatformError(c, err, "login failed")
		return
	}
	metrics.IncBusinessCounter("ecommerce_auth_login_total")
	if h.audit != nil {
		_ = h.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "auth.login", TargetType: "user", TargetID: result.User.ID, Status: "success", Details: "product login completed", AfterSnapshot: result.User})
	}
	lc.Finish(observability.Fields{"user_id": result.User.ID, "org_id": result.User.OrgID, "has_email": result.User.Email != ""})
	response.JSONSuccess(c, result)
}

func (h *Handler) Session(c *gin.Context) {
	lc := startAuthLifecycle(c, "session.verify", observability.Fields{"user_id": c.GetString("userID"), "org_id": c.GetString("orgID")})
	result, err := h.service.Session(c.GetString("userID"), c.GetString("orgID"))
	if err != nil {
		category := classifyAuthFailure("session.verify", err)
		lc.Fail(err, category, observability.Fields{"failure_category": category, "user_id": c.GetString("userID"), "org_id": c.GetString("orgID")})
		writePlatformError(c, err, "load session failed")
		return
	}
	lc.Finish(observability.Fields{"user_id": result.User.ID, "org_id": result.User.OrgID, "has_email": result.User.Email != ""})
	response.JSONSuccess(c, result)
}

func startAuthLifecycle(c *gin.Context, operation string, fields observability.Fields) *observability.Lifecycle {
	return observability.StartGin(c, "ecommerce-service/auth-handler", "ecommerce.auth."+operation, "ecommerce.auth."+operation, "auth", operation, fields)
}

func classifyAuthFailure(operation string, err error) string {
	switch {
	case err == nil:
		return "auth_internal"
	case operation == "register" && platform.IsConflict(err):
		return "user_exists"
	case operation == "login" && platform.IsUnauthorized(err):
		return "credential_invalid"
	case operation == "session.verify" && (platform.IsUnauthorized(err) || platform.IsNotFound(err)):
		return "session_invalid"
	}
	code := strings.ToUpper(platform.ErrorCode(err))
	switch {
	case strings.Contains(code, "EMAIL_ALREADY_EXISTS") || strings.Contains(code, "USER_EXISTS"):
		return "user_exists"
	case strings.Contains(code, "INVALID_CREDENTIAL") || strings.Contains(code, "CREDENTIAL"):
		return "credential_invalid"
	case strings.Contains(code, "TOKEN") || strings.Contains(strings.ToLower(err.Error()), "token"):
		return "token_issue_failed"
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return "platform_sync_failed"
	case platform.StatusCode(err) >= 500:
		return "platform_sync_failed"
	default:
		return "auth_internal"
	}
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
