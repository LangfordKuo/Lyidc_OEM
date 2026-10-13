package dbbackup

import (
	"strings"
	"testing"
	"time"
)

// TestAppendValueLiteral 覆盖字面量编码的关键分支（契约 21.1）：
// NULL 与字符串严格区分、转义正确、二进制走十六进制、时间按 UTC 墙钟值写回。
func TestAppendValueLiteral(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		binary bool
		want   string
	}{
		{name: "NULL", value: nil, want: "NULL"},
		{name: "空串不是 NULL", value: []byte(""), want: "''"},
		{name: "字符串", value: []byte("hello"), want: "'hello'"},
		{name: "单引号转义", value: []byte("it's"), want: `'it\'s'`},
		{name: "反斜杠转义", value: []byte(`a\b`), want: `'a\\b'`},
		{name: "换行转义", value: []byte("a\nb"), want: `'a\nb'`},
		{name: "回车转义", value: []byte("a\rb"), want: `'a\rb'`},
		{name: "NUL 转义", value: []byte{'a', 0x00, 'b'}, want: `'a\0b'`},
		{name: "中文原样保留", value: []byte("香港·测试机房"), want: "'香港·测试机房'"},
		{name: "emoji 原样保留", value: []byte("🚀"), want: "'🚀'"},
		{name: "二进制走十六进制", value: []byte{0x00, 0xff, 0x1a}, binary: true, want: `X'00FF1A'`},
		{name: "空二进制", value: []byte{}, binary: true, want: `X''`},
		{name: "整数", value: int64(-42), want: "-42"},
		{name: "无符号整数", value: uint64(18446744073709551615), want: "18446744073709551615"},
		{name: "浮点可精确往返", value: float64(0.1), want: "0.1"},
		{name: "布尔", value: true, want: "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(appendValueLiteral(nil, c.value, c.binary))
			if got != c.want {
				t.Fatalf("appendValueLiteral(%v) = %s，期望 %s", c.value, got, c.want)
			}
		})
	}
}

// TestAppendTimeLiteral 覆盖时间字面量（含零值日期与小数秒）。
func TestAppendTimeLiteral(t *testing.T) {
	cases := []struct {
		name  string
		value time.Time
		want  string
	}{
		{
			name:  "UTC 墙钟值写回",
			value: time.Date(2026, 10, 14, 7, 8, 9, 0, time.UTC),
			want:  "'2026-10-14 07:08:09'",
		},
		{
			name:  "非 UTC 时区折算成 UTC 写回",
			value: time.Date(2026, 10, 14, 15, 8, 9, 0, time.FixedZone("CST", 8*3600)),
			want:  "'2026-10-14 07:08:09'",
		},
		{
			name:  "小数秒原样保留",
			value: time.Date(2026, 10, 14, 7, 8, 9, 123000000, time.UTC),
			want:  "'2026-10-14 07:08:09.123000'",
		},
		{
			name:  "零值日期",
			value: time.Time{},
			want:  "'0000-00-00 00:00:00'",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(appendTimeLiteral(nil, c.value)); got != c.want {
				t.Fatalf("appendTimeLiteral(%v) = %s，期望 %s", c.value, got, c.want)
			}
		})
	}
}

// TestStringLiteralHasNoRawNewline 断言字符串字面量里不出现裸换行——
// 这保证一条 INSERT 始终是备份文件里的一行（可读可 diff），也简化了恢复端的分词。
func TestStringLiteralHasNoRawNewline(t *testing.T) {
	value := "第一行\r\n第二行\n第三行\ttab"
	got := string(appendStringLiteral(nil, value))
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("字面量里出现裸换行/回车：%q", got)
	}
}

// TestIsBinaryType 覆盖二进制列判定：文本型列（VARCHAR/TEXT/JSON/DECIMAL）绝不能走十六进制。
func TestIsBinaryType(t *testing.T) {
	binary := []string{"BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "BIT", "GEOMETRY"}
	for _, name := range binary {
		if !isBinaryType(name) {
			t.Fatalf("%s 应判为二进制列", name)
		}
	}
	text := []string{"VARCHAR", "TEXT", "LONGTEXT", "JSON", "DECIMAL", "ENUM", "SET", "TINYINT", "DATETIME", "TIME"}
	for _, name := range text {
		if isBinaryType(name) {
			t.Fatalf("%s 不应判为二进制列（驱动同样返回 []byte，但它是文本）", name)
		}
	}
}

// TestQuoteIdent 覆盖标识符转义（反引号双写）。
func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent("members"); got != "`members`" {
		t.Fatalf("quoteIdent = %s", got)
	}
	if got := quoteIdent("we`ird"); got != "`we``ird`" {
		t.Fatalf("quoteIdent 未双写反引号：%s", got)
	}
}
