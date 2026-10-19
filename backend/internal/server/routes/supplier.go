package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterSupplierRoutes registers the fork-local supplier console and its admin API.
// The middleware chains mirror RegisterUserRoutes and RegisterAdminRoutes.
func RegisterSupplierRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	adminAuth middleware.AdminAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	s := h.Supplier

	console := v1.Group("/supplier")
	console.Use(gin.HandlerFunc(jwtAuth))
	console.Use(middleware.BackendModeUserGuard(settingService))
	console.Use(panelRateLimiter.Global())
	console.Use(gin.HandlerFunc(auditLog))
	console.Use(s.RequireSupplier)
	{
		console.GET("/me", s.Me)

		console.GET("/proxies", s.ListProxies)
		console.POST("/proxies", s.CreateProxy)
		console.PUT("/proxies/:id", s.UpdateProxy)
		console.DELETE("/proxies/:id", s.DeleteProxy)
		console.POST("/proxies/:id/test", s.TestProxy)

		console.GET("/accounts", s.ListAccounts)
		console.POST("/accounts/auth-url", s.GenerateAuthURL)
		console.POST("/accounts/exchange-code", s.ExchangeCode)
		console.POST("/accounts/cookie-auth", s.CookieAuth)
		console.PUT("/accounts/:id", s.UpdateAccount)
		console.DELETE("/accounts/:id", s.DeleteAccount)

		console.GET("/usage/summary", panelRateLimiter.Heavy(), s.UsageSummary)
		console.GET("/usage/daily", panelRateLimiter.Heavy(), s.UsageDaily)
		console.GET("/usage/accounts", panelRateLimiter.Heavy(), s.UsageByAccount)

		console.GET("/settlements", s.ListSettlements)
		console.GET("/settlements/:id", s.GetSettlement)
	}

	admin := v1.Group("/admin")
	admin.Use(gin.HandlerFunc(adminAuth))
	admin.Use(panelRateLimiter.Global())
	admin.Use(gin.HandlerFunc(auditLog))
	admin.Use(middleware.AdminComplianceGuard(settingService))
	{
		suppliers := admin.Group("/suppliers")
		suppliers.GET("", s.AdminListSuppliers)
		suppliers.POST("", s.AdminCreateSupplier)
		suppliers.GET("/:id", s.AdminGetSupplier)
		suppliers.PUT("/:id", s.AdminUpdateSupplier)
		suppliers.POST("/:id/attach-accounts", s.AdminAttachAccounts)
		suppliers.GET("/:id/usage/summary", s.AdminUsageSummary)
		suppliers.GET("/:id/usage/daily", s.AdminUsageDaily)
		suppliers.GET("/:id/usage/accounts", s.AdminUsageByAccount)

		accounts := admin.Group("/supplier-accounts")
		accounts.GET("", s.AdminListAccounts)
		accounts.POST("/:id/review", s.AdminReviewAccount)

		settlements := admin.Group("/supplier-settlements")
		settlements.GET("", s.AdminListSettlements)
		settlements.POST("", s.AdminCreateSettlement)
		settlements.POST("/preview", s.AdminPreviewSettlement)
		settlements.GET("/:id", s.AdminGetSettlement)
		settlements.POST("/:id/confirm", s.AdminConfirmSettlement)
		settlements.POST("/:id/mark-paid", s.AdminMarkSettlementPaid)
		settlements.POST("/:id/void", s.AdminVoidSettlement)
	}
}
