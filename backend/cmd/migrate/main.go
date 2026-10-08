// Command migrate 执行数据库结构迁移（golang-migrate，SQL 文件见 backend/migrations）。
//
// 用法：
//
//	go run ./cmd/migrate up            应用全部未执行的迁移
//	go run ./cmd/migrate down [n]      回滚 n 步（缺省 1 步）
//	go run ./cmd/migrate version       打印当前迁移版本
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/migrations"
)

const pingTimeout = 15 * time.Second

func main() {
	configPath := flag.String("config", "", "配置文件路径（缺省时按 ./config.yaml、./backend/config.yaml 顺序查找）")
	flag.Usage = usage
	flag.Parse()

	if err := run(*configPath, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "迁移失败: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("缺少子命令")
	}
	command := args[0]

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	dsn, err := multiStatementDSN(cfg.Database.DSN)
	if err != nil {
		return err
	}

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("打开数据库连接: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("连接 MySQL 失败（%s）: %w", config.MaskDSN(cfg.Database.DSN), err)
	}

	driver, err := migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	if err != nil {
		return fmt.Errorf("初始化迁移驱动: %w", err)
	}

	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("加载迁移文件: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		return fmt.Errorf("初始化迁移器: %w", err)
	}
	defer func() { _, _ = migrator.Close() }()

	switch command {
	case "up":
		return runUp(migrator)
	case "down":
		return runDown(migrator, args[1:])
	case "version":
		return runVersion(migrator)
	default:
		usage()
		return fmt.Errorf("未知子命令 %q", command)
	}
}

func runUp(migrator *migrate.Migrate) error {
	if err := migrator.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("up: 已是最新版本，无变更")
			return printVersion(migrator)
		}
		return fmt.Errorf("up: %w", err)
	}
	fmt.Println("up: 迁移执行完成")
	return printVersion(migrator)
}

func runDown(migrator *migrate.Migrate, args []string) error {
	steps := 1
	if len(args) > 0 {
		parsed, err := strconv.Atoi(args[0])
		if err != nil || parsed <= 0 {
			return fmt.Errorf("down 步数必须为正整数，收到 %q", args[0])
		}
		steps = parsed
	}

	if err := migrator.Steps(-steps); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("down: 没有可回滚的迁移")
			return printVersion(migrator)
		}
		return fmt.Errorf("down: %w", err)
	}
	fmt.Printf("down: 已回滚 %d 步\n", steps)
	return printVersion(migrator)
}

func runVersion(migrator *migrate.Migrate) error {
	return printVersion(migrator)
}

func printVersion(migrator *migrate.Migrate) error {
	version, dirty, err := migrator.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("version: 尚未执行任何迁移")
			return nil
		}
		return fmt.Errorf("version: %w", err)
	}
	fmt.Printf("version: %d (dirty=%t)\n", version, dirty)
	return nil
}

// multiStatementDSN 确保迁移连接允许一次发送多条语句（golang-migrate SQL 文件）。
func multiStatementDSN(dsn string) (string, error) {
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("解析 DSN 失败: %w", err)
	}
	parsed.MultiStatements = true
	return parsed.FormatDSN(), nil
}

func usage() {
	fmt.Fprint(os.Stderr, `用法: go run ./cmd/migrate [-config 配置文件] <子命令>

子命令:
  up            应用全部未执行的迁移
  down [n]      回滚 n 步（缺省 1 步）
  version       打印当前迁移版本
`)
}
