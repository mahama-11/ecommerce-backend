package response

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type ResponseCode int

const (
	CodeSuccess ResponseCode = 0

	CodeInvalidParameter ResponseCode = 1000
	CodeUnauthorized     ResponseCode = 1001
	CodeBadRequest       ResponseCode = 1002
	CodeForbidden        ResponseCode = 1003
	CodeNotFound         ResponseCode = 1004
	CodeConflict         ResponseCode = 1005
	CodeMethodNotAllowed ResponseCode = 1006
	CodeTooManyRequests  ResponseCode = 1007
	CodeMissingParameter ResponseCode = 1008

	CodeBusinessError      ResponseCode = 2000
	CodeExternalDependency ResponseCode = 2001

	CodeInternalError      ResponseCode = 5000
	CodeDatabaseError      ResponseCode = 5001
	CodeThirdPartyError    ResponseCode = 5002
	CodeServiceUnavailable ResponseCode = 5003
)

var ResponseMessage = map[ResponseCode]string{
	CodeSuccess:            "success",
	CodeInvalidParameter:   "Invalid parameter",
	CodeUnauthorized:       "Unauthorized",
	CodeBadRequest:         "Invalid request parameters",
	CodeForbidden:          "Forbidden",
	CodeNotFound:           "Resource not found",
	CodeConflict:           "Resource conflict",
	CodeMethodNotAllowed:   "Method not allowed",
	CodeTooManyRequests:    "Too many requests",
	CodeMissingParameter:   "Missing required parameter",
	CodeBusinessError:      "Business operation failed",
	CodeExternalDependency: "Upstream platform request failed",
	CodeInternalError:      "Internal server error",
	CodeDatabaseError:      "Database operation failed",
	CodeThirdPartyError:    "Third party service error",
	CodeServiceUnavailable: "Service temporarily unavailable",
}

type BaseResponse struct {
	Code      ResponseCode `json:"code"`
	Message   string       `json:"message"`
	Timestamp int64        `json:"timestamp"`
	RequestID string       `json:"request_id,omitempty"`
}

type SuccessResponse struct {
	BaseResponse
	Data any `json:"data,omitempty"`
}

type ErrorResponse struct {
	BaseResponse
	Error     string       `json:"error,omitempty"`
	ErrorCode string       `json:"error_code,omitempty"`
	ErrorHint string       `json:"error_hint,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   string `json:"value,omitempty"`
}

func NewSuccessResponse(data any) *SuccessResponse {
	return &SuccessResponse{BaseResponse: BaseResponse{Code: CodeSuccess, Message: ResponseMessage[CodeSuccess]}, Data: data}
}

func NewErrorResponse(code ResponseCode, errMsg string) *ErrorResponse {
	return &ErrorResponse{BaseResponse: BaseResponse{Code: code, Message: ResponseMessage[code]}, Error: errMsg}
}

func NewSemanticErrorResponse(code ResponseCode, errMsg, errorCode, errorHint string) *ErrorResponse {
	resp := NewErrorResponse(code, errMsg)
	resp.ErrorCode = errorCode
	resp.ErrorHint = errorHint
	return resp
}

func NewErrorResponseWithFields(code ResponseCode, errMsg string, fieldErrors []FieldError) *ErrorResponse {
	resp := NewErrorResponse(code, errMsg)
	resp.Errors = fieldErrors
	return resp
}

func (r *BaseResponse) SetRequestInfo(c *gin.Context) {
	r.Timestamp = time.Now().UnixMilli()
	r.RequestID = c.GetString("requestID")
}

func JSONSuccess(c *gin.Context, data any) {
	resp := NewSuccessResponse(data)
	resp.SetRequestInfo(c)
	c.JSON(http.StatusOK, resp)
}

func JSONSuccessWithStatus(c *gin.Context, status int, data any) {
	resp := NewSuccessResponse(data)
	resp.SetRequestInfo(c)
	c.JSON(status, resp)
}

func JSONError(c *gin.Context, code ResponseCode, message string) {
	resp := NewErrorResponse(code, message)
	resp.SetRequestInfo(c)
	c.JSON(GetHTTPStatusCode(code), resp)
}

func JSONErrorSemantic(c *gin.Context, code ResponseCode, message, errorCode, errorHint string) {
	resp := NewSemanticErrorResponse(code, message, errorCode, errorHint)
	resp.SetRequestInfo(c)
	c.JSON(GetHTTPStatusCode(code), resp)
}

func JSONErrorWithStatus(c *gin.Context, code ResponseCode, message string, status int) {
	resp := NewErrorResponse(code, message)
	resp.SetRequestInfo(c)
	c.JSON(status, resp)
}

func JSONErrorWithStatusSemantic(c *gin.Context, code ResponseCode, message, errorCode, errorHint string, status int) {
	resp := NewSemanticErrorResponse(code, message, errorCode, errorHint)
	resp.SetRequestInfo(c)
	c.JSON(status, resp)
}

func JSONErrorWithFields(c *gin.Context, code ResponseCode, message string, fieldErrors []FieldError) {
	resp := NewErrorResponseWithFields(code, message, fieldErrors)
	resp.SetRequestInfo(c)
	c.JSON(GetHTTPStatusCode(code), resp)
}

func JSONBindError(c *gin.Context, err error, fallback string) {
	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		fields := make([]FieldError, 0, len(validationErrs))
		for _, fieldErr := range validationErrs {
			fields = append(fields, FieldError{Field: fieldErr.Field(), Message: validationMessage(fieldErr), Value: fmt.Sprintf("%v", fieldErr.Value())})
		}
		JSONErrorWithFields(c, CodeInvalidParameter, fallback, fields)
		return
	}
	JSONError(c, CodeInvalidParameter, fallback)
}

func GetHTTPStatusCode(code ResponseCode) int {
	switch code {
	case CodeSuccess:
		return http.StatusOK
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case CodeTooManyRequests:
		return http.StatusTooManyRequests
	case CodeInternalError, CodeDatabaseError, CodeThirdPartyError, CodeServiceUnavailable:
		return http.StatusInternalServerError
	default:
		if code >= 1000 && code < 2000 {
			return http.StatusBadRequest
		}
		if code >= 2000 && code < 3000 {
			return http.StatusBadGateway
		}
		return http.StatusBadRequest
	}
}

func validationMessage(fieldErr validator.FieldError) string {
	switch fieldErr.Tag() {
	case "required":
		return "field is required"
	case "email":
		return "field must be a valid email"
	case "min":
		return "field value is below minimum"
	case "max":
		return "field value exceeds maximum"
	default:
		return "field validation failed"
	}
}
