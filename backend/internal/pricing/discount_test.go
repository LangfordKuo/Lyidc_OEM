package pricing

import "testing"

func TestCouponDiscountPercent(t *testing.T) {
	cases := []struct {
		name         string
		price        string
		value        string
		wantDiscount string
		wantFinal    string
	}{
		{"10% 对 220.00", "220.00", "10", "22.00", "198.00"},
		{"12.5% 对 20.00", "20.00", "12.50", "2.50", "17.50"},
		{"半进位（20.05×10%=2.005 → 2.01）", "20.05", "10", "2.01", "18.04"},
		{"分位以下舍去（0.01×0.5%=0.00005 → 0.00）", "0.01", "0.5", "0.00", "0.01"},
		{"进一位（3.00×33.33%=0.9999 → 1.00）", "3.00", "33.33", "1.00", "2.00"},
		{"100% 全额抵扣", "220.00", "100", "220.00", "0.00"},
		{"零价", "0.00", "10", "0.00", "0.00"},
		{"最小分值", "0.01", "100", "0.01", "0.00"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			discount, final, err := CouponDiscount(testCase.price, CouponTypePercent, testCase.value)
			if err != nil {
				t.Fatalf("CouponDiscount 返回错误: %v", err)
			}
			if discount != testCase.wantDiscount || final != testCase.wantFinal {
				t.Fatalf("折扣 = %s / 折后 = %s，期望 %s / %s",
					discount, final, testCase.wantDiscount, testCase.wantFinal)
			}
		})
	}
}

func TestCouponDiscountFixed(t *testing.T) {
	cases := []struct {
		name         string
		price        string
		value        string
		wantDiscount string
		wantFinal    string
	}{
		{"常规减免", "220.00", "20", "20.00", "200.00"},
		{"减免大于售价时封顶到售价", "220.00", "500", "220.00", "0.00"},
		{"减免等于售价", "220.00", "220.00", "220.00", "0.00"},
		{"零价商品的减免封顶为 0", "0.00", "20", "0.00", "0.00"},
		{"带小数的减免", "20.00", "5.55", "5.55", "14.45"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			discount, final, err := CouponDiscount(testCase.price, CouponTypeFixed, testCase.value)
			if err != nil {
				t.Fatalf("CouponDiscount 返回错误: %v", err)
			}
			if discount != testCase.wantDiscount || final != testCase.wantFinal {
				t.Fatalf("折扣 = %s / 折后 = %s，期望 %s / %s",
					discount, final, testCase.wantDiscount, testCase.wantFinal)
			}
		})
	}
}

func TestCouponDiscountRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name  string
		price string
		kind  string
		value string
	}{
		{"未知折扣类型", "20.00", "half", "10"},
		{"售价非法", "abc", CouponTypePercent, "10"},
		{"折扣值非法", "20.00", CouponTypeFixed, "20 元"},
		{"折扣值为负", "20.00", CouponTypeFixed, "-1"},
		{"percent 超过 100", "20.00", CouponTypePercent, "100.01"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, _, err := CouponDiscount(testCase.price, testCase.kind, testCase.value); err == nil {
				t.Fatalf("CouponDiscount(%q, %q, %q) 期望报错，实际通过",
					testCase.price, testCase.kind, testCase.value)
			}
		})
	}
}

func TestParseCouponValue(t *testing.T) {
	valid := map[string]int64{
		"0":             0,
		"10":            1000,
		"12.5":          1250,
		"100":           10000,
		"0.01":          1,
		"9999999999.99": maxCouponValueCents,
	}
	for input, want := range valid {
		got, err := ParseCouponValue(input)
		if err != nil {
			t.Errorf("ParseCouponValue(%q) 返回错误: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseCouponValue(%q) = %d，期望 %d", input, got, want)
		}
	}

	invalid := []string{"", "abc", "-1", "1.234", "10000000000", "1e3"}
	for _, input := range invalid {
		if got, err := ParseCouponValue(input); err == nil {
			t.Errorf("ParseCouponValue(%q) 期望报错，实际返回 %d", input, got)
		}
	}
}
