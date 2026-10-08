package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SaveInput 描述要写回配置文件的变更（空字段表示不修改）。
//
// 只承载安装向导需要写入的两项部署级参数：数据库连接串与 JWT 密钥。
// 其余「用户可设置」的内容一律走 settings 表，不进配置文件（契约 12.1）。
type SaveInput struct {
	// DSN 非空时更新 database.dsn。
	DSN string
	// JWTSecret 非空时更新 jwt.secret。
	JWTSecret string
}

// ResolveWritePath 返回配置文件的「生效路径」：优先当前已加载路径（cfg.SourcePath），
// 为空时取运行目录下的 config.yaml（DefaultSearchPaths 的第一项）。返回值是绝对路径。
func ResolveWritePath(sourcePath string) (string, error) {
	path := strings.TrimSpace(sourcePath)
	if path == "" {
		path = DefaultSearchPaths[0]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析配置文件路径失败: %w", err)
	}
	return abs, nil
}

// SaveMerged 把变更**合并**写回配置文件（契约 13.4）：
//   - 文件不存在（或为空）时按缺省结构新建，并带上说明性文件头注释；
//   - 文件已存在时保留全部既有键、注释与书写顺序，只更新 database.dsn / jwt.secret，
//     并补齐缺失的缺省节与缺省键；
//   - 写入采用「同目录临时文件 + 原子替换」，权限 0600（文件含数据库密码与 JWT 密钥）。
//
// 返回实际写入的绝对路径。调用方负责确保参数不落日志（本函数不打印任何内容）。
func SaveMerged(path string, input SaveInput) (string, error) {
	if strings.TrimSpace(input.DSN) == "" && strings.TrimSpace(input.JWTSecret) == "" {
		return "", errors.New("没有需要写入的配置项")
	}

	abs, err := ResolveWritePath(path)
	if err != nil {
		return "", err
	}

	doc, err := loadDocument(abs)
	if err != nil {
		return "", err
	}
	if err := mergeDocument(doc, input); err != nil {
		return "", err
	}

	out, err := marshalDocument(doc)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomically(abs, out); err != nil {
		return "", err
	}
	return abs, nil
}

// CheckWritable 实际试写（配置文件本身与所在目录），判断安装向导能否写入生效路径。
// 返回 nil 表示可写。
func CheckWritable(path string) error {
	abs, err := ResolveWritePath(path)
	if err != nil {
		return err
	}

	if _, err := os.Stat(abs); err == nil {
		// 文件已存在：以「不截断」的方式打开一次，确认可写（Windows 下也可捕获只读属性）。
		file, err := os.OpenFile(abs, os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("配置文件 %s 不可写（请检查文件权限或只读属性）: %w", abs, err)
		}
		_ = file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查配置文件 %s 失败: %w", abs, err)
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("配置目录 %s 不可用: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".lyidc-write-check-*")
	if err != nil {
		return fmt.Errorf("配置目录 %s 不可写: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// loadDocument 读取并解析既有配置文件；文件不存在或为空时返回缺省文档。
func loadDocument(path string) (*yaml.Node, error) {
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(raw))) == 0 {
			return newDocument(), nil
		}
		doc := &yaml.Node{}
		if err := yaml.Unmarshal(raw, doc); err != nil {
			return nil, fmt.Errorf("解析既有配置文件 %s 失败（请先修正语法或删除该文件）: %w", path, err)
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
			return nil, fmt.Errorf("配置文件 %s 的根结构不是键值映射，无法合并写入", path)
		}
		return doc, nil
	case errors.Is(err, os.ErrNotExist):
		return newDocument(), nil
	default:
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}
}

// documentHeader 是新建配置文件的文件头注释（与既有文件的注释风格一致）。
const documentHeader = `Lyidc_OEM 后端配置（由浏览器安装向导生成，也可手工修改后重启服务生效）。

本文件只保留**部署级参数**（监听地址、数据库连接、JWT 密钥、日志等级）；
所有「用户可设置」的内容由后台管理设置承载（settings 表 + 管理端接口，改完立即生效）：
  PUT /api/v1/admin/settings/payment/epay   易支付参数
  PUT /api/v1/admin/settings/upstream       上游对接参数
本文件含数据库密码与 JWT 密钥，请勿提交到版本库。契约见 docs/api-contract.md 13.4。`

// newDocument 构造缺省配置文件文档（键顺序与 backend/config.example.yaml 一致）。
func newDocument() *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: documentHeader}
	root.Content = append(root.Content,
		key("server", "HTTP 服务监听地址与运行模式（debug/release/test）。"),
		mapping(
			key("addr", "监听地址 host:port。"), quoted(DefaultAddr),
			key("mode", ""), quoted(DefaultMode),
		),

		key("database", "MySQL 连接（部署级参数；密码只存在本文件与本进程内存）。"),
		mapping(
			key("dsn", "连接串，安装向导写入；multiStatements 供迁移使用。"), quoted(""),
			key("max_open_conns", ""), integer(DefaultMaxOpenConns),
			key("max_idle_conns", ""), integer(DefaultMaxIdleConns),
			key("conn_max_lifetime", "连接最长存活时间，支持 30m / 1h 写法。"), quoted("1h"),
		),

		key("jwt", "JWT（HS256）签名密钥：会员与管理员 token 共用，用 aud 区分。"),
		mapping(
			key("secret", "由安装向导随机生成；泄露即可伪造任意账号的 token。"), quoted(""),
			key("expire_hours", "token 有效期（小时），缺省 168 = 7 天，取值 1-8760。"), integer(DefaultJWTExpireHours),
		),

		key("log", ""),
		mapping(
			key("level", "日志级别：debug | info | warn | error。"), quoted(DefaultLogLevel),
			key("format", "日志格式：text | json。"), quoted(DefaultLogFormat),
		),
	)
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
}

// mergeDocument 把变更合并进文档，并补齐缺失的缺省节与缺省键。
func mergeDocument(doc *yaml.Node, input SaveInput) error {
	root := doc.Content[0]

	server, err := ensureSection(root, "server")
	if err != nil {
		return err
	}
	if err := ensureScalar(server, "addr", DefaultAddr); err != nil {
		return err
	}
	if err := ensureScalar(server, "mode", DefaultMode); err != nil {
		return err
	}

	database, err := ensureSection(root, "database")
	if err != nil {
		return err
	}
	if dsn := strings.TrimSpace(input.DSN); dsn != "" {
		if err := setScalar(database, "dsn", dsn); err != nil {
			return err
		}
	}
	if err := ensureScalar(database, "dsn", DefaultDSN); err != nil {
		return err
	}
	if err := ensureInt(database, "max_open_conns", DefaultMaxOpenConns); err != nil {
		return err
	}
	if err := ensureInt(database, "max_idle_conns", DefaultMaxIdleConns); err != nil {
		return err
	}
	if err := ensureScalar(database, "conn_max_lifetime", "1h"); err != nil {
		return err
	}

	jwtSection, err := ensureSection(root, "jwt")
	if err != nil {
		return err
	}
	if secret := strings.TrimSpace(input.JWTSecret); secret != "" {
		if err := setScalar(jwtSection, "secret", secret); err != nil {
			return err
		}
	}
	if err := ensureScalar(jwtSection, "secret", DefaultJWTSecret); err != nil {
		return err
	}
	if err := ensureInt(jwtSection, "expire_hours", DefaultJWTExpireHours); err != nil {
		return err
	}

	logSection, err := ensureSection(root, "log")
	if err != nil {
		return err
	}
	if err := ensureScalar(logSection, "level", DefaultLogLevel); err != nil {
		return err
	}
	return ensureScalar(logSection, "format", DefaultLogFormat)
}

// key 构造映射键节点（comment 非空时作为该键上方的注释）。
func key(name, comment string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name, HeadComment: comment}
}

// quoted 构造双引号风格的字符串标量节点（与项目既有配置文件的书写风格一致）。
func quoted(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: yaml.DoubleQuotedStyle}
}

// integer 构造整型标量节点。
func integer(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}

// mapping 构造映射值节点（Content 为「键, 值, 键, 值…」成对排列）。
func mapping(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: pairs}
}

// ensureSection 在根映射中查找节；不存在时以空映射补齐，返回该节的映射节点。
func ensureSection(root *yaml.Node, name string) (*yaml.Node, error) {
	if index := indexOfKey(root, name); index >= 0 {
		value := root.Content[index+1]
		if value.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("配置项 %s 不是键值映射，无法合并写入（请先修正配置文件）", name)
		}
		return value, nil
	}

	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, key(name, ""), value)
	return value, nil
}

// ensureScalar 在映射中确保键存在；键已存在时保持原值不动。
func ensureScalar(mapping *yaml.Node, name, defaultValue string) error {
	if indexOfKey(mapping, name) >= 0 {
		return nil
	}
	return setScalar(mapping, name, defaultValue)
}

// ensureInt 在映射中确保整型键存在；键已存在时保持原值不动。
func ensureInt(mapping *yaml.Node, name string, defaultValue int) error {
	index := indexOfKey(mapping, name)
	if index < 0 {
		mapping.Content = append(mapping.Content, key(name, ""), integer(defaultValue))
		return nil
	}
	if mapping.Content[index+1].Kind != yaml.ScalarNode {
		return fmt.Errorf("配置项 %s 不是标量，无法读取（请先修正配置文件）", name)
	}
	return nil
}

// setScalar 在映射中写入（或覆盖）一个字符串标量键。
func setScalar(mapping *yaml.Node, name, value string) error {
	index := indexOfKey(mapping, name)
	if index < 0 {
		mapping.Content = append(mapping.Content, key(name, ""), quoted(value))
		return nil
	}

	current := mapping.Content[index+1]
	if current.Kind != yaml.ScalarNode {
		return fmt.Errorf("配置项 %s 不是标量，无法写入（请先修正配置文件）", name)
	}
	// 保留既有节点的注释与引号风格，只替换取值。
	current.Tag = "!!str"
	current.Value = value
	return nil
}

// indexOfKey 返回映射中键名对应的下标（未找到返回 -1）。
func indexOfKey(mapping *yaml.Node, name string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return i
		}
	}
	return -1
}

// marshalDocument 序列化文档：缩进固定 2 空格，与项目既有配置文件风格一致。
func marshalDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}
	return buf.Bytes(), nil
}

// writeFileAtomically 先写同目录临时文件，再原子替换目标文件（权限 0600）。
func writeFileAtomically(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".lyidc-config-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时配置文件失败（目录 %s 可能不可写）: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("设置配置文件权限失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时配置文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换配置文件 %s 失败: %w", path, err)
	}
	return nil
}
