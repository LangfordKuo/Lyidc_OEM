// Package store 封装阶段 1 的数据库访问（GORM + MySQL 5.7）。
// 上层（router）只依赖本包的方法与哨兵错误，便于把持久化细节集中在一处。
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// 持久化层哨兵错误，router 据此映射业务错误码。
var (
	// ErrUnavailable 表示数据库连接未初始化（服务启动时连接失败）。
	ErrUnavailable = errors.New("数据库连接未初始化")
	// ErrNotFound 表示记录不存在（对应 code=404 或 401）。
	ErrNotFound = errors.New("记录不存在")
	// ErrUsernameTaken 表示 username 唯一键冲突（对应 code=409）。
	ErrUsernameTaken = errors.New("用户名已被占用")
	// ErrEmailTaken 表示 email 唯一键冲突（对应 code=409）。
	ErrEmailTaken = errors.New("邮箱已被占用")
	// ErrDuplicate 表示其它唯一键冲突（对应 code=409）。
	ErrDuplicate = errors.New("唯一键冲突")
	// ErrCouponCodeTaken 表示优惠码 code 唯一键冲突（大小写不敏感，对应 code=409）。
	ErrCouponCodeTaken = errors.New("优惠码已存在")
	// ErrTradeNoTaken 表示本地单号（orders.trade_no / recharges.trade_no）唯一键冲突，
	// 调用方应重新生成单号并重试。
	ErrTradeNoTaken = errors.New("本地单号已存在")
	// ErrInsufficientBalance 表示余额不足以支付该订单。
	ErrInsufficientBalance = errors.New("余额不足")
	// ErrStateConflict 表示记录当前状态不允许该操作（如取消已支付订单）；
	// 调用方应回读记录状态给出具体提示。
	ErrStateConflict = errors.New("记录状态不允许该操作")
)

// mysqlDuplicateEntry 是 MySQL 唯一键冲突错误码。
const mysqlDuplicateEntry = 1062

// 唯一索引名（见 backend/migrations/0002、0003、0005、0006）。
const (
	indexMembersUsername  = "uk_members_username"
	indexMembersEmail     = "uk_members_email"
	indexAdminsUsername   = "uk_admins_username"
	indexCouponsCode      = "uk_coupons_code"
	indexOrdersTradeNo    = "uk_orders_trade_no"
	indexRechargesTradeNo = "uk_recharges_trade_no"
	indexTicketsTradeNo   = "uk_tickets_trade_no"
)

// Store 是阶段 1 的数据访问入口。
type Store struct {
	db *gorm.DB
}

// New 构造 Store；db 允许为 nil（服务启动时数据库不可用），此时所有方法返回 ErrUnavailable。
func New(db *gorm.DB) *Store { return &Store{db: db} }

// session 返回带上下文的 GORM 句柄；数据库不可用时返回 ErrUnavailable。
func (s *Store) session(ctx context.Context) (*gorm.DB, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	return s.db.WithContext(ctx), nil
}

// mapDuplicateError 把 MySQL 1062 唯一键冲突翻译成带业务语义的哨兵错误。
func mapDuplicateError(err error) error {
	var mysqlErr *gomysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != mysqlDuplicateEntry {
		return err
	}
	switch {
	case strings.Contains(mysqlErr.Message, indexMembersUsername):
		return ErrUsernameTaken
	case strings.Contains(mysqlErr.Message, indexMembersEmail):
		return ErrEmailTaken
	case strings.Contains(mysqlErr.Message, indexAdminsUsername):
		return ErrUsernameTaken
	case strings.Contains(mysqlErr.Message, indexCouponsCode):
		return ErrCouponCodeTaken
	case strings.Contains(mysqlErr.Message, indexOrdersTradeNo),
		strings.Contains(mysqlErr.Message, indexRechargesTradeNo),
		strings.Contains(mysqlErr.Message, indexTicketsTradeNo):
		return ErrTradeNoTaken
	default:
		return fmt.Errorf("%w: %s", ErrDuplicate, mysqlErr.Message)
	}
}

// escapeLike 转义 LIKE 模式中的通配符，使用户输入按字面量匹配
// （MySQL 默认转义符为反斜杠）。
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// containsKeyword 生成 "%关键字%" 形式的 LIKE 模式。
func containsKeyword(keyword string) string {
	return "%" + escapeLike(keyword) + "%"
}

// notFoundIfNeeded 把 GORM 的 ErrRecordNotFound 翻译为 ErrNotFound。
func notFoundIfNeeded(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
