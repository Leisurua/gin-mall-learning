package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"gin-mall/internal/account"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB, accounts *account.Service, cookieSecure bool, logger *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(requestLog(logger), gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Error("请求处理发生 panic", "panic", recovered)
		failure(c, http.StatusInternalServerError, "INTERNAL_ERROR", "请求处理失败")
	}))

	handler := &productHandler{db: db, logger: logger}
	api := router.Group("/api")
	api.GET("/products", handler.list)
	api.GET("/products/:id", handler.detail)
	accountHTTP := &accountHandler{service: accounts, cookieSecure: cookieSecure, logger: logger}
	auth := api.Group("/auth")
	auth.POST("/register", accountHTTP.register)
	auth.POST("/login", accountHTTP.login)
	api.GET("/users/me", accountHTTP.requireLogin, accountHTTP.me)

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
