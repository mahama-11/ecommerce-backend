package moduleutil

import (
	"net/http"
	"strconv"

	"ecommerce-service/internal/platform"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func QueryInt(c *gin.Context, key string, fallback int) int {
	raw := c.Query(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func WritePlatformError(c *gin.Context, err error, fallback string) {
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
