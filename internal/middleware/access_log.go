package middleware

import (
	"regexp"
	"time"

	"ecommerce-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		log := logger.With(
			"request_id", c.GetString("requestID"),
			"trace_id", c.GetString("traceID"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", c.FullPath(),
			"client_ip", c.ClientIP(),
		)
		log.Info("request.started")
		c.Next()

		log = log.With(
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"user_id", c.GetString("userID"),
			"org_id", c.GetString("orgID"),
		)
		if len(c.Errors) > 0 {
			log.Error("request.finished", "errors", redactLogError(c.Errors.String()))
			return
		}
		log.Info("request.finished")
	}
}

var accessLogSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)((?:token|secret|password|provider_key|provider_payload|storage_key)=)[^\s,;]+`),
	regexp.MustCompile(`(?i)((?:token|secret|password|provider_key|provider_payload|storage_key)":")[^"]+`),
	regexp.MustCompile(`(?i)((?:postgres|postgresql|mysql)://[^:]+:)[^@\s]+(@)`),
}

func redactLogError(message string) string {
	for _, pattern := range accessLogSensitivePatterns {
		message = pattern.ReplaceAllString(message, `${1}[redacted]${2}`)
	}
	return message
}
