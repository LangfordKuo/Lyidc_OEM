package category

import "testing"

// TestDerive_ZoneFromProductName 商品名「地域+区」形态 → 子分类取该形态（契约 10.1）。
func TestDerive_ZoneFromProductName(t *testing.T) {
	cases := []struct {
		product string
		group   string
		groupID int
		main    string
		sub     string
		subKey  string
	}{
		{"香港二区 CN2 A型", "二区 CN2标准性能", 1, "香港", "香港二区", "z:香港二区"},
		{"香港三区 CN2 F型", "三区 CN2高性能", 37, "香港", "香港三区", "z:香港三区"},
		{"美国一区 Kurun A型", "一区 Kurun精品", 4, "美国", "美国一区", "z:美国一区"},
		// 跨上游组并入同一子分类（组名是「传家宝」，但商品名有区段形态）。
		{"美国一区 Cera 传家宝", "传家宝", 5, "美国", "美国一区", "z:美国一区"},
		{"美国一区 Cera C型 黑五特惠", "美国一区 CERA高防", 8, "美国", "美国一区", "z:美国一区"},
	}
	for _, tc := range cases {
		got := Derive(tc.product, tc.group, tc.groupID, 0)
		if got.MainName != tc.main || got.SubName != tc.sub || got.SubKey != tc.subKey {
			t.Fatalf("Derive(%q,%q) = 主=%q 子=%q/%q, want 主=%q 子=%q/%q",
				tc.product, tc.group, got.MainName, got.SubName, got.SubKey, tc.main, tc.sub, tc.subKey)
		}
		if got.MainKey != "m:"+tc.main {
			t.Fatalf("MainKey = %q, want m:%s", got.MainKey, tc.main)
		}
	}
}

// TestDerive_GroupNameFallback 无区段形态 → 子分类取上游组名原文，稳定键按上游组 id。
func TestDerive_GroupNameFallback(t *testing.T) {
	cases := []struct {
		product string
		group   string
		groupID int
		main    string
		sub     string
		subKey  string
	}{
		{"香港-E3-1", "香港精品网", 23, "香港", "香港精品网", "g:23"},
		{"美国-DD-1", "美国普防", 24, "美国", "美国普防", "g:24"},
		{"台湾-01", "台湾精品网", 29, "台湾", "台湾精品网", "g:29"},
		{"日本BGP云主机 2核 2GB A型", "日本BGP直连", 46, "日本", "日本BGP直连", "g:46"},
		{"亚太高防型-体验版", "亚太高防CDN", 11, "亚太", "亚太高防CDN", "g:11"},
		{"国内CDN-体验版", "国内高防CDN", 15, "国内", "国内高防CDN", "g:15"},
	}
	for _, tc := range cases {
		got := Derive(tc.product, tc.group, tc.groupID, 0)
		if got.MainName != tc.main || got.SubName != tc.sub || got.SubKey != tc.subKey {
			t.Fatalf("Derive(%q,%q) = 主=%q 子=%q/%q, want 主=%q 子=%q/%q",
				tc.product, tc.group, got.MainName, got.SubName, got.SubKey, tc.main, tc.sub, tc.subKey)
		}
	}
}

// TestDerive_RegionFromGroupName 商品名推不出地域时用组名兜底。
func TestDerive_RegionFromGroupName(t *testing.T) {
	// `USA-AK-1` 是英文缩写，地域只能从组名「美国大带宽」推出。
	got := Derive("USA-AK-1", "美国大带宽", 25, 0)
	if got.MainName != "美国" {
		t.Fatalf("主分类 = %q, want 美国", got.MainName)
	}
	if got.SubName != "美国大带宽" || got.SubKey != "g:25" {
		t.Fatalf("子分类 = %q/%q, want 美国大带宽/g:25", got.SubName, got.SubKey)
	}

	// `MLXY-DD-1` 同理，从组名「马来大陆优化」推出「马来」。
	got = Derive("MLXY-DD-1", "马来大陆优化", 31, 0)
	if got.MainName != "马来" || got.SubName != "马来大陆优化" {
		t.Fatalf("马来归类 = %+v", got)
	}
}

// TestDerive_MainlandAliases 大陆机房地域并入「国内」（实测别名表）。
func TestDerive_MainlandAliases(t *testing.T) {
	cases := []struct {
		product string
		group   string
	}{
		{"西安电信 2-2 A型", "西安电信高防"},
		{"襄阳云服务器-A型", "襄阳电信高防"},
		{"内蒙古云服务器-A型", "内蒙电信普防"},
		{"绍兴电信云服务器-A型", "绍兴电信高防"},
		{"绍兴BGP云服务器-A型", "绍兴BGP高防"},
		{"十堰电信云主机 4核 4GB A型", "十堰电信高防"},
	}
	for _, tc := range cases {
		got := Derive(tc.product, tc.group, 1, 0)
		if got.MainName != "国内" {
			t.Fatalf("Derive(%q) 主分类 = %q, want 国内", tc.product, got.MainName)
		}
		if got.MainKey != "m:国内" {
			t.Fatalf("MainKey = %q, want m:国内", got.MainKey)
		}
	}
}

// TestDerive_FallbackOther 推不出地域 → 「其他」。
func TestDerive_FallbackOther(t *testing.T) {
	got := Derive("代理级别一", "代理折扣", 3, 0)
	if got.MainName != "其他" || got.MainKey != "m:其他" {
		t.Fatalf("代理折扣归类 = %+v, want 主分类「其他」", got)
	}
	if got.SubName != "代理折扣" || got.SubKey != "g:3" {
		t.Fatalf("子分类 = %q/%q, want 代理折扣/g:3", got.SubName, got.SubKey)
	}
}

// TestDerive_ZonePatternDoesNotOverMatch 区段形态必须是「地域+数字+区」，
// `美国家宽ISP A区`、`香港-E3-1` 这类不能误判成区段。
func TestDerive_ZonePatternDoesNotOverMatch(t *testing.T) {
	for _, product := range []string{
		"美国家宽ISP A区 2核 2GB 500Mbps",
		"香港-E3-1",
		"日本BGP云主机 2核 2GB A型",
		"美国双ISP住宅云主机◆2核 2GB A型",
	} {
		got := Derive(product, "某组", 42, 0)
		if got.SubKey != "g:42" {
			t.Fatalf("Derive(%q) 子分类键 = %q, want g:42（不应识别为区段形态）", product, got.SubKey)
		}
	}
}

// TestDerive_StableKeys 键只由稳定输入决定：商品改名/组名被改不影响区段键，
// 上游组 id 变化才影响 g: 键。
func TestDerive_StableKeys(t *testing.T) {
	a := Derive("香港二区 CN2 A型", "二区 CN2标准性能", 1, 0)
	b := Derive("香港二区 CN2 A型（改名后）", "管理员改过的组名", 1, 0)
	if a.SubKey != b.SubKey {
		t.Fatalf("区段键不应随文案漂移: %q vs %q", a.SubKey, b.SubKey)
	}
	if a.SubName != "香港二区" {
		t.Fatalf("子分类名 = %q, want 香港二区", a.SubName)
	}

	// 组名兜底的键按上游组 id：组名改了键不变（名字只在首次创建写入）。
	c := Derive("某商品", "原组名", 42, 0)
	d := Derive("某商品", "新组名", 42, 0)
	if c.SubKey != d.SubKey || c.SubKey != "g:42" {
		t.Fatalf("组兜底键应为 g:42: %q vs %q", c.SubKey, d.SubKey)
	}
}

// TestDerive_MainSort 主分类排序按地域固定序，「其他」最后。
func TestDerive_MainSort(t *testing.T) {
	hk := Derive("香港二区 A型", "g", 1, 0)
	jp := Derive("日本BGP云主机", "g", 2, 0)
	other := Derive("代理级别一", "代理折扣", 3, 0)
	if !(hk.MainSort < jp.MainSort && jp.MainSort < other.MainSort) {
		t.Fatalf("主分类排序不对: 香港=%d 日本=%d 其他=%d", hk.MainSort, jp.MainSort, other.MainSort)
	}
	if other.MainSort != len(regions) {
		t.Fatalf("「其他」排序 = %d, want %d", other.MainSort, len(regions))
	}
}

// TestDerive_RealCorpus 核对开发库 159 个商品名的归类结果（2026-10-13 全量核对）。
// 只断言有代表性的锚点，覆盖 11 个主分类与两种子分类派生路径。
func TestDerive_RealCorpus(t *testing.T) {
	cases := []struct {
		product string
		group   string
		groupID int
		main    string
		sub     string
	}{
		{"香港二区 CN2 A型", "二区 CN2标准性能", 1, "香港", "香港二区"},
		{"香港三区 CN2 A型", "三区 CN2高性能", 37, "香港", "香港三区"},
		{"香港-E3-1", "香港精品网", 23, "香港", "香港精品网"},
		{"美国一区 Kurun A型", "一区 Kurun精品", 4, "美国", "美国一区"},
		{"美国一区 Cera 传家宝", "传家宝", 5, "美国", "美国一区"},
		{"美国CDN-体验版", "美国高防CDN", 12, "美国", "美国高防CDN"},
		{"美国-DD-1", "美国普防", 24, "美国", "美国普防"},
		{"USA-AK-1", "美国大带宽", 25, "美国", "美国大带宽"},
		{"美国双ISP住宅云主机◆2核 2GB A型", "美国家宽ISP A区", 32, "美国", "美国家宽ISP A区"},
		{"德国双ISP住宅云主机◆2核 2GB A型", "德国家宽ISP A区", 34, "德国", "德国家宽ISP A区"},
		{"台湾-01", "台湾精品网", 29, "台湾", "台湾精品网"},
		{"MLXY-DD-1", "马来大陆优化", 31, "马来", "马来大陆优化"},
		{"日本BGP云主机 2核 2GB A型", "日本BGP直连", 46, "日本", "日本BGP直连"},
		{"亚太高防型-体验版", "亚太高防CDN", 11, "亚太", "亚太高防CDN"},
		{"亚太普防型-体验版", "亚太普防CDN", 17, "亚太", "亚太普防CDN"},
		{"国内CDN-体验版", "国内高防CDN", 15, "国内", "国内高防CDN"},
		{"全球终极版", "国内高防CDN", 15, "国内", "国内高防CDN"},
		{"西安电信 2-2 A型", "西安电信高防", 22, "国内", "西安电信高防"},
		{"襄阳云服务器-A型", "襄阳电信高防", 38, "国内", "襄阳电信高防"},
		{"内蒙古云服务器-A型", "内蒙电信普防", 39, "国内", "内蒙电信普防"},
		{"绍兴电信云服务器-A型", "绍兴电信高防", 43, "国内", "绍兴电信高防"},
		{"绍兴BGP云服务器-A型", "绍兴BGP高防", 44, "国内", "绍兴BGP高防"},
		{"十堰电信云主机 4核 4GB A型", "十堰电信高防", 45, "国内", "十堰电信高防"},
		{"代理级别一", "代理折扣", 3, "其他", "代理折扣"},
	}
	for _, tc := range cases {
		got := Derive(tc.product, tc.group, tc.groupID, 0)
		if got.MainName != tc.main || got.SubName != tc.sub {
			t.Fatalf("Derive(%q,%q) = 主=%q 子=%q, want 主=%q 子=%q",
				tc.product, tc.group, got.MainName, got.SubName, tc.main, tc.sub)
		}
	}
}
