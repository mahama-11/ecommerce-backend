package middleware

import (
	"strings"
	"time"

	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func PlatformJWTAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseClaims(c.GetHeader("Authorization"), jwtSecret)
		if !ok {
			response.JSONErrorSemantic(c, 401, "Invalid or missing token", "TOKEN_INVALID", "Sign in again to continue.")
			c.Abort()
			return
		}
		setClaims(c, claims)
		c.Next()
	}
}

func OptionalPlatformJWTAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseClaims(c.GetHeader("Authorization"), jwtSecret)
		if ok {
			setClaims(c, claims)
		}
		c.Next()
	}
}

func parseClaims(authHeader, jwtSecret string) (jwt.MapClaims, bool) {
	if authHeader == "" {
		return nil, false
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, false
	}
	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (any, error) { return []byte(jwtSecret), nil })
	if err != nil || !token.Valid {
		return nil, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, false
	}
	if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
		return nil, false
	}
	return claims, true
}

func setClaims(c *gin.Context, claims jwt.MapClaims) {
	userID, _ := claims["user_id"].(string)
	orgID, _ := claims["org_id"].(string)
	orgRole, _ := claims["org_role"].(string)
	if userID != "" {
		c.Set("userID", userID)
	}
	if orgID != "" {
		c.Set("orgID", orgID)
	}
	if orgRole != "" {
		c.Set("orgRole", orgRole)
	}
}
