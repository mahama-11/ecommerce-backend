package app

import (
	"context"
	"fmt"
	"strings"

	"ecommerce-service/internal/config"
	accessmodule "ecommerce-service/internal/modules/access"
	auditmodule "ecommerce-service/internal/modules/audit"
	authmodule "ecommerce-service/internal/modules/auth"
	"ecommerce-service/internal/modules/authz"
	billingmodule "ecommerce-service/internal/modules/billing"
	commercialmodule "ecommerce-service/internal/modules/commercial"
	commissionmodule "ecommerce-service/internal/modules/commission"
	imageruntimemodule "ecommerce-service/internal/modules/imageruntime"
	productcoremodule "ecommerce-service/internal/modules/productcore"
	promotionmodule "ecommerce-service/internal/modules/promotion"
	promptcentermodule "ecommerce-service/internal/modules/promptcenter"
	templatecentermodule "ecommerce-service/internal/modules/templatecenter"
	visualworkflowmodule "ecommerce-service/internal/modules/visualworkflow"
	walletmodule "ecommerce-service/internal/modules/wallet"
	"ecommerce-service/internal/modules/workspace"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"ecommerce-service/internal/router"
	"ecommerce-service/internal/storage"
	"ecommerce-service/internal/telemetry"
	"ecommerce-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type App struct {
	Config      config.Config
	Router      *gin.Engine
	DB          *gorm.DB
	Redis       *redis.Client
	Shutdown    func(context.Context) error
	Middlewares struct{ Optional gin.HandlerFunc }
}

func New(configFile string) (*App, error) {
	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := validateProductionSecrets(*cfg); err != nil {
		return nil, err
	}
	logger.Init(cfg.LogLevel, cfg.Monitoring.Tracing.ServiceName)
	shutdownTracing, err := telemetry.InitTracing(cfg.Monitoring.Tracing)
	if err != nil {
		return nil, fmt.Errorf("init tracing: %w", err)
	}
	db, err := storage.InitDB(cfg.Database, cfg.GinMode)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}
	redisClient, err := storage.InitRedis(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("init redis: %w", err)
	}
	platformClient := platform.New(cfg.Platform)
	userRepo := repository.NewUserRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	imageRuntimeRepo := repository.NewImageRuntimeRepository(db)
	workspaceRepo := repository.NewWorkspaceRepository(db)
	templateCenterRepo := repository.NewTemplateCenterRepository(db)
	promptCenterRepo := repository.NewPromptCenterRepository(db)
	commercialRepo := repository.NewCommercialRepository(db)
	productcenterRepo := repository.NewProductCenterRepository(db)
	visualWorkflowRepo := repository.NewVisualWorkflowRepository(db)
	auditService := auditmodule.NewService(auditRepo)
	imageRuntimeService := imageruntimemodule.NewService(imageRuntimeRepo, commercialRepo, templateCenterRepo, productcenterRepo, auditService, platformClient, cfg.App).WithPromptRepository(promptCenterRepo)
	authzService := authz.NewService(platformClient)
	promotionService := promotionmodule.NewService(platformClient, commercialRepo, cfg.App)
	walletService := walletmodule.NewService(platformClient, commercialRepo, cfg.App)
	commissionService := commissionmodule.NewService(platformClient, cfg.App)
	billingService := billingmodule.NewService(platformClient, commercialRepo, cfg.App)
	commercialService := commercialmodule.NewService(platformClient, commercialRepo, cfg.App)
	authService := authmodule.NewService(platformClient, userRepo, authzService, promotionService, cfg.App)
	workspaceService := workspace.NewService(workspaceRepo, redisClient)
	templateCenterService := templatecentermodule.NewService(templateCenterRepo, auditService, platformClient)
	promptCenterService := promptcentermodule.NewService(promptCenterRepo, templateCenterRepo, imageRuntimeRepo, productcenterRepo, cfg.App)
	productcoreService := productcoremodule.NewService(productcenterRepo, imageRuntimeRepo, platformClient)
	visualWorkflowService := visualworkflowmodule.NewService(visualWorkflowRepo, productcenterRepo, imageRuntimeRepo).WithPromptRepository(promptCenterRepo).WithPromptSnapshotCreator(promptCenterService).WithWorkspaceRepository(workspaceRepo).WithRuntimeOrchestrator(platformClient)
	accessHandler := accessmodule.NewHandler(authzService)
	authHandler := authmodule.NewHandler(authService, auditService)
	billingHandler := billingmodule.NewHandler(billingService)
	commissionHandler := commissionmodule.NewHandler(commissionService, auditService)
	commercialHandler := commercialmodule.NewHandler(commercialService)
	imageRuntimeHandler := imageruntimemodule.NewHandler(imageRuntimeService).WithVisualWorkflowService(visualWorkflowService)
	promotionHandler := promotionmodule.NewHandler(promotionService, auditService)
	workspaceHandler := workspace.NewHandler(workspaceService, auditService)
	auditHandler := auditmodule.NewHandler(auditService)
	templateCenterHandler := templatecentermodule.NewHandler(templateCenterService)
	promptCenterHandler := promptcentermodule.NewHandler(promptCenterService)
	walletHandler := walletmodule.NewHandler(walletService)
	productcoreHandler := productcoremodule.NewHandler(productcoreService)
	visualWorkflowHandler := visualworkflowmodule.NewHandler(visualWorkflowService)

	if err := promotionService.Bootstrap(); err != nil {
		return nil, fmt.Errorf("bootstrap ecommerce promotion: %w", err)
	}

	if err := templateCenterService.SeedPresetCatalog(); err != nil {
		return nil, fmt.Errorf("seed template center catalog: %w", err)
	}

	app := &App{Config: *cfg, DB: db, Redis: redisClient}
	app.Router = router.New(*cfg, platformClient, db, redisClient, authHandler, accessHandler, imageRuntimeHandler, workspaceHandler, auditHandler, templateCenterHandler, promptCenterHandler, walletHandler, promotionHandler, commissionHandler, billingHandler, commercialHandler, productcoreHandler, visualWorkflowHandler)
	app.Shutdown = func(ctx context.Context) error {
		if shutdownTracing != nil {
			return shutdownTracing(ctx)
		}
		return nil
	}
	return app, nil
}

func validateProductionSecrets(cfg config.Config) error {
	if !strings.EqualFold(cfg.GinMode, gin.ReleaseMode) {
		return nil
	}
	weak := map[string]struct{}{
		"":                                   {},
		"ecommerce-dev-secret":               {},
		"ecommerce-encryption-key-change-me": {},
		"ecommerce-service-secret":           {},
		"platform-internal-secret":           {},
		"platform-dev-secret":                {},
	}
	checks := map[string]string{
		"security.jwt_secret":              cfg.Security.JWTSecret,
		"security.encryption_key":          cfg.Security.EncryptionKey,
		"security.service_secret_key":      cfg.Security.ServiceSecretKey,
		"platform.internal_service_secret": cfg.Platform.InternalServiceSecret,
		"platform.jwt_secret":              cfg.Platform.JWTSecret,
	}
	for name, value := range checks {
		trimmed := strings.TrimSpace(value)
		if _, ok := weak[trimmed]; ok || len(trimmed) < 16 {
			return fmt.Errorf("production config rejected weak/default secret: %s", name)
		}
	}
	return nil
}
