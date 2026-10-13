// Package updates 实现「系统版本与更新检查」（契约 19，R13-A）：语义化版本比较、GitHub Releases
// 最新正式版的获取、更新检查缓存与定时刷新。本包只做检测与展示，不下载、不替换、不重启（待 R13-B）。
package updates

import (
	"strconv"
	"strings"
)

// 比较结论（契约 19.3 的 compare 字段）。
const (
	compareOK         = "ok"
	compareDevBuild   = "dev_build"
	compareUnknown    = "unknown"
	devVersionLiteral = "dev"
	devVersionPrefix  = "dev-"
)

// semver 是解析后的 vMAJOR.MINOR.PATCH[-预发布]；构建元数据忽略（契约 19.2）。
type semver struct {
	major, minor, patch uint64
	// pre 是预发布标识符列表，空表示正式版。
	pre []string
}

// isDevBuild 判断是否为开发构建：缺省 dev，或 CI 注入的 dev-<sha7>（契约 19.1）。
func isDevBuild(version string) bool {
	return version == devVersionLiteral || strings.HasPrefix(version, devVersionPrefix)
}

// parseVersion 解析版本号，允许有或没有 v 前缀；无法解析返回 false。
func parseVersion(raw string) (semver, bool) {
	text := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if i := strings.IndexByte(text, '+'); i >= 0 {
		text = text[:i]
	}
	core, pre, hasPre := strings.Cut(text, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var nums [3]uint64
	for i, part := range parts {
		if !isNumeric(part) {
			return semver{}, false
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return semver{}, false
		}
		nums[i] = n
	}

	version := semver{major: nums[0], minor: nums[1], patch: nums[2]}
	if hasPre {
		identifiers := strings.Split(pre, ".")
		for _, identifier := range identifiers {
			if !isPrereleaseIdentifier(identifier) {
				return semver{}, false
			}
		}
		version.pre = identifiers
	}
	return version, true
}

// isNumeric 判断是否为非空十进制数字串，且没有前导零（"0" 本身合法）。
func isNumeric(text string) bool {
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return false
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isPrereleaseIdentifier 判断预发布标识符是否合法：非空，只含 [0-9A-Za-z-]，数字段不得有前导零。
func isPrereleaseIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}
	allDigits := true
	for _, c := range identifier {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
			allDigits = false
		default:
			return false
		}
	}
	if allDigits {
		return isNumeric(identifier)
	}
	return true
}

// compareVersions 按 semver 2.0.0 比较两个版本：返回负数、0 或正数。
func compareVersions(a, b semver) int {
	for _, pair := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return comparePrerelease(a.pre, b.pre)
}

// comparePrerelease 比较预发布部分：正式版高于同号的任何预发布。
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(a), len(b))
}

// compareIdentifier 比较单个预发布标识符：数字段按数值，非数字段按字典序，数字段低于非数字段。
func compareIdentifier(a, b string) int {
	aNumeric, bNumeric := isNumeric(a), isNumeric(b)
	switch {
	case aNumeric && bNumeric:
		// 无前导零时，长度更长的数字更大；等长再按字典序。
		if c := compareInt(len(a), len(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// versionComparison 是当前版本与最新正式版的比较结论。
type versionComparison struct {
	// compare 为 ok / dev_build / unknown。
	compare string
	// hasUpdate 只有 compare 为 ok 且最新版是真正更新的正式版时为 true。
	hasUpdate bool
}

// compareLatest 按契约 19.2 得出比较结论；latest 为空表示尚未获取到最新版本。
func compareLatest(current, latest string) versionComparison {
	if isDevBuild(current) {
		return versionComparison{compare: compareDevBuild}
	}
	cur, ok := parseVersion(current)
	if !ok || latest == "" {
		return versionComparison{compare: compareUnknown}
	}
	lat, ok := parseVersion(latest)
	if !ok {
		return versionComparison{compare: compareUnknown}
	}
	// 最新版仍带预发布后缀属于仓库侧异常：不提示为「有新版本」（契约 19.2 第 3 条）。
	if len(lat.pre) > 0 {
		return versionComparison{compare: compareOK}
	}
	return versionComparison{compare: compareOK, hasUpdate: compareVersions(lat, cur) > 0}
}
