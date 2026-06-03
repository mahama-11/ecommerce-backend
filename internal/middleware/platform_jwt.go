package middleware

import (
	"errors"
	"strings"
	"time"

	"ecommerce-service/internal/observability"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func PlatformJWTAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseClaims(c.GetHeader("Authorization"), jwtSecret)
		if !ok {
			if isAuthSessionPath(c) {
				lc := observability.StartGin(c, "ecommerce-service/auth-middleware", "ecommerce.auth.session.verify", "ecommerce.auth.session.verify", "auth", "session.verify", observability.Fields{})
				lc.Fail(errors.New("invalid or missing session token"), "session_invalid", observability.Fields{"failure_category": "session_invalid"})
			}
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

func isAuthSessionPath(c *gin.Context) bool {
	return c != nil && strings.Contains(c.FullPath(), "/auth/session")
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
