package pricing

import (
	"errors"
	"testing"
)

// TestComputeRebateCentsFormulaExamples 覆盖契约 18.2 的四个验证例子（逐一断言）。
//
//	返佣 = max(订单原价 × 生效比例 − 本单优惠券减免, 0)，下取整到分。
func TestComputeRebateCentsFormulaExamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     string // 订单原价（元）
		discount string // 本单优惠券减免（元）
		ratio    string // 生效比例
		want     string // 期望返佣（元）
	}{
		{"原价 100、比例 20% → 返 20", "100.00", "0.00", "20.00", "20.00"},
		{"用券实付 90（减 10）→ 返 10", "100.00", "10.00", "20.00", "10.00"},
		{"20% 比例下 8 折券（减 20）→ 返 0", "100.00", "20.00", "20.00", "0.00"},
		{"9 折券（减 10）→ 返 10", "100.00", "10.00", "20.00", "10.00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, err := ParseAmount(tc.base)
			if err != nil {
				t.Fatalf("ParseAmount(%q) = %v", tc.base, err)
			}
			discount, err := ParseAmount(tc.discount)
			if err != nil {
				t.Fatalf("ParseAmount(%q) = %v", tc.discount, err)
			}
			ratio, err := ParsePercent(tc.ratio)
			if err != nil {
				t.Fatalf("ParsePercent(%q) = %v", tc.ratio, err)
			}
			want, err := ParseAmount(tc.want)
			if err != nil {
				t.Fatalf("ParseAmount(%q) = %v", tc.want, err)
			}
			if got := ComputeRebateCents(base, discount, ratio); got != want {
				t.Fatalf("ComputeRebateCents(%d, %d, %d) = %s，期望 %s",
					base, discount, ratio, FormatAmount(got), tc.want)
			}
		})
	}
}

func TestComputeRebateCentsFloorAndFloorZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     int64
		discount int64
		ratio    int64
		want     int64
	}{
		// 33 分 × 20% = 6.6 分 → 下取整到分 = 6 分。
		{"下取整到分", 33, 0, 2000, 6},
		// 1 分 × 20% = 0.2 分 → 0 分。
		{"极小额下取整为 0", 1, 0, 2000, 0},
		// 券减免吃满比例后超出 → 公式地板为 0，永远不会负。
		{"减免超出比例时地板为 0", 10000, 5000, 2000, 0},
		// 比例 0 → 不返。
		{"比例为 0 不返", 10000, 0, 0, 0},
		// 负比例/负基数按 0 处理（防御）。
		{"负比例返回 0", 10000, 0, -1, 0},
		{"负基数返回 0", -1, 0, 2000, 0},
		{"负减免返回 0", 10000, -1, 2000, 0},
		// 100% 比例：返原价减减免。
		{"100% 比例", 10000, 1000, 10000, 9000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ComputeRebateCents(tc.base, tc.discount, tc.ratio); got != tc.want {
				t.Fatalf("ComputeRebateCents(%d, %d, %d) = %d，期望 %d",
					tc.base, tc.discount, tc.ratio, got, tc.want)
			}
		})
	}
}

func TestParsePercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr error // nil 表示不报错
	}{
		{"整数百分比", "20", 2000, nil},
		{"两位小数", "20.00", 2000, nil},
		{"一位小数", "12.5", 1250, nil},
		{"上限 100.00", "100.00", 10000, nil},
		{"下限 0.00", "0.00", 0, nil},
		{"空串", "", 0, ErrRule},
		{"负数", "-1.00", 0, ErrRule},
		{"超过 100", "100.01", 0, ErrRule},
		{"三位小数", "20.001", 0, ErrFormat},
		{"非数字", "abc", 0, ErrFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePercent(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ParsePercent(%q) err = %v，期望 %v", tc.input, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePercent(%q) = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParsePercent(%q) = %d，期望 %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input int64
		want  string
	}{
		{0, "0.00"},
		{2000, "20.00"},
		{1250, "12.50"},
		{5, "0.05"},
	}
	for _, tc := range tests {
		if got := FormatPercent(tc.input); got != tc.want {
			t.Fatalf("FormatPercent(%d) = %q，期望 %q", tc.input, got, tc.want)
		}
	}
}

func TestParseSignedAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"正数", "12.34", 1234, false},
		{"零", "0.00", 0, false},
		{"负数", "-12.34", -1234, false},
		{"负零", "-0.00", 0, false},
		{"整数", "5", 500, false},
		{"空串", "", 0, true},
		{"双负号", "--1.00", 0, true},
		{"三位小数", "1.001", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSignedAmount(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSignedAmount(%q) = %d，期望报错", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSignedAmount(%q) = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseSignedAmount(%q) = %d，期望 %d", tc.input, got, tc.want)
			}
		})
	}
}
