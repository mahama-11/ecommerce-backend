package middleware

import (
	"crypto/subtle"

	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
)

func RequireInternalService(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		headerSecret := c.GetHeader("X-Internal-Service-Secret")
		if secret == "" || headerSecret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(headerSecret)) != 1 {
			response.JSONErrorSemantic(c, response.CodeUnauthorized, "Unauthorized internal request", "INTERNAL_AUTH_FAILED", "Check internal service authentication.")
			c.Abort()
			return
		}
		if serviceName := c.GetHeader("X-Internal-Service"); serviceName != "" {
			c.Set("internalServiceName", serviceName)
		}
		c.Next()
	}
}
