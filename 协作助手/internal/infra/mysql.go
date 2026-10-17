package infra

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	"gin-mall/internal/config"

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func OpenMySQL(ctx context.Context, cfg config.MySQLConfig) (*gorm.DB, error) {
	driverConfig := mysql.NewConfig()
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	driverConfig.User = cfg.User
	driverConfig.Passwd = cfg.Password
	driverConfig.DBName = cfg.Database
	driverConfig.ParseTime = true
	driverConfig.Params = map[string]string{"charset": "utf8mb4"}
	driverConfig.Timeout = 3 * time.Second

	// sql.Open 创建连接池；PingContext 才实际检查能否连接和登录。
	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("创建 MySQL 连接池: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接 MySQL: %w", err)
	}
	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: db}), &gorm.Config{
		TranslateError:       true,
		DisableAutomaticPing: true,
		// SQL 参数可能包含密码哈希，错误由调用方统一记录。
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化 GORM: %w", err)
	}
	return orm, nil
}
