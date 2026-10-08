package install

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"os"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 安装页资源：内嵌 HTML/CSS/JS，**不依赖前端构建产物**（安装时前端尚未部署，契约 13.2）。
//
//go:embed web/*.html
var webFS embed.FS

var (
	installPageTemplate   = template.Must(template.ParseFS(webFS, "web/install.html"))
	installedPageTemplate = template.Must(template.ParseFS(webFS, "web/installed.html"))
)

// installPageData 是安装向导页的模板数据。
type installPageData struct {
	ConfigPath string
	StepCount  int
}

// installedPageData 是「系统已安装」提示页的模板数据。
type installedPageData struct {
	State      string
	Detail     string
	ConfigPath string
}

// renderInstallPage 渲染安装向导页。
func renderInstallPage(configPath string) []byte {
	return executePage(installPageTemplate, "install.html", installPageData{
		ConfigPath: configPath,
		StepCount:  StepCount,
	})
}

// renderInstalledPage 渲染「系统已安装」提示页（重访 /install 时使用，永不重入安装流程）。
func renderInstalledPage(data installedPageData) []byte {
	return executePage(installedPageTemplate, "installed.html", data)
}

// executePage 执行页面模板；失败时返回纯文本错误页（模板错误属于构建期问题）。
func executePage(tmpl *template.Template, name string, data any) []byte {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return []byte("页面渲染失败：" + template.HTMLEscapeString(err.Error()))
	}
	return buf.Bytes()
}

// statusView 是 GET /install/api/status 的响应数据。
type statusView struct {
	Installed    bool           `json:"installed"`
	State        string         `json:"state"`
	StateLabel   string         `json:"state_label"`
	Detail       string         `json:"detail"`
	FirstStep    int            `json:"first_step"`
	StepCount    int            `json:"step_count"`
	ConfigPath   string         `json:"config_path"`
	ConfigExists bool           `json:"config_exists"`
	ConfigSource string         `json:"config_source"`
	ServerAddr   string         `json:"server_addr"`
	Database     databaseStatus `json:"database"`
	Progress     progressView   `json:"progress"`
	InstalledAt  string         `json:"installed_at"`
	AdminConsole string         `json:"admin_console"`
	// AdminUsername 是库内**实际已就绪**的安装者管理员用户名（未建为空串）。
	// 完成页摘要用它展示，避免依赖页面表单的瞬时值（刷新/重启续装后表单为空）。
	AdminUsername string `json:"admin_username"`
	// SiteName 是库内**实际已写入**的站点名称（未写为空串）。
	SiteName string `json:"site_name"`
}

// databaseStatus 是数据库的展示信息（不含密码，也不回显完整 DSN）。
type databaseStatus struct {
	Configured    bool   `json:"configured"`
	Reachable     bool   `json:"reachable"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Database      string `json:"database"`
	ServerVersion string `json:"server_version"`
	TableCount    int64  `json:"table_count"`
}

// progressView 是安装进度（供界面跳过已完成的步骤）。
type progressView struct {
	DatabaseReady bool `json:"database_ready"`
	TablesReady   bool `json:"tables_ready"`
	AdminReady    bool `json:"admin_ready"`
	SiteReady     bool `json:"site_ready"`
	Completed     bool `json:"completed"`
}

// databaseSaveView 是保存数据库参数的结果。
type databaseSaveView struct {
	databaseView
	State     string `json:"state"`
	FirstStep int    `json:"first_step"`
	Installed bool   `json:"installed"`
}

// initView 是初始化建表的结果。
type initView struct {
	migrationResult
	State     string `json:"state"`
	FirstStep int    `json:"first_step"`
}

// adminView 是创建管理员的结果。
type adminView struct {
	AdminID              uint64 `json:"admin_id"`
	Username             string `json:"username"`
	Created              bool   `json:"created"`
	Updated              bool   `json:"updated"`
	ReplacedDefaultAdmin bool   `json:"replaced_default_admin"`
	State                string `json:"state"`
	FirstStep            int    `json:"first_step"`
}

// siteView 是站点信息的结果。
type siteView struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	AdminEmail string `json:"admin_email"`
	State      string `json:"state"`
	FirstStep  int    `json:"first_step"`
}

// completeView 是安装完成的结果（只回「已写入」状态，绝不回显任何密钥）。
type completeView struct {
	Installed        bool          `json:"installed"`
	ConfigPath       string        `json:"config_path"`
	ConfigWritten    bool          `json:"config_written"`
	JWTSecretWritten bool          `json:"jwt_secret_written"`
	MarkerWritten    bool          `json:"marker_written"`
	RestartRequired  bool          `json:"restart_required"`
	Site             settings.Site `json:"site"`
	AdminConsole     string        `json:"admin_console"`
	AdminConsoleHint string        `json:"admin_console_hint"`
	InstalledAt      string        `json:"installed_at"`
}

// environmentView 是环境检查的结果。
type environmentView struct {
	OK      bool        `json:"ok"`
	Checks  []envCheck  `json:"checks"`
	Runtime runtimeView `json:"runtime"`
}

// envCheck 是一项环境检查。
type envCheck struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Advice string `json:"advice"`
}

// runtimeView 是运行信息。
type runtimeView struct {
	GoVersion    string          `json:"go_version"`
	OS           string          `json:"os"`
	Arch         string          `json:"arch"`
	PID          int             `json:"pid"`
	WorkingDir   string          `json:"working_dir"`
	ConfigPath   string          `json:"config_path"`
	ConfigSource string          `json:"config_source"`
	ServerAddr   string          `json:"server_addr"`
	State        string          `json:"state"`
	Migrations   []migrationStep `json:"migrations"`
}

// buildStatus 组装安装状态视图（含进度探测）。
func (s *Supervisor) buildStatus(ctx context.Context) statusView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	view := statusView{
		Installed:    s.state == StateInstalled,
		State:        string(s.state),
		StateLabel:   stateLabel(s.state),
		Detail:       s.detail,
		FirstStep:    s.state.FirstStep(),
		StepCount:    StepCount,
		ConfigPath:   s.configPath,
		ConfigSource: s.opts.Config.SourcePath,
		ServerAddr:   s.opts.Config.Server.Addr,
		AdminConsole: adminConsolePath,
	}
	if _, err := os.Stat(s.configPath); err == nil {
		view.ConfigExists = true
	}

	view.Database.Configured = s.dsnConfigured
	if s.dsnConfigured {
		host, port, username, database := parseDSNTarget(s.dsn)
		view.Database.Host = host
		view.Database.Port = port
		view.Database.Username = username
		view.Database.Database = database
	}

	if s.gdb == nil || !s.dsnConfigured {
		return view
	}
	if err := pingWithTimeout(ctx, s.gdb, s.opts.ProbeTimeout); err != nil {
		return view
	}

	view.Database.Reachable = true
	view.Progress.DatabaseReady = true

	var version string
	if err := s.gdb.WithContext(ctx).Raw("SELECT VERSION()").Scan(&version).Error; err == nil {
		view.Database.ServerVersion = version
	}
	if count, err := databaseTableCount(ctx, s.gdb, view.Database.Database); err == nil {
		view.Database.TableCount = count
	}
	if missing, err := missingCoreTables(ctx, s.gdb); err == nil {
		view.Progress.TablesReady = len(missing) == 0
	}

	st := store.New(s.gdb)
	if total, unmodified, err := st.AdminStats(ctx); err == nil {
		view.Progress.AdminReady = installerAdminReady(total, unmodified)
	}
	// 展示库内实际已就绪的内容（供完成页摘要使用，不依赖页面表单瞬时值）。
	if admin, err := st.InstallerAdmin(ctx); err == nil && admin != nil {
		view.AdminUsername = admin.Username
	}
	if _, err := st.Setting(ctx, settings.KeySite); err == nil {
		view.Progress.SiteReady = true
		if site, err := settings.NewReader(st).Site(ctx); err == nil {
			view.SiteName = site.Value.Name
		}
	}
	if marker, present, err := installedMarker(ctx, st); err == nil && present {
		view.Progress.Completed = true
		view.InstalledAt = marker.At
	}

	if view.Progress.Completed {
		view.Progress.DatabaseReady = true
		view.Progress.TablesReady = true
		view.Progress.AdminReady = true
		view.Progress.SiteReady = true
	}
	return view
}

// stateLabel 返回状态的中文标签（界面展示用）。
func stateLabel(state State) string {
	switch state {
	case StateUnconfigured:
		return "未配置数据库"
	case StateDBUnreachable:
		return "数据库不可达"
	case StateTablesMissing:
		return "数据库尚未初始化"
	case StateAdminMissing:
		return "等待创建管理员账号"
	case StateSiteMissing:
		return "等待站点信息"
	case StatePending:
		return "等待完成安装"
	default:
		return "已安装"
	}
}
