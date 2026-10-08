package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
)

// ZeroMoney 是金额零值。
const ZeroMoney Money = "0.00"

// Money 表示 DECIMAL(14,2) 定点金额。
//
// 为避免浮点误差，Go 侧与 JSON 均使用字符串形式（如 "0.00"、"12.34"）。
type Money string

// Scan 实现 sql.Scanner：把数据库 DECIMAL 值读成定点小数字符串。
func (m *Money) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*m = ZeroMoney
	case []byte:
		*m = Money(v)
	case string:
		*m = Money(v)
	case float64:
		*m = Money(strconv.FormatFloat(v, 'f', 2, 64))
	default:
		return fmt.Errorf("无法把 %T 解析为金额", src)
	}
	if *m == "" {
		*m = ZeroMoney
	}
	return nil
}

// Value 实现 driver.Valuer：空值归一为 "0.00"，避免写入非法空串。
func (m Money) Value() (driver.Value, error) {
	if m == "" {
		return string(ZeroMoney), nil
	}
	return string(m), nil
}

// MarshalJSON 输出 JSON 字符串（如 "0.00"），保证前端拿到的是定点小数文本。
func (m Money) MarshalJSON() ([]byte, error) {
	if m == "" {
		return json.Marshal(string(ZeroMoney))
	}
	return json.Marshal(string(m))
}
