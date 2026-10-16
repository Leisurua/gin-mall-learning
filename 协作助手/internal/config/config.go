package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	MySQL   MySQLConfig   `yaml:"mysql"`
	Redis   RedisConfig   `yaml:"redis"`
	Session SessionConfig `yaml:"session"`
}

type RedisConfig struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type SessionConfig struct {
	IdleTTL      time.Duration `yaml:"idle_ttl"`
	MaxLifetime  time.Duration `yaml:"max_lifetime"`
	CookieSecure bool          `yaml:"cookie_secure"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

func Load(path string) (Config, error) {
	var cfg Config
	file, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("打开配置文件: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("解析 YAML 配置: %w", err)
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return cfg, fmt.Errorf("server.port 必须在 1 到 65535 之间")
	}
	if cfg.MySQL.Port < 1 || cfg.MySQL.Port > 65535 {
		return cfg, fmt.Errorf("mysql.port 必须在 1 到 65535 之间")
	}
	if cfg.MySQL.Host == "" || cfg.MySQL.User == "" || cfg.MySQL.Database == "" {
		return cfg, fmt.Errorf("mysql.host、mysql.user 和 mysql.database 不能为空")
	}
	if cfg.Redis.Address == "" || cfg.Redis.DB < 0 {
		return cfg, fmt.Errorf("redis.address 不能为空，redis.db 不能为负数")
	}
	if cfg.Session.IdleTTL < time.Second || cfg.Session.MaxLifetime < time.Second ||
		cfg.Session.IdleTTL%time.Second != 0 || cfg.Session.MaxLifetime%time.Second != 0 {
		return cfg, fmt.Errorf("session.idle_ttl 和 session.max_lifetime 必须是正的整秒时长")
	}
	return cfg, nil
}
