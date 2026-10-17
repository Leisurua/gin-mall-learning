package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"gin-mall/internal/account"
	"gin-mall/internal/cart"
	"gin-mall/internal/catalog"
	"github.com/gin-gonic/gin"
)

func NewRouter(catalogs *catalog.Service, accounts *account.Service, carts *cart.Service, cookieSecure bool, logger *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(requestLog(logger), gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Error("请求处理发生 panic", "panic", recovered)
		failure(c, http.StatusInternalServerError, "INTERNAL_ERROR", "请求处理失败")
	}))

	handler := &productHandler{service: catalogs, logger: logger}
	api := router.Group("/api")
	api.GET("/products", handler.list)
	api.GET("/products/:id", handler.detail)
	api.GET("/categories", handler.categories)
	accountHTTP := &accountHandler{service: accounts, cookieSecure: cookieSecure, logger: logger}
	auth := api.Group("/auth")
	auth.POST("/register", accountHTTP.register)
	auth.POST("/login", accountHTTP.login)
	api.GET("/users/me", accountHTTP.requireLogin, accountHTTP.me)
	cartHTTP := &cartHandler{service: carts, logger: logger}
	myCart := api.Group("/cart/items", accountHTTP.requireLogin)
	myCart.GET("", cartHTTP.list)
	myCart.POST("", cartHTTP.add)
	myCart.PATCH("/:id", cartHTTP.update)
	myCart.DELETE("/:id", cartHTTP.remove)
	api.POST("/shops", accountHTTP.requireLogin, handler.createShop)
	shop := api.Group("/shops/me", accountHTTP.requireLogin)
	shop.GET("", handler.myShop)
	shop.GET("/products", handler.ownList)
	shop.POST("/products", handler.create)
	shop.GET("/products/:id", handler.ownDetail)
	shop.PATCH("/products/:id", handler.update)
	shop.PATCH("/products/:id/status", handler.status)
	api.POST("/seller-applications", accountHTTP.requireLogin, accountHTTP.applySeller)
	api.GET("/seller-applications/me", accountHTTP.requireLogin, accountHTTP.myApplications)
	admin := api.Group("/admin", accountHTTP.requireLogin)
	admin.GET("/seller-applications", accountHTTP.adminApplications)
	admin.PATCH("/seller-applications/:id", accountHTTP.reviewApplication)

	router.NoRoute(func(c *gin.Context) {
		failure(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "接口不存在")
	})
	router.NoMethod(func(c *gin.Context) {
		failure(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不支持")
	})
	return router
}

func requestLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("HTTP 请求",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(start),
		)
	}
}
