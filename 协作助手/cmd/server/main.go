package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gin-mall/internal/account"
	"gin-mall/internal/config"
	"gin-mall/internal/httpapi"
	"gin-mall/internal/infra"
)

func main() {
	configPath := flag.String("config", "", "YAML 配置文件路径（必填）")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(*configPath, logger); err != nil {
		logger.Error("服务启动或运行失败", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	if configPath == "" {
		return fmt.Errorf("请用 -config 指定 YAML 配置文件路径")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := infra.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	redisClient, err := infra.OpenRedis(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer redisClient.Close()
	if err := account.EnsureSchema(ctx, db); err != nil {
		return err
	}
	accounts := account.NewService(db, redisClient, cfg.Session.IdleTTL, cfg.Session.MaxLifetime)

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Server.Port))
	server := &http.Server{
		Addr:              address,
		Handler:           httpapi.NewRouter(db, accounts, cfg.Session.CookieSecure, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
	stop, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	logger.Info("商城服务启动", "address", address, "database", cfg.MySQL.Database)
	select {
	case err := <-serverErrors:
		return err
	case <-stop.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		// 先结束 HTTP 处理，再由 defer 关闭 MySQL 和 Redis 客户端。
		return server.Shutdown(shutdownCtx)
	}
}
