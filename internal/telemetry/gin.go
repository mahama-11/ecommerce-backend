package telemetry

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func StartGinSpan(c *gin.Context, tracerName, spanName string) trace.Span {
	ctx, span := otel.Tracer(tracerName).Start(c.Request.Context(), spanName)
	c.Request = c.Request.WithContext(ctx)
	return span
}
