package middleware

import (
	"time"

	"ecommerce-service/pkg/metrics"

	"github.com/gin-gonic/gin"
)

func Metrics(namespace, subsystem string, buckets []float64) gin.HandlerFunc {
	metrics.Configure(namespace, subsystem, buckets)
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		metrics.RecordHTTPRequest(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(start))
	}
}

func MetricsHandler(namespace, subsystem string, buckets []float64) gin.HandlerFunc {
	metrics.Configure(namespace, subsystem, buckets)
	handler := metrics.Handler()
	return func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}
