package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// KeySite 是站点信息设置的键（安装向导第 5 步写入，契约 13.3）。
const KeySite = "site"

// 站点名称长度上限（按 rune 计）。
const maxSiteNameRunes = 64

// Site 是 site 设置的取值结构：站点名称 + 站点地址 + 管理员邮箱。
//
// URL 供后续生成支付回调（notify_url）等推荐地址；AdminEmail 为阶段 6 邮件通知预留。
type Site struct {
	// Name 站点名称（必填，1-64 个字符）。
	Name string `json:"name"`
	// URL 站点对外地址（可空；非空时必须是带主机名的 http(s) 地址，不带结尾斜杠）。
	URL string `json:"url"`
	// AdminEmail 管理员邮箱（可空；非空时须为合法邮箱格式）。
	AdminEmail string `json:"admin_email"`
}

// Validate 校验站点取值：名称必填且有长度上限；URL 与邮箱留空合法，非空时校验格式。
func (s Site) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: name 不能为空", ErrRule)
	}
	if utf8.RuneCountInString(s.Name) > maxSiteNameRunes {
		return fmt.Errorf("%w: name 长度不能超过 %d 个字符", ErrRule, maxSiteNameRunes)
	}
	if err := validateHTTPURL("url", s.URL); err != nil {
		return err
	}
	if s.AdminEmail != "" && !looksLikeEmail(s.AdminEmail) {
		return fmt.Errorf("%w: admin_email 格式不正确，收到 %q", ErrFormat, s.AdminEmail)
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (s Site) Encode() (string, error) { return encode(s) }

// decodeSite 解析库内 JSON；空值按未配置处理。
func decodeSite(raw string) (Site, error) {
	var value Site
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Site{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeySite, err)
	}
	value.Name = strings.TrimSpace(value.Name)
	value.URL = strings.TrimSpace(value.URL)
	value.AdminEmail = strings.TrimSpace(value.AdminEmail)
	return value, nil
}

// looksLikeEmail 做基础邮箱格式校验（与账号体系的口径一致：本地部分@主机.顶级）。
func looksLikeEmail(value string) bool {
	if strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}
	host := value[at+1:]
	dot := strings.LastIndex(host, ".")
	return dot > 0 && dot < len(host)-1
}

// SiteState 是 site 设置的读取结果。
type SiteState struct {
	Value Site
	Meta  Meta
	// Fingerprint 是库内 JSON 原文（未配置为空串）。
	Fingerprint string
}

// Site 读取 site 设置；未配置时返回零值（Name 为空）与空指纹。
func (r *Reader) Site(ctx context.Context) (SiteState, error) {
	raw, meta, err := r.raw(ctx, KeySite)
	if err != nil {
		return SiteState{}, err
	}
	value, err := decodeSite(raw)
	if err != nil {
		return SiteState{}, err
	}
	return SiteState{Value: value, Meta: meta, Fingerprint: raw}, nil
}
