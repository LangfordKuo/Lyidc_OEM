package pricing

// 配置差价（R7，契约 10.2.1）：从 products.config_json 里解析每个可配置值的周期单价，
// 按商品定价规则换算后参与成交价。
//
//	成交价 = 基础价 + Σ 配置差价
//
// 上游数据形状（实测 159 个商品）：config_groups[].options[].sub[].pricings[]，
// type="configoptions"，relid=子项 id，六周期字段名是上游副词形态（见 UpstreamCycle）。
// 子项（sub）就是「价格档位」：数量型可配置项下多个子项的 qty 区间互不重叠且上下衔接
// （实测 153 单档位 / 133 多档位，多档位 100% 衔接）。数量落在哪一档就用哪一档的单价，
// 可计费数量 = 数量 − 低一档的 qty_maximum（无低一档时为 0）。
//
// 降级：子项无 pricings、无 CNY/首行、该周期缺失或为负（上游 -1.00 哨兵）时按 0 计，
// 并在明细里标注「无差价数据」，不阻断下单。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NoteNoPricingData 是明细里「该周期无差价数据」的标注文案（契约 10.2.1）。
const NoteNoPricingData = "无差价数据"

// DeltaKindSelect / DeltaKindQuantity 是差价明细的配置项形态（契约 12.4）。
const (
	DeltaKindSelect   = "select"
	DeltaKindQuantity = "quantity"
)

// quantityOptionTypes 是数量型（拉条型）可配置项的上游 option_type 编码（契约 10.3 第 5 条）。
var quantityOptionTypes = map[int]bool{4: true, 11: true, 14: true, 15: true, 19: true}

// IsQuantityOptionType 判定数量型可配置项。
func IsQuantityOptionType(optionType int) bool { return quantityOptionTypes[optionType] }

// ConfigTier 是一个可选值（价格档位）的周期单价。
type ConfigTier struct {
	ValueID    int
	QtyMinimum int
	QtyMaximum int
	Hidden     bool
	// unit 是周期 → 单位差价（分）；ok=false 表示该周期无差价数据（按 0 计）。
	unit map[string]unitPrice
}

type unitPrice struct {
	cents int64
	ok    bool
}

// ConfigOptionPricing 是一个可配置项的差价索引。
type ConfigOptionPricing struct {
	ID         int
	Name       string
	OptionType int
	Hidden     bool
	// Tiers 按 QtyMinimum 升序；数量型可有多档，选项型恒一档。
	Tiers []ConfigTier
}

// ConfigPricings 是整个商品的配置差价索引（option id → 项）。
type ConfigPricings struct {
	options map[int]*ConfigOptionPricing
}

// configPricingRow 是 pricings[] 的一行（六周期 + 归属）。
type configPricingRow struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	Currency   int    `json:"currency"`
	Monthly    string `json:"monthly"`
	Quarterly  string `json:"quarterly"`
	SemiAnnual string `json:"semiannually"`
	Annual     string `json:"annually"`
	Biennial   string `json:"biennially"`
	Triennial  string `json:"triennially"`
}

// configJSONDoc 对应 products.config_json 里我们关心的部分（上游 /cart/get_product_config 的 data）。
type configJSONDoc struct {
	ConfigGroups []struct {
		Options []struct {
			ID         int    `json:"id"`
			OptionName string `json:"option_name"`
			OptionType int    `json:"option_type"`
			Hidden     int    `json:"hidden"`
			Values     []struct {
				ID         int                `json:"id"`
				OptionName string             `json:"option_name"`
				Hidden     int                `json:"hidden"`
				QtyMinimum int                `json:"qty_minimum"`
				QtyMaximum int                `json:"qty_maximum"`
				Pricings   []configPricingRow `json:"pricings"`
			} `json:"sub"`
		} `json:"options"`
	} `json:"config_groups"`
}

// ParseConfigPricings 解析 config_json 里的配置差价。
// 空串/解析失败返回空索引（调用方按「无差价数据」降级），不报错，避免阻断下单。
func ParseConfigPricings(configJSON string) *ConfigPricings {
	index := &ConfigPricings{options: make(map[int]*ConfigOptionPricing)}
	trimmed := strings.TrimSpace(configJSON)
	if trimmed == "" || trimmed == "null" {
		return index
	}
	var doc configJSONDoc
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return index
	}
	for _, group := range doc.ConfigGroups {
		for _, option := range group.Options {
			if option.ID <= 0 {
				continue
			}
			entry := &ConfigOptionPricing{
				ID:         option.ID,
				Name:       option.OptionName,
				OptionType: option.OptionType,
				Hidden:     option.Hidden != 0,
			}
			for _, value := range option.Values {
				if value.ID <= 0 {
					continue
				}
				entry.Tiers = append(entry.Tiers, ConfigTier{
					ValueID:    value.ID,
					QtyMinimum: value.QtyMinimum,
					QtyMaximum: value.QtyMaximum,
					Hidden:     value.Hidden != 0,
					unit:       unitFromPricings(value.Pricings),
				})
			}
			sort.SliceStable(entry.Tiers, func(i, j int) bool {
				if entry.Tiers[i].QtyMinimum != entry.Tiers[j].QtyMinimum {
					return entry.Tiers[i].QtyMinimum < entry.Tiers[j].QtyMinimum
				}
				return entry.Tiers[i].ValueID < entry.Tiers[j].ValueID
			})
			index.options[option.ID] = entry
		}
	}
	return index
}

// unitFromPricings 挑出子项的周期单价：取 type=configoptions 的行，优先 code=CNY，否则首行。
// 每周期独立判定可用性：缺失 / 非法 / 负值（上游 -1.00 哨兵）都算「无差价数据」。
func unitFromPricings(rows []configPricingRow) map[string]unitPrice {
	out := make(map[string]unitPrice, len(Cycles))
	for _, cycle := range Cycles {
		out[cycle] = unitPrice{}
	}
	if len(rows) == 0 {
		return out
	}
	picked := -1
	for i := range rows {
		if rows[i].Type != "" && rows[i].Type != "configoptions" {
			continue
		}
		if strings.EqualFold(rows[i].Code, "CNY") {
			picked = i
			break
		}
		if picked < 0 {
			picked = i
		}
	}
	if picked < 0 {
		return out
	}
	row := rows[picked]
	raw := map[string]string{
		CycleMonthly:      row.Monthly,
		CycleQuarterly:    row.Quarterly,
		CycleSemiAnnually: row.SemiAnnual,
		CycleAnnually:     row.Annual,
		CycleBiennially:   row.Biennial,
		CycleTriennially:  row.Triennial,
	}
	for _, cycle := range Cycles {
		out[cycle] = parseUnit(raw[cycle])
	}
	return out
}

// parseUnit 解析单周期单价：非负合法金额才算有数据，其余（含 -1.00 哨兵）算无数据。
func parseUnit(amount string) unitPrice {
	cents, err := ParseAmount(amount)
	if err != nil || cents < 0 {
		return unitPrice{}
	}
	return unitPrice{cents: cents, ok: true}
}

// Option 返回某可配置项的差价索引；不存在返回 nil。
func (c *ConfigPricings) Option(optionID int) *ConfigOptionPricing {
	if c == nil {
		return nil
	}
	return c.options[optionID]
}

// Range 返回数量型可配置项的取值区间（**全部可见档位的并集**，契约 10.3 第 6 条）。
// 无可见档位时 bounded=false（调用方不做范围校验）。
func (o *ConfigOptionPricing) Range() (min int, max int, bounded bool) {
	if o == nil {
		return 0, 0, false
	}
	first := true
	for _, tier := range o.Tiers {
		if tier.Hidden {
			continue
		}
		if first {
			min, max, first = tier.QtyMinimum, tier.QtyMaximum, false
			continue
		}
		if tier.QtyMinimum < min {
			min = tier.QtyMinimum
		}
		if tier.QtyMaximum > max {
			max = tier.QtyMaximum
		}
	}
	return min, max, !first
}

// TierFor 返回数量落在的档位（按 QtyMinimum~QtyMaximum）；找不到返回 nil。
func (o *ConfigOptionPricing) TierFor(qty int) *ConfigTier {
	if o == nil {
		return nil
	}
	for i := range o.Tiers {
		tier := &o.Tiers[i]
		if qty >= tier.QtyMinimum && qty <= tier.QtyMaximum {
			return tier
		}
	}
	return nil
}

// TierByID 返回指定值 id 的档位；找不到返回 nil。
func (o *ConfigOptionPricing) TierByID(valueID int) *ConfigTier {
	if o == nil {
		return nil
	}
	for i := range o.Tiers {
		if o.Tiers[i].ValueID == valueID {
			return &o.Tiers[i]
		}
	}
	return nil
}

// VisibleTiers 返回会员可见的档位（hidden != 0 的上游隐藏值不下发）。
func (o *ConfigOptionPricing) VisibleTiers() []ConfigTier {
	if o == nil {
		return nil
	}
	out := make([]ConfigTier, 0, len(o.Tiers))
	for _, tier := range o.Tiers {
		if !tier.Hidden {
			out = append(out, tier)
		}
	}
	return out
}

// Unit 返回某周期的单位差价（分）与是否有差价数据。
func (t *ConfigTier) Unit(cycle string) (cents int64, ok bool) {
	if t == nil {
		return 0, false
	}
	price, exists := t.unit[cycle]
	if !exists {
		return 0, false
	}
	return price.cents, price.ok
}

// UnitView 返回该档位六个周期的差价视图（已按定价规则换算，供会员端 `pricings` 下发）。
// 数量型是**单位差价**；null 表示该周期无差价数据（契约 10.3 第 8 条）。
func (t *ConfigTier) UnitView(rule Rule) map[string]*string {
	out := make(map[string]*string, len(Cycles))
	for _, cycle := range Cycles {
		cents, ok := t.Unit(cycle)
		if !ok {
			out[cycle] = nil
			continue
		}
		amount := FormatAmount(rule.AdjustDelta(cents))
		out[cycle] = &amount
	}
	return out
}

// AdjustDelta 按定价规则换算配置差价（契约 10.2.1）：
//
//	upstream：差价原样
//	markup  ：差价 ×(1+markup_percent%)，四舍五入到分
//	fixed   ：固定价只覆盖基础价，差价**原样叠加**（不套 markup 之外的处理）
//
// 组合模式（markup + fixed）下差价按 markup 加价。
// 加价作用于**单位差价**再乘可计费数量，保证前端预览与服务端成交逐分一致。
func (r Rule) AdjustDelta(cents int64) int64 {
	percent, hasMarkup := r.markupBasisPoints()
	if !hasMarkup {
		return cents
	}
	return applyMarkup(cents, percent)
}

// DeltaLine 是成交价明细里的一项（契约 12.4 的 price_breakdown.deltas[]）。
type DeltaLine struct {
	OptionID   int    `json:"option_id"`
	OptionName string `json:"option_name"`
	Kind       string `json:"kind"`
	ValueID    int    `json:"value_id,omitempty"`
	Qty        int    `json:"qty,omitempty"`
	Unit       string `json:"unit"`
	Billable   int    `json:"billable,omitempty"`
	Amount     string `json:"amount"`
	Note       string `json:"note,omitempty"`
}

// Breakdown 是成交价明细快照（契约 12.4 / 10.2.1）。
type Breakdown struct {
	Base   string      `json:"base"`
	Deltas []DeltaLine `json:"deltas"`
	Total  string      `json:"total"`
}

// ConfigSelection 是一次配置选择（键=配置项 id 的十进制串，值=值 id 或数量）。
type ConfigSelection map[string]string

// Quote 按「基础价 + Σ 配置差价」计算某周期成交价并生成明细。
//
// base 是该周期基础价（已按 10.2 计算的本地售价，可为空串表示不可售）。
// 配置里的未知项/未知值跳过（下单校验另有前置拦截），数量型按档位单位价 × 可计费数量。
// 返回的 Breakdown.Total 与 Amount 一致；Deltas 顺序按配置项 id 升序，保证快照稳定可比。
func (c *ConfigPricings) Quote(rule Rule, config ConfigSelection, cycle string, base string) Breakdown {
	baseCents, err := ParseAmount(base)
	if err != nil || baseCents < 0 {
		baseCents = 0
	}
	breakdown := Breakdown{
		Base:   FormatAmount(baseCents),
		Deltas: []DeltaLine{},
	}
	total := baseCents

	// 明细按配置项 id **数值**升序，保证快照稳定可比（不能按字符串序：10 会排到 5 前面）。
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left, leftOK := parseOptionID(keys[i])
		right, rightOK := parseOptionID(keys[j])
		if leftOK != rightOK {
			return leftOK
		}
		if left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		optionID, ok := parseOptionID(key)
		if !ok {
			continue
		}
		option := c.Option(optionID)
		if option == nil {
			continue
		}
		raw := strings.TrimSpace(config[key])
		if IsQuantityOptionType(option.OptionType) {
			qty := parseQty(raw)
			if qty < 0 {
				continue
			}
			line := option.quantityLine(rule, cycle, qty)
			if line == nil {
				continue
			}
			total += mustCents(line.Amount)
			breakdown.Deltas = append(breakdown.Deltas, *line)
			continue
		}
		valueID, ok := parseOptionID(raw)
		if !ok {
			continue
		}
		tier := option.TierByID(valueID)
		if tier == nil {
			continue
		}
		line := option.selectLine(rule, cycle, tier)
		total += mustCents(line.Amount)
		breakdown.Deltas = append(breakdown.Deltas, line)
	}

	breakdown.Total = FormatAmount(total)
	return breakdown
}

// selectLine 选项型差价 = 该值该周期的单位差价（无差价数据按 0 并标注）。
func (o *ConfigOptionPricing) selectLine(rule Rule, cycle string, tier *ConfigTier) DeltaLine {
	cents, ok := tier.Unit(cycle)
	line := DeltaLine{
		OptionID:   o.ID,
		OptionName: o.Name,
		Kind:       DeltaKindSelect,
		ValueID:    tier.ValueID,
		Unit:       FormatAmount(0),
		Amount:     FormatAmount(0),
	}
	if !ok {
		line.Note = NoteNoPricingData
		return line
	}
	adjusted := rule.AdjustDelta(cents)
	line.Unit = FormatAmount(adjusted)
	line.Amount = FormatAmount(adjusted)
	return line
}

// quantityLine 数量型差价 = 档位单位差价 × 可计费数量，
// 可计费数量 = 数量 − 低一档的 qty_maximum（无低一档时为 0）。
func (o *ConfigOptionPricing) quantityLine(rule Rule, cycle string, qty int) *DeltaLine {
	tier := o.TierFor(qty)
	if tier == nil {
		// 数量不在任何档位内（异常数据 / 未校验通过）：按无差价数据降级，不阻断。
		return &DeltaLine{
			OptionID:   o.ID,
			OptionName: o.Name,
			Kind:       DeltaKindQuantity,
			Qty:        qty,
			Unit:       FormatAmount(0),
			Billable:   0,
			Amount:     FormatAmount(0),
			Note:       NoteNoPricingData,
		}
	}
	billable := qty - o.tierFloor(tier)
	if billable < 0 {
		billable = 0
	}
	cents, ok := tier.Unit(cycle)
	line := &DeltaLine{
		OptionID:   o.ID,
		OptionName: o.Name,
		Kind:       DeltaKindQuantity,
		ValueID:    tier.ValueID,
		Qty:        qty,
		Unit:       FormatAmount(0),
		Billable:   billable,
		Amount:     FormatAmount(0),
	}
	if !ok {
		line.Note = NoteNoPricingData
		return line
	}
	adjusted := rule.AdjustDelta(cents)
	line.Unit = FormatAmount(adjusted)
	line.Amount = FormatAmount(adjusted * int64(billable))
	return line
}

// tierFloor 返回档位的计费起点：低一档的 qty_maximum；无低一档时为 0
// （单档位即退化为「单位差价 × 数量」，契约 10.2.1）。
func (o *ConfigOptionPricing) tierFloor(tier *ConfigTier) int {
	floor := 0
	for i := range o.Tiers {
		other := &o.Tiers[i]
		if other.QtyMaximum < tier.QtyMinimum && other.QtyMaximum > floor {
			floor = other.QtyMaximum
		}
	}
	return floor
}

// Amount 是 Breakdown 的成交总额（= Total），便于调用方直接取。
func (b Breakdown) Amount() string { return b.Total }

// Marshal 序列化明细快照（落 orders.price_breakdown_json）。
func (b Breakdown) Marshal() (string, error) {
	encoded, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("价格明细序列化失败: %w", err)
	}
	return string(encoded), nil
}

// ParseBreakdown 解析落库的明细快照；异常返回 nil（视图不因此失败）。
func ParseBreakdown(raw string) *Breakdown {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var breakdown Breakdown
	if err := json.Unmarshal([]byte(trimmed), &breakdown); err != nil {
		return nil
	}
	return &breakdown
}

func parseOptionID(raw string) (int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	id := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		id = id*10 + int(ch-'0')
		if id > 1<<31-1 {
			return 0, false
		}
	}
	return id, id > 0
}

func parseQty(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return -1
	}
	qty := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return -1
		}
		qty = qty*10 + int(ch-'0')
		if qty > 1<<31-1 {
			return -1
		}
	}
	return qty
}

func mustCents(amount string) int64 {
	cents, err := ParseAmount(amount)
	if err != nil {
		return 0
	}
	return cents
}
