package settings

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// KeyInstalled 是「系统已安装」标记的键（契约 13.1）：安装向导完成时写入，
// 存在即视为已安装（安装模式永久关闭）；不存在时还要结合 admins 表是否为空判定。
const KeyInstalled = "installed"

// KeyInstallProgress 是「安装进行中」进度标记的键（契约 13.2）：
// 向导改动了数据库（建表 / 建管理员 / 写站点信息）后写入，完成时删除。
// 作用是把「向导走了一半的库」与「存量开发库」区分开——两者在数据库内容上完全一样
// （表齐全、只有迁移 0003 的默认管理员、无 installed 标记），
// 若不做区分，向导中途重启进程会导致库被判为存量库、自动补标记并关闭向导。
const KeyInstallProgress = "install.progress"

// 安装进度阶段取值。
const (
	// StageInitialized 已建表。
	StageInitialized = "initialized"
	// StageAdmin 已建管理员账号。
	StageAdmin = "admin"
	// StageSite 已写站点信息。
	StageSite = "site"
)

// InstallProgress 是安装进度标记的取值结构。
type InstallProgress struct {
	// Stage 当前阶段：initialized / admin / site。
	Stage string `json:"stage"`
	// At 最近一次更新时间（RFC3339 UTC）。
	At string `json:"at"`
}

// NewInstallProgress 构造安装进度标记（时间取当前 UTC）。
func NewInstallProgress(stage string) InstallProgress {
	return InstallProgress{Stage: stage, At: time.Now().UTC().Format(time.RFC3339)}
}

// Encode 返回落库用的 JSON 文本。
func (p InstallProgress) Encode() (string, error) { return encode(p) }

// MarkerVersion 是当前安装流程版本（写入 installed 标记，便于将来识别安装来源）。
const MarkerVersion = 1

// Installed 是 installed 标记的取值结构。
type Installed struct {
	// At 安装完成时间（RFC3339 UTC）。
	At string `json:"at"`
	// Version 安装流程版本。
	Version int `json:"version"`
	// Source 安装来源：wizard＝浏览器安装向导；auto＝场景 5 的存量库自动补写。
	Source string `json:"source"`
}

// 安装来源取值。
const (
	// SourceWizard 由安装向导写入。
	SourceWizard = "wizard"
	// SourceAuto 由启动时的存量库自动补写（场景 5）。
	SourceAuto = "auto"
)

// NewInstalled 构造安装标记（时间取当前 UTC）。
func NewInstalled(source string) Installed {
	return Installed{
		At:      time.Now().UTC().Format(time.RFC3339),
		Version: MarkerVersion,
		Source:  source,
	}
}

// Encode 返回落库用的 JSON 文本。
func (i Installed) Encode() (string, error) { return encode(i) }

// ParseInstalled 解析库内的安装标记；内容为空或非法 JSON 时返回零值与错误。
func ParseInstalled(raw string) (Installed, error) {
	var value Installed
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Installed{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyInstalled, err)
	}
	return value, nil
}
