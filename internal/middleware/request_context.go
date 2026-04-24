package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func RequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = buildRequestID("req")
		}
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = requestID
		}
		if spanCtx := trace.SpanFromContext(c.Request.Context()).SpanContext(); spanCtx.IsValid() {
			traceID = spanCtx.TraceID().String()
		}

		c.Set("requestID", requestID)
		c.Set("traceID", traceID)
		c.Set("requestStartedAt", time.Now())
		ctx := context.WithValue(c.Request.Context(), "request_id", requestID)
		ctx = context.WithValue(ctx, "trace_id", traceID)
		c.Request = c.Request.WithContext(ctx)

		c.Writer.Header().Set("X-Request-ID", requestID)
		c.Writer.Header().Set("X-Trace-ID", traceID)
		c.Next()
	}
}

func buildRequestID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
