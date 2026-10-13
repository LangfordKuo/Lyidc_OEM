package dbbackup

import (
	"strings"
	"testing"
)

// collectStatements 用分词器切分输入，返回语句列表。
func collectStatements(t *testing.T, input string) []string {
	t.Helper()
	var got []string
	if err := splitStatements(strings.NewReader(input), func(statement string) error {
		got = append(got, statement)
		return nil
	}); err != nil {
		t.Fatalf("splitStatements 失败: %v", err)
	}
	return got
}

// TestSplitStatements 覆盖恢复端分词的判定口径（契约 21.5）：
// 只按顶层分号切分，字符串/标识符内的分号不算，注释整行跳过。
func TestSplitStatements(t *testing.T) {
	input := "-- Lyidc_OEM database backup\n" +
		"-- database: lyidc\n" +
		"SET FOREIGN_KEY_CHECKS=0;\n" +
		"SET NAMES utf8mb4;\n" +
		"\n" +
		"-- Table: `members`\n" +
		"DROP TABLE IF EXISTS `members`;\n" +
		"CREATE TABLE `members` (\n" +
		"  `id` bigint NOT NULL,\n" +
		"  `note` varchar(64) DEFAULT 'a;b',\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n" +
		"INSERT INTO `members` (`id`,`note`) VALUES (1,'他;说\\'好\\''),(2,NULL);\n"

	got := collectStatements(t, input)
	if len(got) != 5 {
		t.Fatalf("切分出 %d 条语句，期望 5 条：%#v", len(got), got)
	}
	if got[0] != "SET FOREIGN_KEY_CHECKS=0" {
		t.Fatalf("第 1 条 = %q", got[0])
	}
	if !strings.HasPrefix(got[2], "DROP TABLE IF EXISTS `members`") {
		t.Fatalf("第 3 条 = %q", got[2])
	}
	if !strings.Contains(got[3], "'a;b'") {
		t.Fatalf("建表语句里的字符串分号被误切：%q", got[3])
	}
	if !strings.Contains(got[4], `'他;说\'好\''`) {
		t.Fatalf("INSERT 语句被误切：%q", got[4])
	}
}

// TestSplitStatementsRejectsTruncated 覆盖截断文件的拒绝口径。
func TestSplitStatementsRejectsTruncated(t *testing.T) {
	cases := map[string]string{
		"未以分号结束": "SET NAMES utf8mb4;\nINSERT INTO `t` VALUES (1,'x')\n",
		"字符串未闭合": "INSERT INTO `t` VALUES (1,'x);\n",
		"块注释未闭合": "/* 注释没有结束\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			err := splitStatements(strings.NewReader(input), func(string) error { return nil })
			if err == nil {
				t.Fatalf("截断文件应被拒绝")
			}
			if !strings.Contains(err.Error(), "备份文件") {
				t.Fatalf("错误信息应说明备份文件问题，实际：%v", err)
			}
		})
	}
}

// TestSplitStatementsIgnoresCommentsAndEmpty 覆盖纯注释与空语句不产生执行项。
func TestSplitStatementsIgnoresCommentsAndEmpty(t *testing.T) {
	got := collectStatements(t, "-- 只有注释\n;\n   ;\n# 井号注释\n/* 块注释 */\n")
	if len(got) != 0 {
		t.Fatalf("注释与空语句不应产生执行项：%#v", got)
	}
}

// TestSplitStatementsLargeStatement 覆盖大语句（分块读入的边界），确保跨缓冲不被截断。
func TestSplitStatementsLargeStatement(t *testing.T) {
	// 单条语句超过读缓冲（256 KiB），验证跨块拼接。
	payload := strings.Repeat("x", 600<<10)
	input := "INSERT INTO `big` (`v`) VALUES ('" + payload + "');\nINSERT INTO `big` (`v`) VALUES ('tail');\n"
	got := collectStatements(t, input)
	if len(got) != 2 {
		t.Fatalf("切分出 %d 条语句，期望 2 条", len(got))
	}
	if !strings.Contains(got[0], payload) {
		t.Fatalf("跨缓冲的长语句被截断（长度 %d）", len(got[0]))
	}
	if !strings.HasSuffix(got[1], "'tail')") {
		t.Fatalf("第二条语句不正确：%q", got[1])
	}
}

// TestStatementTableAndDescribe 覆盖表名提取与安全描述（错误信息绝不回显语句正文）。
func TestStatementTableAndDescribe(t *testing.T) {
	cases := []struct {
		statement string
		table     string
		desc      string
	}{
		{"DROP TABLE IF EXISTS `members`", "members", "DROP TABLE `members`"},
		{"CREATE TABLE `product_categories` (\n `id` bigint\n)", "product_categories", "CREATE TABLE `product_categories`"},
		{"INSERT INTO `admins` (`password_hash`) VALUES ('$2a$10$secret')", "admins", "INSERT（数据行，内容不回显）"},
		{"SET FOREIGN_KEY_CHECKS=0", "", "SET 会话语句"},
	}
	for _, c := range cases {
		table, _ := statementTable(c.statement)
		if table != c.table {
			t.Fatalf("statementTable(%q) = %q，期望 %q", c.statement, table, c.table)
		}
		if got := describeStatement(c.statement); got != c.desc {
			t.Fatalf("describeStatement(%q) = %q，期望 %q", c.statement, got, c.desc)
		}
	}
	// 安全边界：INSERT 的描述里绝不能出现密码哈希原文。
	secret := "$2a$10$verysecretpasswordhash"
	if desc := describeStatement("INSERT INTO `admins` VALUES ('" + secret + "')"); strings.Contains(desc, "verysecret") {
		t.Fatalf("语句描述泄漏了内容：%s", desc)
	}
}
