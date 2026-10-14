package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// 操作日志各字段的字符上限（与迁移 0019 的列宽一致，按字符截断）。
const (
	maxOperationLogActorNameRunes = 64
	maxOperationLogActionRunes    = 128
	maxOperationLogSummaryRunes   = 500
	maxOperationLogPathRunes      = 191
)

// clearOperationLogsBatch 是按通道清空时每批删除的行数（避免单条 DELETE 长时间持有行锁/整表锁）。
const clearOperationLogsBatch = 1000

// ErrOperationLogKeyword 表示关键字搜索的关键字不合法（单字符无法被 ngram 全文索引命中）。
var ErrOperationLogKeyword = errors.New("关键字至少 2 个字符（中文按字、英文按字母），且不能包含单字符的词")

// OperationLogInput 是写入一条操作日志的输入（Summary 由调用方保证已脱敏）。
type OperationLogInput struct {
	Channel    string
	ActorID    uint64
	ActorName  string
	Action     string
	Method     string
	Path       string
	HTTPStatus int
	Status     string
	IP         string
	DurationMS int64
	Summary    string
	// CreatedAt 为零值时由写入器在落库时填当前 UTC 时间。
	CreatedAt time.Time
}

// OperationLogFilter 是操作日志的分页查询条件（page 从 1 开始；空/零值字段表示不过滤）。
type OperationLogFilter struct {
	// Channel 为空的通道不过滤。
	Channel string
	// ActorID 非 nil 时按操作人精确过滤（含 0 = 系统任务）。
	ActorID *uint64
	// Status 为空时不过滤（success / failed）。
	Status string
	// Keyword 非空时走 FULLTEXT + ngram 关键字检索（action / summary / actor_name）。
	Keyword string
	// Start / End 为闭区间时间窗（nil 表示不限）。
	Start    *time.Time
	End      *time.Time
	Page     int
	PageSize int
}

// AppendOperationLogs 批量写入操作日志（异步写入器每次 flush 调一次，一条多行 INSERT）。
//
// 写入口径（契约 23.6）：本表**不参与任何业务事务**；写失败只返回错误由调用方记服务日志，
// 绝不影响业务请求的结果。字段按列宽截断（字符计），保证不会因超长而写失败。
func (s *Store) AppendOperationLogs(ctx context.Context, entries []OperationLogInput) error {
	if len(entries) == 0 {
		return nil
	}
	db, err := s.session(ctx)
	if err != nil {
		return err
	}

	records := make([]model.OperationLog, 0, len(entries))
	for _, in := range entries {
		createdAt := in.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		records = append(records, model.OperationLog{
			Channel:    in.Channel,
			ActorID:    in.ActorID,
			ActorName:  truncateRunes(in.ActorName, maxOperationLogActorNameRunes),
			Action:     truncateRunes(in.Action, maxOperationLogActionRunes),
			Method:     truncateRunes(in.Method, 10),
			Path:       truncateRunes(in.Path, maxOperationLogPathRunes),
			HTTPStatus: clampHTTPStatus(in.HTTPStatus),
			Status:     normalizeOperationLogStatus(in.Status, in.HTTPStatus),
			IP:         truncateRunes(in.IP, 45),
			DurationMS: clampDurationMS(in.DurationMS),
			Summary:    truncateRunes(in.Summary, maxOperationLogSummaryRunes),
			CreatedAt:  createdAt,
		})
	}
	return db.Create(&records).Error
}

// normalizeOperationLogStatus 把结果归一为枚举内的合法值：非法/缺省时按 HTTP 状态码推导
// （2xx/3xx 成功、其余失败），避免直接落空串被 MySQL 严格模式拒绝。
func normalizeOperationLogStatus(status string, httpStatus int) string {
	if status == model.OplogStatusSuccess || status == model.OplogStatusFailed {
		return status
	}
	if httpStatus >= 200 && httpStatus < 400 {
		return model.OplogStatusSuccess
	}
	if httpStatus == 0 {
		return model.OplogStatusSuccess
	}
	return model.OplogStatusFailed
}

// ListOperationLogs 分页查询操作日志（新记录在前），返回当页与总数。
func (s *Store) ListOperationLogs(ctx context.Context, filter OperationLogFilter) ([]model.OperationLog, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.OperationLog{})
	if filter.Channel != "" {
		query = query.Where("channel = ?", filter.Channel)
	}
	if filter.ActorID != nil {
		query = query.Where("actor_id = ?", *filter.ActorID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Start != nil {
		query = query.Where("created_at >= ?", filter.Start.UTC())
	}
	if filter.End != nil {
		query = query.Where("created_at <= ?", filter.End.UTC())
	}
	if filter.Keyword != "" {
		expr, err := OperationLogKeywordExpr(filter.Keyword)
		if err != nil {
			return nil, 0, err
		}
		// MATCH 的列必须与迁移 0019 的 ft_operation_logs_search 列序完全一致。
		query = query.Where("MATCH(action, summary, actor_name) AGAINST (? IN BOOLEAN MODE)", expr)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.OperationLog, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ClearOperationLogsByChannel 清空单个通道的操作日志，返回删除条数。
//
// 分批 DELETE（每批 clearOperationLogsBatch 行）：日志表可能很大，单条 DELETE 会长时间持有
// 行锁与 undo（契约 23.6 的清理口径）；每批独立提交，失败即中止并返回已删除条数。
func (s *Store) ClearOperationLogsByChannel(ctx context.Context, channel string) (int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return 0, err
	}

	var deleted int64
	for {
		result := db.Exec("DELETE FROM operation_logs WHERE channel = ? LIMIT ?", channel, clearOperationLogsBatch)
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += result.RowsAffected
		if result.RowsAffected < clearOperationLogsBatch {
			return deleted, nil
		}
	}
}

// TruncateOperationLogs 清空全部操作日志（TRUNCATE，自增 ID 归零），返回清空前的条数。
//
// 取舍：清空「全部」走 TRUNCATE——大表上比全表 DELETE 快几个数量级，代价是自增 ID 重新从 1 开始
// （契约 23.6 已写明）。因此它**不适用于按通道清空**（会连未选中通道一起清掉）。
func (s *Store) TruncateOperationLogs(ctx context.Context) (int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return 0, err
	}

	var before int64
	if err := db.Model(&model.OperationLog{}).Count(&before).Error; err != nil {
		return 0, err
	}
	if err := db.Exec("TRUNCATE TABLE operation_logs").Error; err != nil {
		return before, err
	}
	return before, nil
}

// OperationLogKeywordExpr 把用户关键字编译为 MySQL 布尔模式的 ngram 全文检索表达式。
//
// 口径（契约 23.5）：
//   - 关键字按空白拆成多个词，每个词**按短语整体匹配**（ngram 解析器会先按 ngram_token_size=2
//     切分，短语查询要求这些 ngram 连续出现，因此「上游设置」能命中「修改上游设置」，
//     但不会被「设置」单独命中而放大结果）；
//   - 多个词之间是 AND（每词前缀 `+`），即「都能命中才返回」；
//   - 单个词短于 2 个字符时无法被 ngram 索引（ngram_token_size=2），返回 ErrOperationLogKeyword，
//     由接口层以 40001 明确拒绝——**不**退回全表 LIKE（那会随表增长拖慢站点）。
func OperationLogKeywordExpr(keyword string) (string, error) {
	terms := strings.Fields(keyword)
	if len(terms) == 0 {
		return "", ErrOperationLogKeyword
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		if utf8.RuneCountInString(term) < 2 {
			return "", ErrOperationLogKeyword
		}
		parts = append(parts, "+"+quoteBooleanPhrase(term))
	}
	return strings.Join(parts, " "), nil
}

// quoteBooleanPhrase 把单个词编成布尔模式的短语（双引号内转义 `"` 与 `\`，
// 其余布尔运算符在引号内都是字面量）。
func quoteBooleanPhrase(term string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(term) + `"`
}

// clampHTTPStatus 把 HTTP 状态码限制在 SMALLINT UNSIGNED 范围内（负数与越界值归 0）。
func clampHTTPStatus(status int) uint16 {
	if status < 0 || status > 65535 {
		return 0
	}
	return uint16(status)
}

// clampDurationMS 把耗时限制在 INT UNSIGNED 范围内（负数归 0）。
func clampDurationMS(duration int64) uint64 {
	if duration < 0 {
		return 0
	}
	if duration > 4294967295 {
		return 4294967295
	}
	return uint64(duration)
}
