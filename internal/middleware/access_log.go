package middleware

import (
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
			log.Error("request.finished", "errors", c.Errors.String())
			return
		}
		log.Info("request.finished")
	}
}
