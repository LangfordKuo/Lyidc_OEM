package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gomysql "github.com/go-sql-driver/mysql"
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

// ErrOperationLogKeywordTimeout 表示关键字检索命中过多、超出执行超时被 MySQL 中断（契约 23.5 第 5 条）。
//
// 背景（契约 23.10 实测）：MySQL 5.7 InnoDB 的 FULLTEXT 会**物化全部命中**，`LIMIT` 截断不了、
// 加时间窗也帮不上——命中 4.3 万条要 6 秒左右，单个搜索请求会长时间占住一条数据库连接。
// 因此关键字路径的语句带 MAX_EXECUTION_TIME 提示，超时由 MySQL 中断该语句（连接不受影响），
// 这里把 MySQL 的中断错误翻译成**可读的业务错误**（接口层回 40002，**不是 500**），
// 提示用户收窄条件后重试——而不是让请求一直等，也不是丢掉日志表数据。
var ErrOperationLogKeywordTimeout = errors.New(
	"关键字命中过多，检索超出执行超时：请缩小范围（加时间窗或通道过滤），或换更精确的关键字")

// OperationLogKeywordTimeoutMS 是关键字检索语句的 MySQL 执行超时预算（毫秒，契约 23.5 第 5 条）。
//
// 取值 5 秒的依据见契约 23.10：命中 6 千条约 70 ms、命中 4.5 万条约 3.2 s（耗时与命中集合成正比），
// 5 秒足以放行所有「有参考价值」的检索，只掐掉命中数万的泛词搜索。
//
// **这条保护能做什么、不能做什么（真机实测定稿，见契约 23.10）**：
//   - 能做：语句超过预算时由 MySQL 中断（错误码 3024），接口回可读的业务错误（40002），
//     而不是 500、也不是让请求无限等下去；
//   - 不能做：MySQL 5.7 InnoDB 的**全文索引物化阶段本身不可中断**，超时只在阶段边界的
//     检查点生效——实测取页语句给 1189 ms 预算仍要 ~3.2 s 才报错（物化跑完才检查）。
//     因此「语句在预算内跑完」的检索照旧返回结果，这正是想要的语义（结果秒级可得就不该报错）。
//
// 声明为变量而非常量：集成测试需要把它调到很小才能稳定构造真实的超时场景；
// 生产代码只读不写（每次查询现读，故测试改完即生效）。
var OperationLogKeywordTimeoutMS = 5000

// mysqlQueryTimeout 是 MySQL 的 ER_QUERY_TIMEOUT（语句执行超时被中断）错误码。
const mysqlQueryTimeout = 3024

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
//
// 两条路径（契约 23.5）：**无关键字**走常规构建器（毫秒级，索引路径齐备，保持原样）；
// **有关键字**走 listOperationLogsByKeyword（带 MAX_EXECUTION_TIME 执行超时保护）。
func (s *Store) ListOperationLogs(ctx context.Context, filter OperationLogFilter) ([]model.OperationLog, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	if filter.Keyword != "" {
		return s.listOperationLogsByKeyword(db, filter)
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

// listOperationLogsByKeyword 是带关键字的查询路径（契约 23.5 第 5 条）。
//
// 为什么手写 SQL 而不用构建器：**只有这一条路径**要在 `SELECT` 之后插入 MySQL 优化器提示
// `/*+ MAX_EXECUTION_TIME(N) */`（MySQL 要求提示紧跟语句关键字，构建器给不出这个位置）；
// 条件与取值仍是**参数绑定**（`?` + args），不存在字符串拼接注入。
// 计数与取页两条语句都带提示（两条都要物化全部命中），且**共用同一个预算**：
// 第二句只拿「总预算 − 第一句已用时间」，保证一个请求的整体耗时被压在预算附近。
func (s *Store) listOperationLogsByKeyword(db *gorm.DB, filter OperationLogFilter) ([]model.OperationLog, int64, error) {
	expr, err := OperationLogKeywordExpr(filter.Keyword)
	if err != nil {
		return nil, 0, err
	}

	// MATCH 的列必须与迁移 0019 的 ft_operation_logs_search 列序完全一致。
	conditions := []string{"MATCH(action, summary, actor_name) AGAINST (? IN BOOLEAN MODE)"}
	args := []any{expr}
	if filter.Channel != "" {
		conditions = append(conditions, "channel = ?")
		args = append(args, filter.Channel)
	}
	if filter.ActorID != nil {
		conditions = append(conditions, "actor_id = ?")
		args = append(args, *filter.ActorID)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Start != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.Start.UTC())
	}
	if filter.End != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.End.UTC())
	}

	where := " FROM operation_logs WHERE " + strings.Join(conditions, " AND ")
	budget := OperationLogKeywordTimeoutMS

	var total int64
	if err := db.Raw(operationLogKeywordCountSQL(budget, where), args...).Row().Scan(&total); err != nil {
		return nil, 0, classifyOperationLogQueryError(err)
	}

	items := make([]model.OperationLog, 0)
	pageArgs := append(append([]any{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err := db.Raw(operationLogKeywordPageSQL(budget, where), pageArgs...).Scan(&items).Error; err != nil {
		return nil, 0, classifyOperationLogQueryError(err)
	}
	return items, total, nil
}

// operationLogKeywordCountSQL 组装计数语句（提示紧跟 SELECT）。
func operationLogKeywordCountSQL(budgetMS int, where string) string {
	return "SELECT " + operationLogKeywordHint(budgetMS) + "COUNT(*)" + where
}

// operationLogKeywordPageSQL 组装取页语句（提示紧跟 SELECT）。
func operationLogKeywordPageSQL(budgetMS int, where string) string {
	return "SELECT " + operationLogKeywordHint(budgetMS) + "*" + where + " ORDER BY id DESC LIMIT ? OFFSET ?"
}

// operationLogKeywordHint 返回关键字检索语句的 MySQL 优化器提示（紧跟 SELECT 关键字，契约 23.5 第 5 条）。
// 它必须出现在 SELECT 之后、结果列之前——这是 MySQL 5.7 认这个提示的唯一位置。
// timeoutMS 是该语句的可用预算（毫秒；0 在 MySQL 里表示「不限时」，调用方必须保证为正）。
//
// **返回值末尾带一个空格**，调用方必须原样拼接：提示的 `*/` 直接接上后面的内容会有坑——
// `... */COUNT(*)` 尚可，但 `... */*`（提示紧接结果列的 `*`）会让**整个提示被静默忽略**
// （真机实测：预算 1839 ms 的取页语句照跑 3.17 s，超时保护形同虚设），CLI 里甚至是语法错误。
func operationLogKeywordHint(timeoutMS int) string {
	return "/*+ MAX_EXECUTION_TIME(" + strconv.Itoa(timeoutMS) + ") */ "
}

// classifyOperationLogQueryError 把关键字查询的底层错误归一：
// MySQL 因超出 MAX_EXECUTION_TIME 中断语句（错误码 3024）时翻译为 ErrOperationLogKeywordTimeout，
// 由接口层回可读的业务错误；其余错误原样返回（仍走 50001）。
func classifyOperationLogQueryError(err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *gomysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlQueryTimeout {
		return ErrOperationLogKeywordTimeout
	}
	return err
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
