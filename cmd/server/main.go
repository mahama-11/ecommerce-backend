package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"ecommerce-service/internal/app"
)

// @title Agent Ecommerce Backend API
// @version 0.1.0
// @description Frontend-facing APIs for Agent Ecommerce auth, session, access, and workspace orchestration.
// @description Shared identity, org, and wallet truth remain in v-platform-backend.
// @BasePath /
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	configFile := flag.String("config", getenv("ECOMMERCE_CONFIG_FILE", "config.local"), "config file name without .yaml suffix")
	flag.Parse()

	application, err := app.New(*configFile)
	if err != nil {
		log.Fatalf("failed to initialize ecommerce service: %v", err)
	}
	if application.Shutdown != nil {
		defer func() {
			_ = application.Shutdown(context.Background())
		}()
	}

	addr := fmt.Sprintf("%s:%d", application.Config.Host, application.Config.Port)
	if err := application.Router.Run(addr); err != nil {
		log.Fatalf("ecommerce service exited with error: %v", err)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
