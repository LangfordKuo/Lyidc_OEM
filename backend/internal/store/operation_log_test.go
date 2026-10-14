package store

import (
	"errors"
	"io"
	"strings"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
)

// TestOperationLogKeywordExpr 覆盖关键字编译口径（契约 23.5）：
// 单词按短语整体匹配、多词 AND、单字符词明确报错（不退回全表 LIKE）。
func TestOperationLogKeywordExpr(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		want    string
	}{
		{name: "中文单词", keyword: "上游设置", want: `+"上游设置"`},
		{name: "英文单词", keyword: "admin", want: `+"admin"`},
		{name: "多词 AND", keyword: "备份 目录", want: `+"备份" +"目录"`},
		{name: "多余空白折叠", keyword: "  备份   目录  ", want: `+"备份" +"目录"`},
		{name: "转义双引号与反斜杠", keyword: `a"b\c`, want: `+"a\"b\\c"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OperationLogKeywordExpr(tt.keyword)
			if err != nil {
				t.Fatalf("OperationLogKeywordExpr(%q) 返回错误: %v", tt.keyword, err)
			}
			if got != tt.want {
				t.Fatalf("OperationLogKeywordExpr(%q) = %q，期望 %q", tt.keyword, got, tt.want)
			}
		})
	}

	invalid := []string{"", "   ", "上", "a", "备份 目", "商 品"}
	for _, keyword := range invalid {
		if _, err := OperationLogKeywordExpr(keyword); !errors.Is(err, ErrOperationLogKeyword) {
			t.Errorf("OperationLogKeywordExpr(%q) 应返回 ErrOperationLogKeyword，实际 err=%v", keyword, err)
		}
	}
}

// TestOperationLogKeywordTimeoutHint 覆盖关键字检索的执行超时保护（契约 23.5 第 5 条）：
// 默认预算 5 秒、提示必须拼成 MySQL 认的 /*+ MAX_EXECUTION_TIME(N) */ 形式，
// 且预算可被调整（集成测试把它调小以构造真实超时场景）。
func TestOperationLogKeywordTimeoutHint(t *testing.T) {
	original := OperationLogKeywordTimeoutMS
	t.Cleanup(func() { OperationLogKeywordTimeoutMS = original })

	if OperationLogKeywordTimeoutMS != 5000 {
		t.Errorf("关键字检索默认预算 = %d ms，期望 5000 ms（契约 23.5 第 5 条）",
			OperationLogKeywordTimeoutMS)
	}
	// 末尾的空格是**必需的**：提示的 */ 直接接结果列的 * 会让整条提示被 MySQL 静默忽略
	// （真机实测到的坑，见 operationLogKeywordHint 的注释）。
	if got, want := operationLogKeywordHint(5000), "/*+ MAX_EXECUTION_TIME(5000) */ "; got != want {
		t.Fatalf("关键字检索提示 = %q，期望 %q", got, want)
	}
	if got, want := operationLogKeywordHint(250), "/*+ MAX_EXECUTION_TIME(250) */ "; got != want {
		t.Fatalf("改预算后的提示 = %q，期望 %q", got, want)
	}
	if !strings.HasSuffix(operationLogKeywordHint(5000), "*/ ") {
		t.Fatal("提示必须以 */ 加空格结尾，否则紧跟 * 或 COUNT 时会失效")
	}
}

// TestOperationLogKeywordSQLShape 断言两条语句的实际形态（契约 23.5 第 5 条）：
// 提示必须紧跟 SELECT，且提示的 `*/` 之后**必须有空白**——`... */*`（直接接结果列的 *）
// 会被 MySQL **静默忽略**（真机实测：预算 1839 ms 的取页语句照跑 3.17 s，超时保护形同虚设）。
func TestOperationLogKeywordSQLShape(t *testing.T) {
	where := " FROM operation_logs WHERE MATCH(action, summary, actor_name) AGAINST (? IN BOOLEAN MODE)"

	countSQL := operationLogKeywordCountSQL(5000, where)
	if !strings.HasPrefix(countSQL, "SELECT /*+ MAX_EXECUTION_TIME(5000) */ COUNT(*)") {
		t.Fatalf("计数语句形态不对：%q", countSQL)
	}
	pageSQL := operationLogKeywordPageSQL(1200, where)
	if !strings.HasPrefix(pageSQL, "SELECT /*+ MAX_EXECUTION_TIME(1200) */ *") {
		t.Fatalf("取页语句形态不对（提示与结果列的 * 之间必须有空白）：%q", pageSQL)
	}
	if !strings.Contains(pageSQL, "ORDER BY id DESC LIMIT ? OFFSET ?") {
		t.Fatalf("取页语句缺少排序与分页：%q", pageSQL)
	}
}

// TestClassifyOperationLogQueryError 覆盖超时错误的翻译：
// 只有 MySQL 的 ER_QUERY_TIMEOUT（3024）翻译成 ErrOperationLogKeywordTimeout，
// 其余错误原样上抛（接口层仍按 50001 处理），不误伤也不吞错。
func TestClassifyOperationLogQueryError(t *testing.T) {
	timeoutErr := &gomysql.MySQLError{
		Number:  3024,
		Message: "Query execution was interrupted, maximum statement execution time exceeded",
	}
	if got := classifyOperationLogQueryError(timeoutErr); !errors.Is(got, ErrOperationLogKeywordTimeout) {
		t.Errorf("3024 应翻译为 ErrOperationLogKeywordTimeout，实际 %v", got)
	}

	otherMysqlErr := &gomysql.MySQLError{Number: 1064, Message: "syntax error"}
	if got := classifyOperationLogQueryError(otherMysqlErr); !errors.Is(got, otherMysqlErr) ||
		errors.Is(got, ErrOperationLogKeywordTimeout) {
		t.Errorf("非超时的 MySQL 错误应原样上抛，实际 %v", got)
	}
	if got := classifyOperationLogQueryError(io.EOF); !errors.Is(got, io.EOF) {
		t.Errorf("普通错误应原样上抛，实际 %v", got)
	}
	if got := classifyOperationLogQueryError(nil); got != nil {
		t.Errorf("classifyOperationLogQueryError(nil) = %v，期望 nil", got)
	}
}

// TestOperationLogKeywordTimeoutMessage 断言超时提示是「可读 + 可执行」的：
// 必须说清原因（命中过多）与怎么办（缩小范围：时间窗 / 通道过滤 / 更精确的关键字）。
func TestOperationLogKeywordTimeoutMessage(t *testing.T) {
	message := ErrOperationLogKeywordTimeout.Error()
	for _, want := range []string{"命中过多", "缩小范围", "时间窗", "通道过滤", "关键字"} {
		if !strings.Contains(message, want) {
			t.Errorf("超时提示 %q 缺少关键要素 %q", message, want)
		}
	}
}

// TestOperationLogClampHelpers 覆盖落库前的边界收敛（列宽与状态码范围）。
func TestOperationLogClampHelpers(t *testing.T) {
	if got := clampHTTPStatus(-1); got != 0 {
		t.Errorf("clampHTTPStatus(-1) = %d，期望 0", got)
	}
	if got := clampHTTPStatus(200); got != 200 {
		t.Errorf("clampHTTPStatus(200) = %d，期望 200", got)
	}
	if got := clampHTTPStatus(70000); got != 0 {
		t.Errorf("clampHTTPStatus(70000) = %d，期望 0（越界归零）", got)
	}
	if got := clampDurationMS(-5); got != 0 {
		t.Errorf("clampDurationMS(-5) = %d，期望 0", got)
	}
	if got := clampDurationMS(1234); got != 1234 {
		t.Errorf("clampDurationMS(1234) = %d，期望 1234", got)
	}
}
