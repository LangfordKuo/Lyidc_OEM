package model

import (
	"encoding/json"
	"testing"
)

func TestMoneyScanFromDatabaseValues(t *testing.T) {
	tests := []struct {
		name string
		src  any
		want Money
	}{
		{name: "字节切片", src: []byte("12.34"), want: "12.34"},
		{name: "字符串", src: "0.00", want: "0.00"},
		{name: "浮点", src: float64(3.5), want: "3.50"},
		{name: "NULL", src: nil, want: ZeroMoney},
		{name: "空字符串", src: "", want: ZeroMoney},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var money Money
			if err := money.Scan(tt.src); err != nil {
				t.Fatalf("Scan(%v) 返回错误: %v", tt.src, err)
			}
			if money != tt.want {
				t.Errorf("Scan(%v) = %q, 期望 %q", tt.src, money, tt.want)
			}
		})
	}

	var money Money
	if err := money.Scan(struct{}{}); err == nil {
		t.Error("Scan(非法类型) 期望返回错误，实际为 nil")
	}
}

func TestMoneyValueNormalizesEmpty(t *testing.T) {
	if value, err := ZeroMoney.Value(); err != nil || value != "0.00" {
		t.Errorf("ZeroMoney.Value() = (%v, %v), 期望 (0.00, nil)", value, err)
	}
	var empty Money
	if value, err := empty.Value(); err != nil || value != "0.00" {
		t.Errorf("空值.Value() = (%v, %v), 期望 (0.00, nil)", value, err)
	}
}

func TestMoneyMarshalJSON(t *testing.T) {
	payload, err := json.Marshal(struct {
		Balance Money `json:"balance"`
	}{Balance: "199.90"})
	if err != nil {
		t.Fatalf("json.Marshal() 返回错误: %v", err)
	}
	if want := `{"balance":"199.90"}`; string(payload) != want {
		t.Errorf("JSON = %s, 期望 %s", payload, want)
	}

	payload, err = json.Marshal(struct {
		Balance Money `json:"balance"`
	}{})
	if err != nil {
		t.Fatalf("json.Marshal() 返回错误: %v", err)
	}
	if want := `{"balance":"0.00"}`; string(payload) != want {
		t.Errorf("零值 JSON = %s, 期望 %s", payload, want)
	}
}
