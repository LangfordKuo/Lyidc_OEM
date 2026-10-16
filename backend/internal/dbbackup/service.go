// Package dbbackup 实现「数据库备份与恢复」（R14，契约 21）：
// **纯 Go** 逐表导出可重放的 SQL 文本（不依赖 mysqldump 或任何外部二进制），
// 以及覆盖式恢复（DROP + CREATE + INSERT）。
//
// 三条硬边界（契约 21.1 / 21.4）：
//   - **绝不提供下载**：本包与接口层都只输出元信息（时间 / 大小 / 版本 / 操作人 / 文件名），
//     没有任何函数把备份文件内容回给调用方，也没有任何函数把内容写进日志；
//   - **备份目录绝不在 Web 根内**：缺省 `<安装目录>/backups`（与 `dist/` 同级），
//     设置项解析结果落在 `<安装目录>/dist` 内时一律拒绝使用（启动、每次使用与**真正写文件之前**
//     都校验）；判定前两侧路径先经 normalizeDirPath 归一（`\\?\` 扩展长度路径等 Windows
//     等价写法归一到同一形态），设备路径 / 卷路径 / 空段等无法可靠判定的形态一律拒绝；
//   - **接口只接受备份 ID**：ID 必须匹配 `lyidc-backup-<UTC 时间戳>-<8 位随机串>`，
//     服务端据此在自己的白名单（备份目录）里解析路径，并复核结果仍在目录内且不是符号链接。
//
// 内容含敏感数据（管理员密码哈希在内），因此错误信息、日志与接口响应都只带元信息。
package dbbackup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// 目录与文件名约定（契约 21.4）。
const (
	// DefaultDirName 是缺省备份目录名（与 dist/ 同级，绝不在 Web 根内）。
	DefaultDirName = "backups"
	// WebRootName 是 Web 根目录名（一键升级替换的前端产物目录，契约 20.1）。
	WebRootName = "dist"
	// FileExt 是备份文件扩展名。
	FileExt = ".sql"
	// MetaExt 是元信息 sidecar 的扩展名（契约 21.2：元信息随文件存放，恢复覆盖库后仍可读）。
	MetaExt = ".sql.json"
	// FilePrefix 是备份文件名前缀。
	FilePrefix = "lyidc-backup-"
)

// 哨兵错误（接口层据此映射错误码，契约 21.3）。
var (
	// ErrNotFound 备份不存在：ID 形状非法、不在白名单、元数据缺失或文件已删。
	ErrNotFound = errors.New("备份不存在或备份文件已删除")
	// ErrDirUnusable 备份目录不可用：配置非法（落在 Web 根内）、不可写或不是目录。
	ErrDirUnusable = errors.New("备份目录不可用")
	// ErrBusy 已有备份或恢复在进行中。
	ErrBusy = errors.New("已有备份或恢复任务在进行中")
	// ErrIncompatible 备份文件与当前代码不兼容（格式非法 / 转义口径不符）。
	ErrIncompatible = errors.New("备份文件与当前代码不兼容")
)

// backupIDPattern 是备份 ID 的精确形状：`lyidc-backup-<yyyyMMddThhmmssZ>-<8 位小写十六进制>`。
// 形状之外的任何输入（含 `../`、绝对路径、盘符、白名单外的名字）一律按「不存在」拒绝。
var backupIDPattern = regexp.MustCompile(`^` + FilePrefix + `\d{8}T\d{6}Z-[0-9a-f]{8}$`)

// SettingsReader 读取数据库备份设置（生产为 *settings.Reader）。
type SettingsReader interface {
	DBBackup(ctx context.Context) (settings.DBBackupState, error)
}

// Options 是 Service 的构造参数。
type Options struct {
	// InstallDir 是安装目录（契约 21.4）；空时取当前可执行文件所在目录。
	InstallDir string
	// WebRoot 是 Web 根目录；空时取 `<安装目录>/dist`（备份目录落在其内一律拒绝）。
	WebRoot string
	// DB 是应用数据库句柄；必填。
	DB *gorm.DB
	// DSN 是数据库连接串：恢复后追迁移时用它新开一条连接（迁移文件多为多语句，需要
	// DSN 上的 multiStatements=true，应用连接池的 DSN 未必带该参数）；为空时退回应用连接池。
	DSN string
	// Settings 读取 db_backup 设置（备份目录）；nil 时恒用缺省目录。
	Settings SettingsReader
	Logger   *slog.Logger
	// Now 注入时钟（测试用）；nil 时取 time.Now。
	Now func() time.Time
}

// Service 提供备份列表、创建、删除与恢复（同一时刻至多一个写操作）。
type Service struct {
	installDir string
	webRoot    string
	sqlDB      *sql.DB
	dsn        string
	settings   SettingsReader
	logger     *slog.Logger
	now        func() time.Time

	// mu 串行化创建 / 恢复 / 删除：恢复会重建整库，绝不能与导出并发。
	mu sync.Mutex
}

// NewService 构造备份服务。**必须**能从 gorm 句柄取到底层 *sql.DB。
func NewService(opts Options) (*Service, error) {
	if opts.DB == nil {
		return nil, errors.New("数据库句柄未初始化")
	}
	installDir := strings.TrimSpace(opts.InstallDir)
	if installDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("无法定位安装目录（os.Executable）：%w", err)
		}
		installDir = filepath.Dir(exe)
	}
	abs, err := filepath.Abs(installDir)
	if err != nil {
		return nil, fmt.Errorf("安装目录不是合法路径：%w", err)
	}
	webRoot := strings.TrimSpace(opts.WebRoot)
	if webRoot == "" {
		webRoot = filepath.Join(abs, WebRootName)
	} else if webRoot, err = filepath.Abs(webRoot); err != nil {
		return nil, fmt.Errorf("Web 根目录不是合法路径：%w", err)
	}
	sqlDB, err := opts.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接池失败：%w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		installDir: abs,
		webRoot:    webRoot,
		sqlDB:      sqlDB,
		dsn:        strings.TrimSpace(opts.DSN),
		settings:   opts.Settings,
		logger:     logger,
		now:        now,
	}, nil
}

// InstallDir 返回安装目录（绝对路径）。
func (s *Service) InstallDir() string { return s.installDir }

// DirState 描述当前生效的备份目录与其合法性（契约 21.6 的目录展示口径）。
type DirState struct {
	// Dir 是解析后的绝对路径（配置非法时是**被拒绝前**的解析结果，仅供诊断展示）。
	Dir string `json:"dir"`
	// Default 表示当前用的是缺省目录（设置项留空）。
	Default bool `json:"default"`
	// OutsideWebRoot 表示目录不在 Web 根内（恒为 true 才允许写入，契约 21.4）。
	OutsideWebRoot bool `json:"outside_web_root"`
	// Message 是目录不可用时的原因（面向管理员，已脱敏）。
	Message string `json:"message"`
}

// Valid 判断目录是否可用于写入。
func (d DirState) Valid() bool { return d.OutsideWebRoot }

// BackupDir 解析当前生效的备份目录并校验（每次调用都重新读设置，改完立即生效）。
//
// 校验口径（契约 21.4）：目录解析结果落在 Web 根（`<安装目录>/dist`）内、指向已存在的普通文件、
// 或符号链接解析后仍在 Web 根内 → 一律拒绝。
func (s *Service) BackupDir(ctx context.Context) DirState {
	state := DirState{Dir: filepath.Join(s.installDir, DefaultDirName), Default: true, OutsideWebRoot: true}
	if s.settings != nil {
		value, err := s.settings.DBBackup(ctx)
		switch {
		case errors.Is(err, settings.ErrCorrupt):
			state.OutsideWebRoot = false
			state.Message = "设置项 db_backup 的值无法解析（只可能是绕过接口手工改库导致），请修正后重试"
			return state
		case err != nil:
			// 读设置失败（库故障）不改变目录判定：写入路径由调用方再校验一次。
			return state
		}
		if dir := strings.TrimSpace(value.Value.Dir); dir != "" {
			state.Default = false
			state.Dir = s.resolveConfigured(dir)
		}
	}
	if err := s.checkDir(state.Dir); err != nil {
		state.OutsideWebRoot = false
		state.Message = Reason(err)
	}
	return state
}

// DefaultDir 返回缺省备份目录的绝对路径（`<安装目录>/backups`，与 `dist/` 同级）。
func (s *Service) DefaultDir() string { return filepath.Join(s.installDir, DefaultDirName) }

// DirReport 是备份目录的解析与校验报告（R15，契约 21.10 的展示口径）：
// 「解析后的绝对路径 + 是否在 Web 根外 + 是否存在 + 是否可写」。
//
// 只供设置接口与测试使用，不经 JSON 直接下发（对外视图由 router 层组装）。
type DirReport struct {
	// Dir 是解析后的绝对路径（相对路径按安装目录解析）。
	Dir string
	// DefaultDir 是缺省目录的绝对路径，与当前配置无关（供界面展示「留空 = 用这个」）。
	DefaultDir string
	// Default 表示用的是缺省目录（配置留空）。
	Default bool
	// OutsideWebRoot 表示目录不在 Web 根内（false 时不可用，原因见 Message）。
	OutsideWebRoot bool
	// Exists 表示目录当前存在。
	Exists bool
	// Writable 表示写入探针通过（目录不存在时恒为 false）。
	Writable bool
	// Message 是需要管理员注意的原因（面向管理员、已脱敏）；一切正常时为空串。
	Message string
}

// InspectDir 解析并检查候选备份目录（设置页读取用，契约 21.10）：**不创建目录、不写任何配置**。
// 目录已存在时做写入探针；不存在时只提示「保存或首次备份时会自动创建」。
func (s *Service) InspectDir(dir string) DirReport {
	report, err := s.resolveReport(dir)
	if err != nil {
		return report
	}
	if !report.Exists {
		report.Message = "备份目录尚不存在：保存该设置或首次备份时会自动创建（0750；上级目录不可写则会失败）"
		return report
	}
	if err := probeWritable(report.Dir); err != nil {
		report.Message = fmt.Sprintf("备份目录不可写：%v", err)
		return report
	}
	report.Writable = true
	return report
}

// PrepareDir 解析候选目录并做保存前准备（设置页保存用，契约 21.10）：
// 目录不存在则按 0750 创建（含中间目录），随后做写入探针；任一步失败都返回 ErrDirUnusable，
// 且**不落库**由调用方保证（本函数只碰文件系统）。返回的 report 在失败时也带可读原因。
func (s *Service) PrepareDir(dir string) (DirReport, error) {
	report, err := s.resolveReport(dir)
	if err != nil {
		// checkDir 的错误已经是 ErrDirUnusable + 可读原因，原样返回（不再二次加前缀）。
		return report, err
	}
	if err := os.MkdirAll(report.Dir, 0o750); err != nil {
		report.Message = fmt.Sprintf("创建备份目录失败：%v", err)
		return report, fmt.Errorf("%w: %s", ErrDirUnusable, report.Message)
	}
	report.Exists = true
	if err := probeWritable(report.Dir); err != nil {
		report.Message = fmt.Sprintf("备份目录不可写：%v", err)
		return report, fmt.Errorf("%w: %s", ErrDirUnusable, report.Message)
	}
	report.Writable = true
	return report, nil
}

// resolveReport 解析候选目录（空串 = 缺省）并执行**与运行时同一套**目录校验
// （Web 根 / 已存在的非目录 / 符号链接，见 checkDir）——两处口径绝不各写一套。
// 校验失败时返回 checkDir 的错误原文（ErrDirUnusable），供调用方原样映射为 40002。
func (s *Service) resolveReport(dir string) (DirReport, error) {
	report := DirReport{
		Dir:            s.DefaultDir(),
		DefaultDir:     s.DefaultDir(),
		Default:        true,
		OutsideWebRoot: true,
	}
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		report.Default = false
		report.Dir = s.resolveConfigured(trimmed)
	}
	if err := s.checkDir(report.Dir); err != nil {
		report.OutsideWebRoot = false
		report.Message = Reason(err)
		return report, err
	}
	if info, err := os.Lstat(report.Dir); err == nil && info.IsDir() {
		report.Exists = true
	}
	return report, nil
}

// probeWritable 在目录里写一个临时文件再删除：权限不足、只读挂载、磁盘故障都在这里暴露。
// 探针文件用完即删（名字带点前缀，正常情况不会出现在备份列表里——列表只认白名单形状的 .sql）。
func probeWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".lyidc-write-probe-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

// resolveConfigured 把设置里的目录解析成绝对路径：相对路径相对**安装目录**解析。
func (s *Service) resolveConfigured(dir string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(s.installDir, dir)
}

// dirError 是「备份目录不可用」的具体原因：Unwrap 到 ErrDirUnusable 供接口映射错误码，
// reason 则是不带哨兵前缀的可读原因，供接口 message 与日志使用。
//
// 拆开两者的目的是让 message 只有一层前缀：调用方拿到 reason 直接用，
// 需要包成 error 时再包一次，避免出现「备份目录不可用: 备份目录不可用: …」。
type dirError struct{ reason string }

func (e *dirError) Error() string { return ErrDirUnusable.Error() + ": " + e.reason }
func (e *dirError) Unwrap() error { return ErrDirUnusable }

// dirUnusable 构造一条带可读原因的目录不可用错误。
func dirUnusable(format string, args ...any) error {
	return &dirError{reason: fmt.Sprintf(format, args...)}
}

// Reason 取目录不可用错误的可读原因（不含「备份目录不可用」前缀）；其他错误原样返回 Error()。
func Reason(err error) string {
	var target *dirError
	if errors.As(err, &target) {
		return target.reason
	}
	return err.Error()
}

// checkDir 校验备份目录：不能落在 Web 根内、不能是已存在的非目录对象。
//
// 两侧（配置目录与 Web 根）都先过 normalizeDirPath 再做包含判定：只有两侧形态一致，
// `\\?\<Web 根>` 这类 Windows 等价写法才不会被判成「在 Web 根外」（契约 21.4 第 2 条）。
// 归一化失败（设备路径、卷路径、空段、段内尾随空格或点……）一律拒绝——
// 无法可靠判定包含关系时不放行，绝不用「判不出来就当不在 Web 根内」兜底。
func (s *Service) checkDir(dir string) error {
	if dir == "" {
		return dirUnusable("备份目录为空")
	}
	normalizedDir, err := normalizeDirPath(dir)
	if err != nil {
		return dirUnusable("%s", err)
	}
	normalizedRoot, err := normalizeDirPath(s.webRoot)
	if err != nil {
		// Web 根由部署方固定（默认 <安装目录>/dist）；它自身形态非法说明部署被改坏，
		// 此时包含关系无从判定，只能拒绝写入而不是放行。
		return dirUnusable("Web 根目录（%s）的形态不受支持（%v），无法判定备份目录是否在其内",
			s.webRoot, err)
	}
	// 符号链接解析后比较，防止用链接把备份目录指进 Web 根。
	if within(canonicalDir(normalizedDir), canonicalDir(normalizedRoot)) {
		return dirUnusable("备份目录不能位于 Web 根（%s）内——备份含管理员密码哈希等敏感数据，绝不能被 Web 服务器读取",
			s.webRoot)
	}
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() {
			// Windows 上 junction 等重解析点既不带 ModeSymlink、也不会被 IsDir 判为目录
			// （EvalSymlinks 同样不解析它），因此统一按「不是真实目录」拒绝：
			// 备份目录必须是一个真实存在的目录，绝不允许经由链接或重解析点落进 Web 根。
			return dirUnusable("%s 已存在且不是真实目录（文件、符号链接、junction 等重解析点一律不接受）", dir)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return dirUnusable("%s 是符号链接，请改用真实目录", dir)
		}
	}
	return nil
}

// verifyWritableDir 是**真正写备份文件之前**的最后一道目录校验（契约 21.4 第 2 条）：
// 设置落库之后仍可能被手工改库改成 Web 根内的路径，保存设置时的那次校验不构成写入时的保证。
//
// 纵深防御：不依赖 BackupDir 的内部顺序，任何写入路径都必须独立通过它。
// 拒绝时记 WARN（只记目录与原因，不含任何备份内容），调用方在创建目录与导出之前就返回。
func (s *Service) verifyWritableDir(dir string) error {
	if err := s.checkDir(dir); err != nil {
		s.logger.Warn("拒绝写入数据库备份：备份目录校验未通过", "dir", dir, "reason", err)
		return err
	}
	return nil
}

// within 判断 child 是否位于 parent 之内（含相等）；用 filepath.Rel 判定，跨盘符返回 false。
func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// canonicalDir 把路径规范化成可比较的形态：解析**最近存在祖先**的符号链接，
// 再把尚不存在的尾部拼回去。
//
// 两侧都走同一个函数，才能稳定比较：EvalSymlinks 会把 Windows 的 8.3 短名展开成长名
// （`C:\Users\ADMINI~1\...` → `C:\Users\Administrator\...`），只解析一侧会让「同一个目录」
// 比较出不同结果，进而误判 Web 根包含关系。
func canonicalDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	current := abs
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

// normalizeDirPath 把备份目录路径归一到**可比形态**，供 Web 根包含判定使用（契约 21.4）。
//
// 两侧（配置目录与 Web 根）必须走同一个函数：只要有一侧没归一化，Windows 上等价的两种写法
// 就会被判成不同目录，把备份目录写成 `\\?\<Web 根>` 即可绕过「备份目录不得位于 Web 根内」。
//
// 支持（归一化后交给 filepath 处理；`/` 与 `\` 等价、盘符大小写不敏感、8.3 短名与符号链接
// 由调用方的 canonicalDir 统一展开）：
//
//   - 普通绝对路径 `D:\data\backups` 与相对路径（调用方已按安装目录解析）；
//   - 扩展长度路径 `\\?\D:\data\backups` → 去掉 `\\?\` 前缀后按普通绝对路径处理；
//   - `\\?\UNC\server\share\backups` → 还原为 `\\server\share\backups`。
//
// 「盘符相对」的拒绝（`C:`、`C:foo`）只在 Windows 上生效：非 Windows 平台上冒号只是普通文件名字符。
//
// 一律拒绝（返回面向管理员的可读原因，调用方包成 ErrDirUnusable）：
//
//   - 设备命名空间 `\\.\C:`、`\\.\PhysicalDrive0` 等——设备路径不受支持；
//   - 卷路径 `\\?\Volume{GUID}\…` 与 `\\?\GLOBALROOT\…`；
//   - 其它无法识别的 `\\?\…` 形态（例如 `\\?\C:foo` 这种盘符相对写法）与裸盘符相对写法 `C:foo`；
//   - NUL 或控制字符；
//   - 空段（连续分隔符）与以空格或点结尾的路径段——Win32 会静默去掉段尾的空格与点，
//     同一个目录因此会出现多种写法，任何依赖字符串比较的判定都不可靠。
func normalizeDirPath(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("备份目录为空")
	}
	for _, r := range raw {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("备份目录路径不能包含 NUL 或控制字符（U+%04X）", r)
		}
	}

	// 盘符（`C:`）是 Windows 概念：非 Windows 平台上冒号只是普通文件名字符，不做盘符判定。
	windows := filepath.Separator == '\\'

	path := raw
	switch {
	case hasPrefixFold(path, `\\?\`):
		rest := path[4:]
		switch {
		case hasPrefixFold(rest, `UNC\`):
			path = `\\` + rest[4:]
		case hasPrefixFold(rest, `Volume{`):
			return "", fmt.Errorf(`卷路径不受支持（%s）：请填写带盘符的普通目录路径`, raw)
		case hasPrefixFold(rest, `GLOBALROOT`):
			return "", fmt.Errorf(`GLOBALROOT 路径不受支持（%s）：请填写带盘符的普通目录路径`, raw)
		case isDriveAbsolute(rest):
			path = rest
		default:
			return "", fmt.Errorf(`不支持的扩展长度路径形态（%s）：只接受 \\?\<盘符>:\… 与 \\?\UNC\<服务器>\<共享>\…`, raw)
		}
	case hasPrefixFold(path, `\\.\`):
		return "", fmt.Errorf("设备路径不受支持（%s）：请填写普通目录路径", raw)
	}
	// 盘符相对写法（`C:`、`C:foo`）在 Windows 上表示「C 盘的当前目录」，语义随进程工作目录变化。
	if windows && len(path) >= 2 && path[1] == ':' && isASCIILetter(path[0]) && !isDriveAbsolute(path) {
		return "", fmt.Errorf(`盘符相对路径不受支持（%s）：请写成 %c:\… 或相对安装目录的路径`, raw, path[0])
	}

	// Windows 上 `/` 与 `\` 等价，统一成平台分隔符后按段检查。
	sep := string(filepath.Separator)
	body := path
	if windows {
		body = strings.ReplaceAll(body, "/", `\`)
	}
	// 前导分隔符（UNC 的 `\\`、根路径的 `/` 或 `\`）不是空段。
	unc := strings.HasPrefix(body, `\\`)
	switch {
	case unc:
		body = body[2:]
		if body == "" || strings.HasPrefix(body, `\`) {
			return "", fmt.Errorf(`UNC 路径必须以 \\<服务器>\<共享> 开头：%s`, raw)
		}
	case strings.HasPrefix(body, sep):
		body = body[1:]
	}
	// 结尾分隔符合法（`C:\`、`/opt/`），不算空段。
	body = strings.TrimRight(body, sep)
	if body == "" {
		// 只有「根路径」会走到这里（前导分隔符已剥掉）：它是合法目录，按原样归一。
		return filepath.Clean(path), nil
	}
	segments := strings.Split(body, sep)
	if unc && len(segments) < 2 {
		return "", fmt.Errorf(`UNC 路径必须包含服务器与共享名（\\<服务器>\<共享>\…）：%s`, raw)
	}
	for _, segment := range segments {
		switch {
		case segment == "":
			return "", fmt.Errorf("备份目录路径含空段（连续分隔符）：%s", raw)
		case segment == "." || segment == "..":
			// 相对段交给 filepath.Clean 解析，不按「以点结尾」处理。
		case strings.HasSuffix(segment, " ") || strings.HasSuffix(segment, "."):
			return "", fmt.Errorf("备份目录路径段 %q 以空格或点结尾（Win32 会静默去掉它们，同一个目录会出现两种写法）：请改写该段", segment)
		}
	}
	return filepath.Clean(path), nil
}

// hasPrefixFold 是不区分大小写的前缀判断（Windows 的路径前缀与盘符都不区分大小写）。
func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// isDriveAbsolute 判断 s 是否为 `<盘符>:\…` / `<盘符>:/…` 形态（盘符后必须紧跟分隔符）。
func isDriveAbsolute(s string) bool {
	if len(s) < 3 || s[1] != ':' || !isASCIILetter(s[0]) {
		return false
	}
	return s[2] == '\\' || s[2] == '/'
}

// isASCIILetter 判断字节是否为 ASCII 字母。
func isASCIILetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// evalPath 解析符号链接；路径（或其父目录）不存在时退回原值。
func evalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(parent, filepath.Base(path))
	}
	return path
}

// Backup 是一条备份的对外视图（契约 21.3）：**只有元信息**，绝无内容。
type Backup struct {
	// ID 是备份标识，等于备份文件名去掉 .sql（接口只接受它，不接受路径）。
	ID string `json:"id"`
	// FileName 是备份文件名（供管理员在服务器上定位文件）。
	FileName string `json:"file_name"`
	// CreatedAt 是备份时间（RFC3339，UTC）。
	CreatedAt string `json:"created_at"`
	// Database 是备份时的库名。
	Database string `json:"database"`
	// SchemaVersion 是备份时的 schema_migrations 版本。
	SchemaVersion int64 `json:"schema_version"`
	// TableCount / RowCount 是备份时的表数与总行数。
	TableCount int   `json:"table_count"`
	RowCount   int64 `json:"row_count"`
	// FileSize 是备份文件字节数（元信息缺失时取自磁盘）。
	FileSize int64 `json:"file_size"`
	// Operator 是发起备份的管理员用户名。
	Operator string `json:"operator"`
	// AppVersion 是备份时的应用版本（buildinfo.Version）。
	AppVersion string `json:"app_version"`
	// FileExists 表示文件仍在磁盘上（被手工删除时为 false，接口按 404 语义处理）。
	FileExists bool `json:"file_exists"`
	// MetaMissing 表示元信息 sidecar 丢失（只可能是被手工删除），字段按可得信息回退。
	MetaMissing bool `json:"meta_missing"`
}

// ListView 是 GET /api/v1/admin/db-backups 的 data（契约 21.3）。
type ListView struct {
	// Database 是当前连接的库名。
	Database string `json:"database"`
	// BackupDir 是当前生效的备份目录（绝对路径）。
	BackupDir string `json:"backup_dir"`
	// BackupDirDefault 表示目录来自缺省值而非设置项。
	BackupDirDefault bool `json:"backup_dir_default"`
	// BackupDirOutsideWebRoot 表示目录不在 Web 根内（false 时不可备份，且给出原因）。
	BackupDirOutsideWebRoot bool `json:"backup_dir_outside_web_root"`
	// BackupDirMessage 是目录不可用时的原因。
	BackupDirMessage string `json:"backup_dir_message"`
	// TotalSizeBytes 是全部备份文件占用的磁盘空间（不含元信息文件）。
	TotalSizeBytes int64 `json:"total_size_bytes"`
	// Backups 按时间倒序。
	Backups []Backup `json:"backups"`
	// AutoCleanup 恒为 false：本批不做自动清理，删除由管理员手动执行（契约 21.3）。
	AutoCleanup bool `json:"auto_cleanup"`
}

// List 返回备份目录状态与全部备份的元信息（绝不读取文件内容）。
func (s *Service) List(ctx context.Context) (ListView, error) {
	dir := s.BackupDir(ctx)
	view := ListView{
		BackupDir:               dir.Dir,
		BackupDirDefault:        dir.Default,
		BackupDirOutsideWebRoot: dir.OutsideWebRoot,
		BackupDirMessage:        dir.Message,
		Backups:                 []Backup{},
		AutoCleanup:             false,
	}
	database, err := s.currentDatabase(ctx)
	if err != nil {
		return view, err
	}
	view.Database = database

	entries, err := os.ReadDir(dir.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return view, nil
		}
		return view, fmt.Errorf("读取备份目录失败: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, FileExt) {
			continue
		}
		id := strings.TrimSuffix(name, FileExt)
		if !backupIDPattern.MatchString(id) {
			continue
		}
		record, err := s.record(dir.Dir, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return view, err
		}
		view.TotalSizeBytes += record.FileSize
		view.Backups = append(view.Backups, record)
	}
	// 时间倒序；时间相同按文件名倒序（文件名内嵌时间戳，等价于按时间稳定排序）。
	sortBackups(view.Backups)
	return view, nil
}

// sortBackups 按备份时间倒序排列（全部归一为 UTC RFC3339，字典序即时间序）。
func sortBackups(records []Backup) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].CreatedAt != records[j].CreatedAt {
			return records[i].CreatedAt > records[j].CreatedAt
		}
		return records[i].ID > records[j].ID
	})
}

// Create 立即执行一次备份：导出到 `<dir>/<id>.sql` 并写入元信息 sidecar。
// operator 是发起备份的管理员用户名（只记名字，不记密码）。
func (s *Service) Create(ctx context.Context, operator string) (Backup, error) {
	if !s.mu.TryLock() {
		return Backup{}, ErrBusy
	}
	defer s.mu.Unlock()

	dir := s.BackupDir(ctx)
	if !dir.Valid() {
		// 拒绝写入必须留痕（只记目录与原因，绝不含任何备份内容）。
		s.logger.Warn("拒绝写入数据库备份：备份目录校验未通过", "dir", dir.Dir, "reason", dir.Message)
		return Backup{}, fmt.Errorf("%w: %s", ErrDirUnusable, dir.Message)
	}
	if err := s.verifyWritableDir(dir.Dir); err != nil {
		return Backup{}, err
	}
	if err := os.MkdirAll(dir.Dir, 0o750); err != nil {
		return Backup{}, fmt.Errorf("%w: 创建目录失败: %v", ErrDirUnusable, err)
	}

	database, err := s.currentDatabase(ctx)
	if err != nil {
		return Backup{}, err
	}
	schemaVersion, err := s.schemaVersion(ctx)
	if err != nil {
		return Backup{}, err
	}

	id := s.newID(dir.Dir)
	finalPath := filepath.Join(dir.Dir, id+FileExt)
	tmpPath := finalPath + ".tmp"
	started := s.now()

	stats, err := dumpDatabase(ctx, s.sqlDB, dumpOptions{
		Path:          tmpPath,
		Database:      database,
		SchemaVersion: schemaVersion,
		AppVersion:    buildinfo.Version,
		Operator:      operator,
		Now:           started,
	})
	if err != nil {
		_ = os.Remove(tmpPath)
		return Backup{}, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return Backup{}, fmt.Errorf("备份文件落盘失败: %w", err)
	}

	record := Backup{
		ID:            id,
		FileName:      id + FileExt,
		CreatedAt:     started.UTC().Format(time.RFC3339),
		Database:      database,
		SchemaVersion: schemaVersion,
		TableCount:    stats.TableCount,
		RowCount:      stats.RowCount,
		FileSize:      stats.FileSize,
		Operator:      operator,
		AppVersion:    buildinfo.Version,
		FileExists:    true,
	}
	if err := writeMeta(dir.Dir, record); err != nil {
		// 文件已经落盘：元信息写失败只降级（列表按文件 mtime 兜底展示），不删备份。
		s.logger.Warn("备份元信息写入失败，将按文件时间兜底展示",
			"backup_id", id, "error", err)
	}
	s.logger.Info("数据库备份完成",
		"backup_id", id, "database", database, "schema_version", schemaVersion,
		"tables", stats.TableCount, "rows", stats.RowCount, "size_bytes", stats.FileSize,
		"operator", operator, "duration_ms", s.now().Sub(started).Milliseconds())
	return record, nil
}

// Delete 删除一条备份（文件 + 元信息）；文件不存在按「备份不存在」处理。
func (s *Service) Delete(ctx context.Context, id string) (Backup, error) {
	if !s.mu.TryLock() {
		return Backup{}, ErrBusy
	}
	defer s.mu.Unlock()

	dir := s.BackupDir(ctx)
	record, path, err := s.resolve(dir, id)
	if err != nil {
		return Backup{}, err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("删除备份文件失败: %w", err)
	}
	if err := os.Remove(s.metaPath(dir.Dir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.logger.Warn("删除备份元信息失败", "backup_id", id, "error", err)
	}
	s.logger.Info("数据库备份已删除", "backup_id", id, "size_bytes", record.FileSize)
	return record, nil
}

// RestoreResult 是恢复接口的 data（契约 21.5）：只报结果，不回显任何内容。
type RestoreResult struct {
	// BackupID / FileName 是被使用的备份。
	BackupID string `json:"backup_id"`
	FileName string `json:"file_name"`
	// Database 是恢复的库名（服务端连接的当前库）。
	Database string `json:"database"`
	// FromSchemaVersion 是备份里的 schema 版本；ToSchemaVersion 是恢复后的实际版本。
	FromSchemaVersion int64 `json:"from_schema_version"`
	ToSchemaVersion   int64 `json:"to_schema_version"`
	// Migrated 表示恢复后执行了 `up` 把库追到当前代码所需的最新版本。
	Migrated bool `json:"migrated"`
	// TableCount 是恢复的表数；DroppedTables 是「备份里没有、恢复时被删掉」的表（覆盖而非合并）。
	TableCount    int      `json:"table_count"`
	DroppedTables []string `json:"dropped_tables"`
	// RestoredAt 是恢复完成时间（RFC3339，UTC）。
	RestoredAt string `json:"restored_at"`
	// Warning 是「备份版本高于当前代码」等需要管理员知悉的提示；为空表示无。
	Warning string `json:"warning"`
}

// Restore 用备份内容**覆盖**当前库（契约 21.5）：
// 逐表 DROP + CREATE + INSERT，随后删除备份里没有的表，最后按需把 schema 版本追到最新。
//
// 覆盖式且不可撤销：恢复前不会自动备份、过程中没有维护模式，失败可能留下不完整的库
// （这些取舍如实写进契约，由界面强提示）。
func (s *Service) Restore(ctx context.Context, id string) (RestoreResult, error) {
	if !s.mu.TryLock() {
		return RestoreResult{}, ErrBusy
	}
	defer s.mu.Unlock()

	dir := s.BackupDir(ctx)
	record, path, err := s.resolve(dir, id)
	if err != nil {
		return RestoreResult{}, err
	}
	database, err := s.currentDatabase(ctx)
	if err != nil {
		return RestoreResult{}, err
	}

	started := s.now()
	s.logger.Warn("数据库恢复开始（覆盖式，不可撤销）",
		"backup_id", id, "database", database, "operator_record", record.Operator)
	stats, err := loadDatabase(ctx, s.sqlDB, loadOptions{Path: path})
	if err != nil {
		s.logger.Error("数据库恢复失败，库可能处于不完整状态",
			"backup_id", id, "restored_tables", stats.TableCount, "error", err)
		return RestoreResult{}, err
	}

	result := RestoreResult{
		BackupID:          id,
		FileName:          record.FileName,
		Database:          database,
		FromSchemaVersion: stats.SchemaVersion,
		ToSchemaVersion:   stats.SchemaVersion,
		TableCount:        stats.TableCount,
		DroppedTables:     stats.DroppedTables,
		RestoredAt:        s.now().UTC().Format(time.RFC3339),
	}
	if result.DroppedTables == nil {
		result.DroppedTables = []string{}
	}

	// 恢复后追迁移（契约 21.5）：备份的 schema 版本低于当前代码内嵌的最新版本时执行 up。
	latest, err := latestMigrationVersion()
	if err != nil {
		return RestoreResult{}, err
	}
	switch {
	case stats.SchemaVersion < int64(latest):
		from, to, err := migrateUp(ctx, s.sqlDB, s.dsn)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("恢复已完成，但追迁移失败（库停留在版本 %d）：%w", stats.SchemaVersion, err)
		}
		result.Migrated = true
		result.ToSchemaVersion = int64(to)
		s.logger.Info("恢复后已执行数据库迁移", "from_version", from, "to_version", to)
	case stats.SchemaVersion > int64(latest):
		result.Warning = fmt.Sprintf(
			"备份的 schema 版本（%d）高于当前程序内嵌的最新版本（%d）：当前代码可能无法完全理解该库结构，请尽快升级到对应版本的程序",
			stats.SchemaVersion, latest)
	}
	s.logger.Info("数据库恢复完成",
		"backup_id", id, "tables", stats.TableCount, "dropped_tables", len(stats.DroppedTables),
		"schema_version", result.ToSchemaVersion, "migrated", result.Migrated,
		"duration_ms", s.now().Sub(started).Milliseconds())
	return result, nil
}

// VerifyDir 在启动时校验一次备份目录（契约 21.4：启动与保存时都校验）。
// 目录不可用只记警告（不影响其他功能），写入路径会在使用时再次拒绝。
func (s *Service) VerifyDir(ctx context.Context) {
	dir := s.BackupDir(ctx)
	if dir.Valid() {
		s.logger.Info("数据库备份目录就绪", "dir", dir.Dir, "default", dir.Default)
		return
	}
	s.logger.Warn("数据库备份目录不可用，备份与恢复将被拒绝", "dir", dir.Dir, "reason", dir.Message)
}

// resolve 把备份 ID 解析为目录内的文件路径（契约 21.4 的白名单口径）。
func (s *Service) resolve(dir DirState, id string) (Backup, string, error) {
	if !backupIDPattern.MatchString(id) {
		return Backup{}, "", ErrNotFound
	}
	path := filepath.Join(dir.Dir, id+FileExt)
	// 双重复核：拼接结果必须仍在备份目录内（形状校验已排除分隔符，这里是纵深防御）。
	if !within(evalPath(filepath.Dir(path)), evalPath(dir.Dir)) {
		return Backup{}, "", ErrNotFound
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		// 不存在、是符号链接或不是普通文件：一律按「备份不存在」处理。
		return Backup{}, "", ErrNotFound
	}
	record, err := s.record(dir.Dir, id)
	if err != nil {
		return Backup{}, "", err
	}
	return record, path, nil
}

// record 组装一条备份的元信息：sidecar 优先，缺失时按文件本身兜底。
func (s *Service) record(dir, id string) (Backup, error) {
	path := filepath.Join(dir, id+FileExt)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Backup{}, ErrNotFound
	}
	record := Backup{
		ID:         id,
		FileName:   id + FileExt,
		CreatedAt:  info.ModTime().UTC().Format(time.RFC3339),
		FileSize:   info.Size(),
		FileExists: true,
	}
	meta, err := readMeta(dir, id)
	switch {
	case err == nil:
		record.CreatedAt = meta.CreatedAt
		record.Database = meta.Database
		record.SchemaVersion = meta.SchemaVersion
		record.TableCount = meta.TableCount
		record.RowCount = meta.RowCount
		record.FileSize = meta.FileSize
		record.Operator = meta.Operator
		record.AppVersion = meta.AppVersion
		// 文件被手工截断/覆盖时以磁盘为准，避免展示与事实不符的大小。
		if meta.FileSize != info.Size() {
			record.FileSize = info.Size()
		}
	case errors.Is(err, os.ErrNotExist):
		record.MetaMissing = true
	case err != nil:
		record.MetaMissing = true
		s.logger.Warn("备份元信息无法解析，已按文件信息兜底", "backup_id", id, "error", err)
	}
	return record, nil
}

// currentDatabase 返回当前连接的库名。
func (s *Service) currentDatabase(ctx context.Context) (string, error) {
	var name sql.NullString
	if err := s.sqlDB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return "", fmt.Errorf("读取当前库名失败: %w", err)
	}
	if !name.Valid || name.String == "" {
		return "", errors.New("当前连接没有选择数据库")
	}
	return name.String, nil
}

// schemaVersion 读取迁移表的当前版本（未建迁移表或未执行迁移时为 0）。
func (s *Service) schemaVersion(ctx context.Context) (int64, error) {
	return schemaVersionOf(ctx, s.sqlDB)
}

// newID 生成备份 ID：UTC 时间戳 + 4 字节 crypto/rand 随机十六进制；
// 目录内已存在同名文件（或 sidecar）时重新生成。
func (s *Service) newID(dir string) string {
	for attempt := 0; attempt < 5; attempt++ {
		id := FilePrefix + s.now().UTC().Format("20060102T150405Z") + "-" + randomHex(4)
		if _, err := os.Lstat(filepath.Join(dir, id+FileExt)); errors.Is(err, os.ErrNotExist) {
			return id
		}
	}
	// 极端情况下（同秒内反复碰撞）退回纳秒级后缀，保证不覆盖既有备份。
	return FilePrefix + s.now().UTC().Format("20060102T150405") + "-" + randomHex(8)
}

// randomHex 返回 n 字节的随机十六进制串（crypto/rand，失败时退回纳秒时间戳 + 哈希，绝不 panic）。
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 在受支持的平台上不会失败；兜底仍保证文件名不可枚举（不是可预测的计数器）。
		sum := sha256.Sum256([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
		return hex.EncodeToString(sum[:n])
	}
	return hex.EncodeToString(buf)
}
