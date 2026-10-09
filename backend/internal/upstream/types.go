package upstream

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Response 是上游统一响应包：{"status":200,"msg":"请求成功","data":{...}}。
//
// 注意两点（详见 docs/api-contract.md 第 8 节）：
//  1. status 是**业务状态码**，上游即便业务失败也返回 HTTP 200；
//  2. 少数接口把结果直接挂在顶层（jwt / hostid / invoiceid / user），因此这里同时保留这些顶层镜像字段。
type Response struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`

	// 顶层镜像字段：仅部分接口返回。
	JWT       string          `json:"jwt"`
	HostID    json.RawMessage `json:"hostid"`
	InvoiceID json.Number     `json:"invoiceid"`
	User      *ClientUser     `json:"user"`
	// CancelRequestID / CancelID 是 /host/cancel 受理后回带的取消申请 ID（阶段 5c；
	// 上游可能放在顶层或 data 内，字段名实测口径为 cancel_request_id、文档口径为 cancel_id，
	// 两种都解析）。
	CancelRequestID json.Number `json:"cancel_request_id"`
	CancelID        json.Number `json:"cancel_id"`

	// IsAff 是上游注入的推广开关（v3.7.5 的 jsons() 行为），与本项目无关。
	IsAff string `json:"is_aff"`
}

// ClientUser 是上游在下单接口里回带的账号信息。
type ClientUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Currency int    `json:"currency"`
}

// HostIDs 解析主机 ID。上游在开通/支付成功时回带主机 ID，位置有两种：
// 顶层 hostid（下单/结算接口）与 data.hostid（/apply_credit 支付接口，上游官方下游实现即读此处）。
// 取值既可能是单个数字、数字字符串，也可能是数组，统一解析为切片。
func (r *Response) HostIDs() []int {
	if ids := parseIDs(r.HostID); len(ids) > 0 {
		return ids
	}
	if len(r.Data) > 0 && string(r.Data) != "null" {
		var body struct {
			HostID json.RawMessage `json:"hostid"`
		}
		if err := json.Unmarshal(r.Data, &body); err == nil {
			return parseIDs(body.HostID)
		}
	}
	return nil
}

// parseIDs 兼容上游 hostid 的三种写法：1、"1"、[1,2]。
func parseIDs(raw json.RawMessage) []int {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	var single json.Number
	if err := json.Unmarshal(raw, &single); err == nil {
		if id, err := strconv.Atoi(single.String()); err == nil && id > 0 {
			return []int{id}
		}
		return nil
	}

	var many []json.Number
	if err := json.Unmarshal(raw, &many); err == nil {
		ids := make([]int, 0, len(many))
		for _, item := range many {
			if id, err := strconv.Atoi(item.String()); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		return ids
	}

	// 形如 ["1","2"] 的字符串数组。
	var strs []string
	if err := json.Unmarshal(raw, &strs); err == nil {
		ids := make([]int, 0, len(strs))
		for _, item := range strs {
			if id, err := strconv.Atoi(item); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		return ids
	}
	return nil
}

// Currency 是上游货币。
type Currency struct {
	ID     int    `json:"id"`
	Code   string `json:"code"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

// ProductCatalog 是 GET /cart/all 的返回：产品分组及各组下的商品。
type ProductCatalog struct {
	Groups   []ProductGroup `json:"products"`
	Count    int            `json:"count"`
	Currency string         `json:"currency"`
}

// ProductGroup 是上游的一级产品分组。
type ProductGroup struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Products []Product `json:"products"`
}

// Product 是上游商品。
//
// 字段取自实测响应（docs/api-contract.md 8.4）：/cart/all 只回带基础字段
// （名称、类型、模块、库存开关等），价格与可配置项需要再调 ProductConfig；
// 上游后续版本新增的字段会被忽略，不会导致解析失败。
type Product struct {
	ID          int    `json:"id"`
	GroupID     int    `json:"gid"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`

	PayMethod string `json:"pay_method"`
	// PayType 上游在该版本里是 JSON 文本（如 {"pay_type":"recurring",...}），原样保留。
	PayType    string `json:"pay_type"`
	APIType    string `json:"api_type"`
	Module     string `json:"module"`
	SourceType string `json:"source_type"`

	// 上游代理相关字段（上游自己也是某个更上游的下游时有意义）。
	IsLocalProxy            int      `json:"is_local_proxy"`
	AllowedProxyModes       []string `json:"allowed_proxy_modes"`
	ProxyMode               string   `json:"proxy_mode"`
	LocalProxyModule        string   `json:"local_proxy_module"`
	DirectUpstreamProductID int      `json:"direct_upstream_product_id"`
	UpstreamPriceType       string   `json:"upstream_price_type"`
	UpstreamPriceValue      string   `json:"upstream_price_value"`
	UpstreamVersion         int      `json:"upstream_version"`

	// 库存与限购（/cart/all 不返回，ProductConfig 与 Stock 返回）。
	StockControl int `json:"stock_control"`
	Qty          int `json:"qty"`
	Ontrial      int `json:"ontrial"`

	// 计费周期（部分接口回带；价格周期以 Pricing 为准）。
	BillingCycle   string `json:"billingcycle"`
	BillingCycleZh any    `json:"billingcycle_zh"`
}

// ProductConfig 是 GET /cart/get_product_config 的返回：商品详情 + 可配置项 + 价格 + 自定义字段。
type ProductConfig struct {
	// Flag 是上游对「当前账号是否可购买该商品」的标记（权限/销售限制）。
	Flag         any             `json:"flag"`
	Product      Product         `json:"products"`
	CustomFields []CustomField   `json:"customfields"`
	Pricings     []Pricing       `json:"product_pricings"`
	ConfigGroups []ConfigGroup   `json:"config_groups"`
	ConfigLinks  []int           `json:"config_links"`
	Advanced     []AdvancedLink  `json:"advanced"`
	Raw          json.RawMessage `json:"-"`
}

// CustomField 是上游商品的自定义字段（下单时对应请求里的 customfield[<上游字段 ID>]）。
type CustomField struct {
	ID          int    `json:"id"`
	FieldName   string `json:"fieldname"`
	Description string `json:"description"`
	FieldType   string `json:"fieldtype"`
	Required    int    `json:"required"`
	Regexpr     string `json:"regexpr"`
	ShowOrder   int    `json:"showorder"`
}

// Pricing 是上游价格表的一行（按货币 + 周期）。
type Pricing struct {
	ID       int    `json:"id"`
	Type     string `json:"type"`
	RelID    int    `json:"relid"`
	Currency int    `json:"currency"`
	Code     string `json:"code"`

	Monthly      string `json:"monthly"`
	Quarterly    string `json:"quarterly"`
	SemiAnnually string `json:"semiannually"`
	Annually     string `json:"annually"`
	Biennially   string `json:"biennially"`
	Triennially  string `json:"triennially"`
}

// ConfigGroup 是可配置项分组。
type ConfigGroup struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Options     []ConfigOption `json:"options"`
}

// ConfigOption 是可配置项。UpstreamID 是该项在上游的 ID，下单时用于 configoption[<UpstreamID>]。
type ConfigOption struct {
	ID         int                 `json:"id"`
	GroupID    int                 `json:"gid"`
	OptionName string              `json:"option_name"`
	OptionType int                 `json:"option_type"`
	UpstreamID int                 `json:"upstream_id"`
	Hidden     int                 `json:"hidden"`
	Values     []ConfigOptionValue `json:"sub"`
}

// ConfigOptionValue 是可选值。UpstreamID 为上游选项 ID；数量型（拉条型）可配置项提交数量
// （configoption[<id>] = qty，契约 12.4），取值范围为 QtyMinimum~QtyMaximum（实测每个子项都带这两个字段）。
type ConfigOptionValue struct {
	ID          int       `json:"id"`
	ConfigID    int       `json:"config_id"`
	OptionName  string    `json:"option_name"`
	UpstreamID  int       `json:"upstream_id"`
	Hidden      int       `json:"hidden"`
	QtyMinimum  int       `json:"qty_minimum"`
	QtyMaximum  int       `json:"qty_maximum"`
	Pricings    []Pricing `json:"pricings"`
}

// AdvancedLink 是「可配置项联动」规则（选项级联）。
type AdvancedLink struct {
	ID       int   `json:"id"`
	ConfigID int   `json:"config_id"`
	SubID    []int `json:"sub_id"`
}

// Stock 是 GET /cart/stock_control 的返回。
type Stock struct {
	Product StockProduct `json:"product"`
}

// StockProduct 是库存信息；StockControl=1 时 Qty 才是有效库存。
type StockProduct struct {
	ID           int `json:"id"`
	Qty          int `json:"qty"`
	StockControl int `json:"stock_control"`
	Hidden       int `json:"hidden"`
}

// OntrialMax 是 GET /cart/ontrialmax 的返回。
type OntrialMax struct {
	Product OntrialProduct `json:"product"`
}

// OntrialProduct 是试用数量与最大购买数量。
type OntrialProduct struct {
	Ontrial int `json:"ontrial"`
	Qty     int `json:"qty"`
}

// HostList 是 GET /cart/hostinfo 的返回：已购产品（主机）列表。
type HostList struct {
	Hosts    []Host `json:"hosts"`
	Currency string `json:"currency"`
}

// Host 是上游主机。字段与上游 hostInfo 接口一一对应（详见 docs/api-contract.md 8.3）。
type Host struct {
	ID          int    `json:"id"`
	ProductID   int    `json:"productid"`
	Domain      string `json:"domain"`
	DedicatedIP string `json:"dedicatedip"`
	// AssignedIPs 上游以逗号分隔字符串返回，接口层已展开为切片。
	AssignedIPs []string `json:"assignedips"`
	// CreateTime 上游存 unix 秒（实测：1791448202）。
	CreateTime int64 `json:"create_time"`
	// NextDueDate 上游存 unix 秒（实测 2026-10-08：1796718606，**不是**日期字符串）。
	NextDueDate  int64  `json:"nextduedate"`
	BillingCycle string `json:"billingcycle"`
	// BillingCycleZh / DomainStatusZh 上游可能返回字符串或 [文案, 颜色] 数组，故用 any。
	BillingCycleZh any `json:"billingcycle_zh"`
	DomainStatusZh any `json:"domainstatus_zh"`

	FirstPaymentAmount string `json:"firstpaymentamount"`
	Amount             string `json:"amount"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	InitiativeRenew    int    `json:"initiative_renew"`
	DomainStatus       string `json:"domainstatus"`

	// OptionConfig 仅在请求 all=1 时返回（可配置项及当前取值）。
	OptionConfig json.RawMessage `json:"host_option_config"`
}

// CloudOSList 是 GET /host/cloudos 的返回：商品可选的操作系统与分组。
//
// 实测（2026-10-08）：上游只返回 cloud_os，且每个系统的 group 是**分组名**（字符串），
// cloud_os_group 可能缺省，因此两者都按可缺省处理。
type CloudOSList struct {
	OS      []CloudOS      `json:"cloud_os"`
	OSGroup []CloudOSGroup `json:"cloud_os_group"`
}

// CloudOS 是一个可选操作系统；ID 用于 Reinstall 的 osID 参数。
type CloudOS struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// CloudOSGroup 是操作系统分组。实测 id 是**字符串**（即分组名，如 "CentOS"）。
type CloudOSGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Credit 是 GET /cart/credit 的返回。未携带有效 JWT 时上游把 credit 返回为 null。
type Credit struct {
	// Credit 上游以字符串返回金额（如 "100.00"），null 会解析为空串。
	Credit   string   `json:"credit"`
	Currency Currency `json:"currency"`
}

// Summary 是 GET /cart/summary 的返回：上游 API 概览。
type Summary struct {
	// APIPassword 是上游账号的 API 密钥明文，**严禁写入日志**。
	APIPassword   string `json:"api_password"`
	APICreateTime any    `json:"api_create_time"`
	APIOpen       int    `json:"api_open"`
	LockReason    int    `json:"lock_reason"`
	APILockTime   any    `json:"api_lock_time"`

	AgentCount  int `json:"agent_count"`
	HostCount   int `json:"host_count"`
	ActiveCount int `json:"active_count"`
	APICount    int `json:"api_count"`

	Ratio       string          `json:"ratio"`
	Up          int             `json:"up"`
	FormAPI     json.RawMessage `json:"form_api"`
	FreeProduct json.RawMessage `json:"free_products"`
}

// ProvisionResult 是生命周期类接口（/provision/default、/provision/button）的返回。
//
// 注意 Status 是**外层业务状态码**（200/400/406…），与内层 data.status 不是一回事：
// 开关机类接口的 data.status 是电源状态文案（如 "on"），因此这里不把内层结构直接映射到本类型，
// 需要内层字段时用 DataField 读取。
type ProvisionResult struct {
	Status  int             `json:"-"`
	Msg     string          `json:"-"`
	Data    json.RawMessage `json:"-"`
	HostIDs []int           `json:"-"`
	// CancelRequestID 是取消申请 ID（仅 /host/cancel 有值；上游放在 data 或顶层，
	// 解析时优先 data.cancel_request_id，取不到回退顶层 cancel_request_id；无申请为 0）。
	CancelRequestID int `json:"-"`
}

// DataField 读取 data 中某个字段的文本值（字符串/数字/布尔都转成字符串），字段不存在时返回空串。
//
// 例：result.DataField("status") 取电源状态（"on"/"off"）。
func (r *ProvisionResult) DataField(key string) string {
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(r.Data, &fields); err != nil {
		return ""
	}
	value, ok := fields[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		// 标准 json.Unmarshal 把数字解成 float64（如 data.cancel_request_id=433）；
		// 用最短表示输出，避免 433 变成 433.000000。
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
