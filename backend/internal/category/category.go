// Package category 实现商品两级分类的派生规则（R7，契约 10.1）。
//
// 导入时按商品名 / 上游组名自动归类，管理端可后改（products.category_source=manual 后导入不覆盖）。
// 派生是**纯函数**，只依赖商品名、上游组名、上游组 id 与商品 sort，因此重复导入必然收敛。
//
//	主分类：地域关键词（商品名优先，组名兜底，推不出归「其他」）
//	子分类：商品名「地域+区」形态优先（`香港二区 CN2 A型` → `香港二区`），否则上游组名原文
package category

import (
	"regexp"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// regions 是主分类地域关键词及其展示名，顺序即主分类排序（契约 10.1）。
//
// 香港 / 日本 / 美国 / 韩国 / 新加坡 / 台湾 / 马来 / 德国 / 国内 / 亚太，推不出归「其他」。
var regions = []string{
	"香港", "日本", "美国", "韩国", "新加坡", "台湾", "马来", "德国", "国内", "亚太",
}

// mainlandAliases 是实测数据里需要并入「国内」的大陆机房地域（2026-10-13 核对 159 个商品名）：
// 西安电信高防 / 襄阳电信高防 / 内蒙电信普防 / 绍兴电信高防 / 绍兴BGP高防 / 十堰电信高防
// 共 5 个上游组、40 个商品。关键词表本身不含这些地域，不并入则会全部落进「其他」。
var mainlandAliases = []string{"西安", "襄阳", "内蒙", "内蒙古", "绍兴", "十堰", "北京", "上海", "广州", "深圳"}

// fallbackRegion 是推不出地域时的主分类名。
const fallbackRegion = "其他"

// zonePattern 匹配商品名开头的「地域 + 区段」形态（`香港二区` / `美国一区`）。
// 区段是阿拉伯数字或中文数字后跟「区」；`美国家宽ISP A区`、`香港-E3-1` 不匹配（符合预期）。
var zonePattern = regexp.MustCompile(`^(香港|日本|美国|韩国|新加坡|台湾|马来|德国|国内|亚太)(?:[0-9]+|[一二三四五六七八九十百]+)区`)

// Derive 按商品名与上游组名派生两级归属。
//
// upstreamGroupID 只用于生成子分类的稳定键（组名被管理员改名也不漂移）；
// productSort 作为子分类首次创建时的排序（保持与商品顺序一致）。
func Derive(productName, groupName string, upstreamGroupID, productSort int) model.ProductCategoryInput {
	region := regionOf(productName)
	if region == "" {
		region = regionOf(groupName)
	}
	if region == "" {
		region = fallbackRegion
	}

	subName, subKey := subOf(productName, groupName, upstreamGroupID)
	return model.ProductCategoryInput{
		MainKey:  "m:" + region,
		MainName: region,
		SubKey:   subKey,
		SubName:  subName,
		MainSort: mainSort(region),
		SubSort:  productSort,
	}
}

// regionOf 在文本里按优先序找地域关键词；找不到返回空串。
func regionOf(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	for _, region := range regions {
		if strings.Contains(trimmed, region) {
			return region
		}
	}
	for _, alias := range mainlandAliases {
		if strings.Contains(trimmed, alias) {
			return "国内"
		}
	}
	return ""
}

// subOf 返回子分类展示名与稳定键：
// 商品名命中「地域+区」→ `香港二区`（键 `z:香港二区`）；否则用上游组名原文（键 `g:<上游组 id>`）。
func subOf(productName, groupName string, upstreamGroupID int) (name string, key string) {
	if match := zonePattern.FindString(strings.TrimSpace(productName)); match != "" {
		return match, "z:" + match
	}
	// 组名兜底（契约 10.1）：展示名在首次创建时写入，之后导入永不改写。
	// 稳定键用上游组 id 而非组名，组名被管理员改名也不会漂移。
	return strings.TrimSpace(groupName), "g:" + itoa(upstreamGroupID)
}

// mainSort 返回主分类的排序（regions 下标）；「其他」排最后。
func mainSort(region string) int {
	for i, item := range regions {
		if item == region {
			return i
		}
	}
	return len(regions)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}
