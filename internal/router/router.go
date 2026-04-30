package router

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	accessmodule "ecommerce-service/internal/modules/access"
	auditmodule "ecommerce-service/internal/modules/audit"
	authmodule "ecommerce-service/internal/modules/auth"
	billingmodule "ecommerce-service/internal/modules/billing"
	commercialmodule "ecommerce-service/internal/modules/commercial"
	commissionmodule "ecommerce-service/internal/modules/commission"
	imageruntimemodule "ecommerce-service/internal/modules/imageruntime"
	productcoremodule "ecommerce-service/internal/modules/productcore"
	promotionmodule "ecommerce-service/internal/modules/promotion"
	templatecentermodule "ecommerce-service/internal/modules/templatecenter"
	walletmodule "ecommerce-service/internal/modules/wallet"
	"ecommerce-service/internal/modules/workspace"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/storage"
	"ecommerce-service/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"
)

func New(cfg config.Config, platformClient *platform.Client, db *gorm.DB, redisClient *redis.Client, authHandler *authmodule.Handler, accessHandler *accessmodule.Handler, imageRuntimeHandler *imageruntimemodule.Handler, workspaceHandler *workspace.Handler, auditHandler *auditmodule.Handler, templateCenterHandler *templatecentermodule.Handler, walletHandler *walletmodule.Handler, promotionHandler *promotionmodule.Handler, commissionHandler *commissionmodule.Handler, billingHandler *billingmodule.Handler, commercialHandler *commercialmodule.Handler, productcoreHandler *productcoremodule.Handler) *gin.Engine {
	gin.SetMode(cfg.GinMode)
	r := gin.New()
	serviceName := cfg.Monitoring.Tracing.ServiceName
	if serviceName == "" {
		serviceName = "ecommerce-service"
	}
	r.Use(otelgin.Middleware(serviceName))
	r.Use(middleware.RequestContext(), middleware.Metrics(cfg.Monitoring.Metrics.Namespace, cfg.Monitoring.Metrics.Subsystem, cfg.Monitoring.Metrics.HistogramBuckets), middleware.AccessLog(), gin.Recovery(), cors(cfg.App.FrontendBaseURL))

	healthHandler := func(c *gin.Context) {
		response.JSONSuccess(c, gin.H{"service": "v-ecommerce-backend", "status": "ok", "platform_base_url": platformClient.BaseURL()})
	}
	readyHandler := func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		checks := gin.H{"database": "ok", "redis": "disabled"}
		if err := storage.PingDB(ctx, db); err != nil {
			checks["database"] = err.Error()
			response.JSONErrorWithStatusSemantic(c, response.CodeServiceUnavailable, "database readiness failed", "DATABASE_NOT_READY", "Check database connectivity.", http.StatusServiceUnavailable)
			return
		}
		if cfg.Redis.Enabled {
			if err := storage.PingRedis(ctx, redisClient); err != nil {
				checks["redis"] = err.Error()
				response.JSONErrorWithStatusSemantic(c, response.CodeServiceUnavailable, "redis readiness failed", "REDIS_NOT_READY", "Check redis connectivity.", http.StatusServiceUnavailable)
				return
			}
			checks["redis"] = "ok"
		}
		response.JSONSuccess(c, gin.H{"service": "v-ecommerce-backend", "status": "ready", "checks": checks})
	}

	r.GET("/healthz", healthHandler)
	r.HEAD("/healthz", healthHandler)
	r.GET("/readyz", readyHandler)
	r.HEAD("/readyz", readyHandler)
	if cfg.Monitoring.Metrics.Enabled {
		r.GET(cfg.Monitoring.Metrics.Path, middleware.MetricsHandler(cfg.Monitoring.Metrics.Namespace, cfg.Monitoring.Metrics.Subsystem, cfg.Monitoring.Metrics.HistogramBuckets))
	}

	v1 := r.Group("/api/v1/ecommerce")
	{
		authAPI := v1.Group("/auth")
		{
			authAPI.POST("/register", authHandler.Register)
			authAPI.POST("/login", authHandler.Login)
			authAPI.GET("/session", middleware.PlatformJWTAuth(cfg.Platform.JWTSecret), authHandler.Session)
		}
		v1.GET("/promotions/codes/:code/resolve", promotionHandler.ResolveCode)
		v1.GET("/health", func(c *gin.Context) {
			response.JSONSuccess(c, gin.H{"service": "ecommerce-api", "status": "ok", "product": cfg.App.ProductName})
		})
		v1.GET("/access/me", middleware.PlatformJWTAuth(cfg.Platform.JWTSecret), accessHandler.Me)

		protected := v1.Group("")
		protected.Use(middleware.PlatformJWTAuth(cfg.Platform.JWTSecret))
		{
			protected.GET("/user/audit-history", auditHandler.History)
			protected.GET("/wallet/summary", walletHandler.Summary)
			protected.GET("/wallet/history", walletHandler.History)
			protected.GET("/commercial/offerings", commercialHandler.GetOfferings)
			protected.POST("/commercial/orders", commercialHandler.CreateOrder)
			protected.GET("/commercial/orders", commercialHandler.ListOrders)
			protected.GET("/commercial/orders/:orderID", commercialHandler.GetOrder)
			protected.POST("/commercial/orders/:orderID/confirm-payment", commercialHandler.ConfirmOrderPayment)
			protected.GET("/billing/summary", billingHandler.Summary)
			protected.GET("/billing/charges", billingHandler.ListCharges)
			protected.GET("/promotions/programs", promotionHandler.ListPrograms)
			protected.GET("/promotions/me/overview", promotionHandler.Overview)
			protected.GET("/promotions/me/codes", promotionHandler.ListCodes)
			protected.POST("/promotions/me/codes/ensure", promotionHandler.EnsureCode)
			protected.POST("/promotions/me/codes", promotionHandler.CreateCode)
			protected.GET("/promotions/me/conversions", promotionHandler.ListConversions)
			protected.GET("/commissions/me/overview", commissionHandler.Overview)
			protected.GET("/commissions/me/referrals", commissionHandler.ListReferralCommissions)
			protected.POST("/commissions/me/referrals/redeem", commissionHandler.Redeem)
			protected.GET("/commissions/me/channel/overview", commissionHandler.ChannelOverview)
			protected.GET("/commissions/me/channel/bindings", commissionHandler.ChannelBindings)
			protected.GET("/commissions/me/channel/commissions", commissionHandler.ChannelCommissions)
			protected.GET("/commissions/me/channel/settlements", commissionHandler.ChannelSettlements)
			protected.POST("/assets/source", imageRuntimeHandler.RegisterSourceAsset)
			protected.GET("/assets/:assetID/content", imageRuntimeHandler.GetAssetContent)
			protected.GET("/image-jobs", imageRuntimeHandler.ListJobs)
			protected.POST("/image-jobs", imageRuntimeHandler.CreateImageJob)
			protected.GET("/image-jobs/:jobID", imageRuntimeHandler.GetJob)
			protected.POST("/image-jobs/:jobID/cancel", imageRuntimeHandler.CancelJob)

			// Product Center APIs
			protected.GET("/products", productcoreHandler.ListProducts)
			protected.POST("/products", productcoreHandler.CreateProduct)
			protected.GET("/products/:product_id", productcoreHandler.GetProduct)
			protected.PATCH("/products/:product_id", productcoreHandler.UpdateProduct)
			protected.PATCH("/products/:product_id/status", productcoreHandler.UpdateProductStatus)
			protected.DELETE("/products/:product_id", productcoreHandler.DeleteProduct)
			protected.GET("/products/:product_id/assets", productcoreHandler.ListProductAssets)
			protected.POST("/products/:product_id/assets", productcoreHandler.AddProductAsset)
			protected.PATCH("/products/:product_id/assets/:asset_relation_id", productcoreHandler.UpdateProductAsset)
			protected.DELETE("/products/:product_id/assets/:asset_relation_id", productcoreHandler.DeleteProductAsset)
			// Listing Version APIs
			protected.GET("/products/:product_id/listing-versions", productcoreHandler.ListListingVersions)
			protected.POST("/products/:product_id/listing-versions", productcoreHandler.CreateListingVersion)
			protected.POST("/products/listing-versions/batch", productcoreHandler.BatchCreateListingVersions)
			protected.POST("/products/:product_id/listing-versions/adopt", productcoreHandler.AdoptListingVersion)
			protected.POST("/products/listing-versions/batch-adopt", productcoreHandler.BatchAdoptListingVersions)
			protected.PATCH("/products/:product_id/listing-versions/:version_id", productcoreHandler.UpdateListingVersion)
			protected.DELETE("/products/:product_id/listing-versions/:version_id", productcoreHandler.DeleteListingVersion)
			// Profit Snapshot APIs
			protected.GET("/products/:product_id/profit-snapshots", productcoreHandler.ListProfitSnapshots)
			protected.POST("/products/:product_id/profit-snapshots/calculate", productcoreHandler.CalculateProfit)
			// Export Task APIs
			protected.GET("/products/:product_id/export-tasks", productcoreHandler.ListExportTasks)
			protected.POST("/products/:product_id/export-tasks", productcoreHandler.CreateExportTask)
			protected.PATCH("/products/:product_id/export-tasks/status", productcoreHandler.UpdateExportTaskStatus)
			protected.GET("/downloads", productcoreHandler.ListDownloads)
			protected.GET("/downloads/:download_id/content", productcoreHandler.DownloadContent)
		}

		workspaceGroup := v1.Group("")
		workspaceGroup.Use(middleware.OptionalPlatformJWTAuth(cfg.Platform.JWTSecret))
		{
			workspaceGroup.GET("/templates/saved", workspaceHandler.ListSavedTemplates)
			workspaceGroup.POST("/templates/saved", workspaceHandler.SaveTemplate)
			workspaceGroup.GET("/workflow/events", workspaceHandler.ListWorkflowEvents)
			workspaceGroup.POST("/workflow/events", workspaceHandler.SaveWorkflowEvent)
			workspaceGroup.GET("/workflow/template-bridges", workspaceHandler.ListTemplateBridges)
			workspaceGroup.POST("/workflow/template-bridges", workspaceHandler.SaveTemplateBridge)
			workspaceGroup.GET("/assets/linked-designs", workspaceHandler.ListLinkedAssets)
			workspaceGroup.POST("/assets/linked-designs", workspaceHandler.SaveLinkedAsset)
			workspaceGroup.GET("/deliveries/linked", workspaceHandler.ListLinkedDeliveries)
			workspaceGroup.POST("/deliveries/linked", workspaceHandler.SaveLinkedDelivery)
		}

		templateCatalog := v1.Group("/template-center")
		templateCatalog.Use(middleware.OptionalPlatformJWTAuth(cfg.Platform.JWTSecret))
		{
			templateCatalog.GET("/catalog", templateCenterHandler.ListCatalog)
			templateCatalog.GET("/catalog/facets", templateCenterHandler.Facets)
			templateCatalog.GET("/catalog/recommendations", templateCenterHandler.Recommendations)
			templateCatalog.GET("/catalog/:templateId", templateCenterHandler.Detail)
			templateCatalog.GET("/assets/preview", templateCenterHandler.PreviewAsset)
		}

		templateProtected := v1.Group("/template-center")
		templateProtected.Use(middleware.PlatformJWTAuth(cfg.Platform.JWTSecret))
		{
			templateProtected.GET("/instances", templateCenterHandler.Instances)
			templateProtected.GET("/favorites", templateCenterHandler.Favorites)
			templateProtected.POST("/catalog/:templateId/favorite", templateCenterHandler.AddFavorite)
			templateProtected.DELETE("/catalog/:templateId/favorite", templateCenterHandler.RemoveFavorite)
			templateProtected.POST("/catalog/:templateId/copy", templateCenterHandler.CopyToMyTemplates)
			templateProtected.POST("/catalog/:templateId/use", templateCenterHandler.Use)
		}
	}

	internal := r.Group("/internal/v1/ecommerce")
	internal.Use(middleware.RequireInternalService(cfg.Security.ServiceSecretKey))
	{
		internal.GET("/health", healthHandler)
		internal.GET("/ready", readyHandler)
		internal.POST("/commercial/billing/charges", billingHandler.RecordCharge)
		internal.POST("/commercial/billing/charges/:recordID/refunds", billingHandler.RefundCharge)
		internal.POST("/commercial/outbox/replay", billingHandler.ReplayOutbox)
		internal.POST("/jobs/:jobID/runtime", imageRuntimeHandler.InternalUpdateJobRuntime)
		internal.POST("/jobs/:jobID/results", imageRuntimeHandler.InternalRecordJobResults)
	}
	return r
}

func cors(allowedOrigin string) gin.HandlerFunc {
	allowedOrigin = strings.TrimSpace(allowedOrigin)
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && (allowedOrigin == "" || origin == allowedOrigin) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
		}
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Trace-ID")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func parsePagination(c *gin.Context) (limit, offset int) {
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
