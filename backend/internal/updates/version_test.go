package updates

import "testing"

func TestParseVersion(t *testing.T) {
	valid := map[string]semver{
		"v1.0.0":            {major: 1},
		"1.0.0":             {major: 1},
		"v0.10.0":           {minor: 10},
		"v1.2.3-rc.1":       {major: 1, minor: 2, patch: 3, pre: []string{"rc", "1"}},
		"v1.2.3+build.5":    {major: 1, minor: 2, patch: 3},
		"v1.2.3-alpha.beta": {major: 1, minor: 2, patch: 3, pre: []string{"alpha", "beta"}},
	}
	for raw, want := range valid {
		got, ok := parseVersion(raw)
		if !ok {
			t.Errorf("parseVersion(%q) 应当成功", raw)
			continue
		}
		if compareVersions(got, want) != 0 || len(got.pre) != len(want.pre) {
			t.Errorf("parseVersion(%q) = %+v，期望 %+v", raw, got, want)
		}
	}

	invalid := []string{
		"", "dev", "v1", "v1.2", "v1.2.3.4", "v01.2.3", "v1.2.x",
		"v1.2.3-", "v1.2.3-rc..1", "v1.2.3-rc_1", "v-1.0.0", "v1.2.3-01", "release-2",
	}
	for _, raw := range invalid {
		if _, ok := parseVersion(raw); ok {
			t.Errorf("parseVersion(%q) 应当失败", raw)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.1.0", -1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.1.0", "v1.0.0", 1},
		// 数字段按数值比较，不是字符串比较（0.10.0 > 0.9.0）。
		{"v0.10.0", "v0.9.0", 1},
		// 正式版高于同号的预发布。
		{"v1.1.0", "v1.1.0-rc1", 1},
		{"v1.1.0-rc1", "v1.1.0", -1},
		// semver 2.0.0 预发布标识符规则：数字段按数值、长度多者高、数字段低于非数字段。
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.0-alpha.1", "v1.0.0-alpha.beta", -1},
		{"v1.0.0-beta", "v1.0.0-rc.1", -1},
		// 构建元数据不参与比较。
		{"v1.0.0+a", "v1.0.0+b", 0},
	}
	for _, c := range cases {
		a, okA := parseVersion(c.a)
		b, okB := parseVersion(c.b)
		if !okA || !okB {
			t.Fatalf("测试数据解析失败: %q / %q", c.a, c.b)
		}
		if got := compareVersions(a, b); sign(got) != c.want {
			t.Errorf("compareVersions(%q, %q) = %d，期望符号 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareLatest(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		latest    string
		compare   string
		hasUpdate bool
	}{
		{"旧版本有新版", "v1.0.0", "v1.1.0", compareOK, true},
		{"版本相等不提示", "v1.1.0", "v1.1.0", compareOK, false},
		{"当前比最新更新不提示", "v1.2.0", "v1.1.0", compareOK, false},
		{"数字段按数值", "v0.9.0", "v0.10.0", compareOK, true},
		{"预发布当前版本可升到正式版", "v1.1.0-rc1", "v1.1.0", compareOK, true},
		{"最新版带预发布后缀不提示", "v1.0.0", "v1.1.0-rc1", compareOK, false},
		{"开发构建不比较", "dev", "v1.1.0", compareDevBuild, false},
		{"dev-sha 也是开发构建", "dev-a1b2c3d", "v1.1.0", compareDevBuild, false},
		{"当前版本不可解析", "garbage", "v1.0.0", compareUnknown, false},
		{"尚无最新版本", "v1.0.0", "", compareUnknown, false},
		{"最新版本不可解析", "v1.0.0", "release-2", compareUnknown, false},
	}
	for _, c := range cases {
		got := compareLatest(c.current, c.latest)
		if got.compare != c.compare || got.hasUpdate != c.hasUpdate {
			t.Errorf("%s：compareLatest(%q, %q) = %+v，期望 compare=%s has_update=%t",
				c.name, c.current, c.latest, got, c.compare, c.hasUpdate)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	for _, version := range []string{"dev", "dev-a1b2c3d"} {
		if !isDevBuild(version) {
			t.Errorf("isDevBuild(%q) 应为 true", version)
		}
	}
	for _, version := range []string{"v1.0.0", "devops", "", "unknown"} {
		if isDevBuild(version) {
			t.Errorf("isDevBuild(%q) 应为 false", version)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
