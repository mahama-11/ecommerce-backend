package app

import (
	"strings"
	"testing"

	"ecommerce-service/internal/config"

	"github.com/gin-gonic/gin"
)

func TestValidateProductionSecretsRejectsDefaultWeakValues(t *testing.T) {
	cfg := config.Config{
		GinMode:  gin.ReleaseMode,
		Security: config.SecurityConfig{JWTSecret: "ecommerce-dev-secret", EncryptionKey: "strong-encryption-key-for-test", ServiceSecretKey: "strong-service-secret-for-test"},
		Platform: config.PlatformConfig{InternalServiceSecret: "strong-platform-internal-secret", JWTSecret: "strong-platform-jwt-secret"},
	}
	err := validateProductionSecrets(cfg)
	if err == nil || !strings.Contains(err.Error(), "security.jwt_secret") {
		t.Fatalf("expected weak production jwt secret rejection, got %v", err)
	}
}

func TestValidateProductionSecretsAllowsDevDefaultsAndStrongReleaseSecrets(t *testing.T) {
	dev := config.Config{GinMode: gin.DebugMode}
	if err := validateProductionSecrets(dev); err != nil {
		t.Fatalf("debug defaults should be allowed for local/dev: %v", err)
	}
	release := config.Config{
		GinMode:  gin.ReleaseMode,
		Security: config.SecurityConfig{JWTSecret: "strong-jwt-secret-for-release", EncryptionKey: "strong-encryption-key-for-release", ServiceSecretKey: "strong-service-secret-for-release"},
		Platform: config.PlatformConfig{InternalServiceSecret: "strong-platform-internal-secret", JWTSecret: "strong-platform-jwt-secret"},
	}
	if err := validateProductionSecrets(release); err != nil {
		t.Fatalf("strong release secrets rejected: %v", err)
	}
}
