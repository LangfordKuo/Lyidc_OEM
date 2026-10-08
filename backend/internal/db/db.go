// Package db 负责 MySQL 连接与连接池管理。
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
)

const pingTimeout = 5 * time.Second

// NormalizeDSN 返回应用层使用的 DSN：强制 parseTime=true 且 loc=UTC。
//
// 目的：members/admins 的 DATETIME 列统一按 UTC 语义读写（见 docs/api-contract.md 第 1.2 节），
// 与开发机本地时区、部署环境 TZ 解耦；接口层再按 RFC3339（UTC）输出。
func NormalizeDSN(dsn string) (string, error) {
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("解析 DSN 失败: %w", err)
	}
	parsed.ParseTime = true
	parsed.Loc = time.UTC
	return parsed.FormatDSN(), nil
}

// Open 建立 MySQL 连接池并返回 GORM 句柄；失败时返回 nil 与具体错误。
func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsn, err := NormalizeDSN(cfg.DSN)
	if err != nil {
		return nil, err
	}

	gdb, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
		// 时间列统一写 UTC。
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("MySQL Ping 失败: %w", err)
	}
	return gdb, nil
}

// Close 关闭 GORM 底层连接池。
func Close(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return fmt.Errorf("获取底层连接池失败: %w", err)
	}
	return sqlDB.Close()
}

// PingFunc 返回探测数据库连通性的函数，供 /api/v1/health 使用。
// gdb 为 nil（连接未建立）时探测恒失败。
func PingFunc(gdb *gorm.DB) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if gdb == nil {
			return errors.New("数据库连接未初始化")
		}
		sqlDB, err := gdb.DB()
		if err != nil {
			return fmt.Errorf("获取底层连接池失败: %w", err)
		}
		return sqlDB.PingContext(ctx)
	}
}
