package install

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/db"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// coreTables 是安装状态判定必需的核心表（缺失即视为「尚未初始化」，契约 13.1）。
var coreTables = []string{"settings", "admins"}

// Options 是 Supervisor 的构造参数。
type Options struct {
	// Logger 记录安装过程与状态变化（不记录任何密钥）。
	Logger *slog.Logger
	// Config 是启动时加载的配置。
	Config config.Config
	// ConfigPath 是配置文件的「生效路径」（cfg.SourcePath）；为空时取运行目录下的 config.yaml。
	ConfigPath string
	// BuildEngine 构建正常模式的 HTTP 处理器（安装完成后热切换使用）。
	BuildEngine func(db *gorm.DB, jwt config.JWTConfig) http.Handler
	// ProbeTimeout 是单次数据库探测超时，缺省 3s。
	ProbeTimeout time.Duration
}

// Supervisor 是 HTTP 入口与安装状态机的持有者：
// 未安装时把请求交给安装页/安装 API，装完后把请求交给正常模式引擎（热切换，免重启）。
//
// 并发：所有安装步骤都在 mu 的写锁内执行，因此并发的安装请求天然串行化（契约 13.5）；
// 跨进程的一次性保护由 installed 标记的条件插入（INSERT ... ON DUPLICATE KEY）保证。
type Supervisor struct {
	opts       Options
	logger     *slog.Logger
	configPath string

	install *gin.Engine

	mu            sync.RWMutex
	state         State
	detail        string
	dsn           string
	dsnConfigured bool
	gdb           *gorm.DB
	gdbDSN        string
	engine        http.Handler
	jwt           config.JWTConfig
	// adminID 是安装向导第 4 步建立的管理员 ID（用于站点信息设置的审计归属与幂等重跑）。
	adminID uint64
	// retired 是热切换过程中被替换掉的数据库句柄，退出时统一关闭。
	retired []*gorm.DB
}

// NewSupervisor 构造 Supervisor；调用方随后需调用 Init 完成首次状态探测。
func NewSupervisor(opts Options) (*Supervisor, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.BuildEngine == nil {
		return nil, errors.New("install: Options.BuildEngine 不能为空")
	}
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = 3 * time.Second
	}

	configPath, err := config.ResolveWritePath(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	s := &Supervisor{
		opts:          opts,
		logger:        opts.Logger,
		configPath:    configPath,
		dsn:           strings.TrimSpace(opts.Config.Database.DSN),
		dsnConfigured: opts.Config.DatabaseConfigured,
		jwt:           opts.Config.JWT,
		state:         StateUnconfigured,
		detail:        "尚未探测安装状态",
	}
	s.install = s.newInstallEngine()
	return s, nil
}

// Init 执行首次状态探测：已安装则构建正常模式引擎，否则进入安装模式。
func (s *Supervisor) Init(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refreshLocked(ctx, true)

	if s.state == StateInstalled {
		s.logger.Info("系统已安装，进入正常模式",
			"config_path", s.configPath, "detail", s.detail)
		return
	}
	s.logger.Warn("系统尚未安装，已进入安装向导模式，请用浏览器访问 /install",
		"state", string(s.state), "detail", s.detail,
		"listen", s.opts.Config.Server.Addr, "config_path", s.configPath)
}

// Shutdown 关闭全部数据库句柄。
func (s *Supervisor) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.gdb != nil {
		if err := db.Close(s.gdb); err != nil {
			s.logger.Warn("关闭数据库连接失败", "error", err)
		}
		s.gdb = nil
	}
	for _, handle := range s.retired {
		if err := db.Close(handle); err != nil {
			s.logger.Warn("关闭数据库连接失败", "error", err)
		}
	}
	s.retired = nil
}

// IsInstalled 返回当前是否已安装。
func (s *Supervisor) IsInstalled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == StateInstalled
}

// Snapshot 返回当前状态与判定依据（依据文本不含任何密钥）。
func (s *Supervisor) Snapshot() (State, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, s.detail
}

// ConfigPath 返回配置文件写入口的绝对路径。
func (s *Supervisor) ConfigPath() string { return s.configPath }

// ServeHTTP 按安装状态分发请求（契约 13.1）：
//   - /install 与 /install/api/** → 安装页/安装 API（已安装时页面给「已安装」提示、API 返回 50302）；
//   - 已安装 → 正常模式引擎；
//   - 未安装：/api/v1/health 照常返回（db=down），其它 /api/** 返回 503（50301），
//     浏览器页面请求 302 跳转到 /install。
func (s *Supervisor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if isInstallPath(path) {
		s.serveWithLog(w, r, s.install.ServeHTTP)
		return
	}

	if s.IsInstalled() {
		s.engineHandler().ServeHTTP(w, r)
		return
	}

	if path == "/api/v1/health" {
		s.serveWithLog(w, r, s.serveHealth)
		return
	}
	if strings.HasPrefix(path, "/api/") || !acceptsHTML(r) {
		s.serveWithLog(w, r, func(w http.ResponseWriter, _ *http.Request) {
			writeEnvelope(w, http.StatusServiceUnavailable, response.New(
				response.CodeNotInstalled, response.Message(response.CodeNotInstalled), nil))
		})
		return
	}

	s.serveWithLog(w, r, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, PathPage, http.StatusFound)
	})
}

// engineHandler 返回正常模式引擎（调用方需确保此时已安装）。
func (s *Supervisor) engineHandler() http.Handler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		// 理论上不可达：状态为已安装时引擎必定已构建。
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeEnvelope(w, http.StatusServiceUnavailable, response.New(
				response.CodeInternalError, "服务尚未就绪，请稍后重试", nil))
		})
	}
	return s.engine
}

// refreshLocked 重新判定安装状态；已安装时构建（或替换）正常模式引擎。
// 调用方必须持有 s.mu 写锁。
//
// autoMark=true 时启用**场景 5**：库内已有管理员账号却没有 installed 标记（存量开发/升级库）
// 时自动补写标记并进入正常模式，避免打断既有部署。向导内部的探测固定用 false，
// 否则「刚跑完迁移、库内只有默认管理员」的中间态会被误判为已安装（契约 13.2）。
func (s *Supervisor) refreshLocked(ctx context.Context, autoMark bool) {
	if !s.dsnConfigured || strings.TrimSpace(s.dsn) == "" {
		s.state = StateUnconfigured
		s.detail = "未找到配置文件或配置文件未提供 database.dsn"
		return
	}

	gdb, err := s.handleLocked()
	if err != nil {
		s.state = StateDBUnreachable
		s.detail = "数据库连接失败：" + sanitizeDBError(err, s.dsn)
		return
	}

	missing, err := missingCoreTables(ctx, gdb)
	if err != nil {
		s.state = StateDBUnreachable
		s.detail = "数据库探测失败：" + sanitizeDBError(err, s.dsn)
		return
	}
	if len(missing) > 0 {
		s.state = StateTablesMissing
		s.detail = "数据库可达但缺少核心表：" + strings.Join(missing, "、")
		return
	}

	st := store.New(gdb)
	marker, present, err := installedMarker(ctx, st)
	if err != nil {
		s.state = StateDBUnreachable
		s.detail = "读取安装标记失败：" + sanitizeDBError(err, s.dsn)
		return
	}
	if present {
		s.enterInstalledLocked(marker)
		return
	}

	// 场景 5：无标记但已有管理员账号的存量库 → 自动补写标记。
	// 例外：库内留着「安装进行中」进度标记（向导尚未走完，例如中途重启进程）时不补标记，
	// 否则刚建完表的库会被误判为存量库、向导提前关闭并把默认管理员留在库里（契约 13.2）。
	total, unmodifiedDefault, err := st.AdminStats(ctx)
	if err != nil {
		s.state = StateDBUnreachable
		s.detail = "读取管理员表失败：" + sanitizeDBError(err, s.dsn)
		return
	}
	installInProgress, err := installProgressExists(ctx, st)
	if err != nil {
		s.state = StateDBUnreachable
		s.detail = "读取安装进度失败：" + sanitizeDBError(err, s.dsn)
		return
	}
	if autoMark && !installInProgress && hasInstallerAdmin(total, unmodifiedDefault) {
		marker = settings.NewInstalled(settings.SourceAuto)
		encoded, encodeErr := marker.Encode()
		if encodeErr != nil {
			s.state = StateAdminMissing
			s.detail = "安装标记序列化失败：" + encodeErr.Error()
			return
		}
		written, writeErr := st.InsertSettingIfAbsent(ctx, settings.KeyInstalled, encoded)
		if writeErr != nil {
			// 补标记失败不阻断既有部署：照常进入正常模式，只记录告警。
			s.logger.Warn("存量库自动补写 installed 标记失败（不影响服务运行）", "error", writeErr)
		} else if written {
			s.logger.Info("检测到存量数据库（已有管理员但无 installed 标记），已自动补写安装标记",
				"marker", settings.KeyInstalled, "admins", total)
			if unmodifiedDefault == total {
				// 只提醒，不阻断：既有部署的默认账号由运营自行替换（契约 13.1 场景 5）。
				s.logger.Warn("库内管理员仍是迁移写入的未修改默认账号，建议尽快修改密码后再对外服务",
					"admins", total)
			}
		}
		s.enterInstalledLocked(marker)
		return
	}

	// 续装定位（契约 13.1 补充规则 3）：无标记、且不是存量库时，按
	// 「安装者管理员 → 站点信息 → 完成」逐级定位续装点，使向导在任意步刷新页面或
	// 重启进程后都落回正确的步骤，而不是一律回退到第 4 步。
	if !installerAdminReady(total, unmodifiedDefault) {
		s.state = StateAdminMissing
		s.detail = "数据库已初始化但尚未创建安装者管理员账号"
		return
	}

	siteReady, err := siteConfigured(ctx, st)
	if err != nil {
		s.state = StateDBUnreachable
		s.detail = "读取站点设置失败：" + sanitizeDBError(err, s.dsn)
		return
	}
	if !siteReady {
		s.state = StateSiteMissing
		s.detail = "安装者管理员已就绪，等待填写站点信息"
		return
	}

	s.state = StatePending
	s.detail = "管理员与站点信息已就绪，等待完成安装"
}

// enterInstalledLocked 把状态切到「已安装」，必要时构建正常模式引擎。
func (s *Supervisor) enterInstalledLocked(marker settings.Installed) {
	s.state = StateInstalled
	if marker.At != "" {
		s.detail = "系统已安装（" + marker.At + "）"
	} else {
		s.detail = "系统已安装"
	}
	if s.engine == nil {
		s.engine = s.opts.BuildEngine(s.gdb, s.jwt)
		s.logger.Info("正常模式引擎已就绪", "installed_at", marker.At, "source", marker.Source)
	}
}

// handleLocked 返回当前生效 DSN 对应的数据库句柄：可复用时复用，否则新建并替换旧句柄。
func (s *Supervisor) handleLocked() (*gorm.DB, error) {
	if s.gdb != nil {
		if s.gdbDSN == s.dsn {
			ctx, cancel := context.WithTimeout(context.Background(), s.opts.ProbeTimeout)
			defer cancel()
			if err := pingHandle(ctx, s.gdb); err == nil {
				return s.gdb, nil
			}
		}
		// DSN 变了或连接已失效：退役旧句柄，重建连接。
		s.retired = append(s.retired, s.gdb)
		s.gdb = nil
		s.gdbDSN = ""
	}

	cfg := s.opts.Config.Database
	cfg.DSN = s.dsn
	gdb, err := db.Open(cfg)
	if err != nil {
		return nil, err
	}
	s.gdb = gdb
	s.gdbDSN = s.dsn
	return gdb, nil
}

// serveHealth 在安装模式下提供 /api/v1/health（包格式与契约第 5 节一致）。
func (s *Supervisor) serveHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.opts.ProbeTimeout)
	defer cancel()

	s.mu.RLock()
	gdb := s.gdb
	s.mu.RUnlock()

	data := healthData{Status: "ok", DB: "up", Time: time.Now().Format(time.RFC3339)}
	if gdb == nil || pingHandle(ctx, gdb) != nil {
		data.Status = "degraded"
		data.DB = "down"
		writeEnvelope(w, http.StatusServiceUnavailable, response.New(
			response.CodeInternalError, response.Message(response.CodeInternalError), data))
		return
	}
	writeEnvelope(w, http.StatusOK, response.New(response.CodeSuccess, "ok", data))
}

// healthData 与 router 包的健康检查数据体保持一致（契约第 5 节）。
type healthData struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Time   string `json:"time"`
}

// serveWithLog 记录一次由 Supervisor 直接处理（或交给安装引擎）的请求。
func (s *Supervisor) serveWithLog(w http.ResponseWriter, r *http.Request, handle func(http.ResponseWriter, *http.Request)) {
	start := time.Now()
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	handle(recorder, r)

	s.logger.Log(r.Context(), levelForStatus(recorder.status), "http 请求",
		"method", r.Method,
		"path", r.URL.Path,
		"status", recorder.status,
		"latency_ms", time.Since(start).Milliseconds(),
		"client_ip", clientIP(r),
	)
}

// statusRecorder 记录响应状态码（供访问日志使用）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后透传。
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// missingCoreTables 返回缺失的核心表名（按 coreTables 顺序）。
func missingCoreTables(ctx context.Context, gdb *gorm.DB) ([]string, error) {
	var missing []string
	for _, table := range coreTables {
		var count int64
		err := gdb.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			table,
		).Scan(&count).Error
		if err != nil {
			return nil, err
		}
		if count == 0 {
			missing = append(missing, table)
		}
	}
	return missing, nil
}

// installedMarker 读取 installed 标记；标记存在但内容损坏时同样按「已安装」处理
// （存在即已安装，避免内容问题把线上服务打回安装模式）。
func installedMarker(ctx context.Context, st *store.Store) (settings.Installed, bool, error) {
	setting, err := st.Setting(ctx, settings.KeyInstalled)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return settings.Installed{}, false, nil
	case err != nil:
		return settings.Installed{}, false, err
	}
	marker, parseErr := settings.ParseInstalled(setting.Value)
	if parseErr != nil {
		return settings.Installed{}, true, nil
	}
	if marker.At == "" {
		marker.At = setting.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return marker, true, nil
}

// hasInstallerAdmin 判断是否为「已有管理员的存量库」（场景 5 判据）。
//
// 按契约 13.1 场景 5，判据取「admins 表非空」：迁移 0003 写入的默认管理员也是存量部署的既成事实，
// 不能因为它把已有部署打回安装模式（unmodifiedDefault 只用于日志告警）。
// 注意与 installerAdminReady 的区别：后者要求存在**安装者**管理员，用于续装定位。
func hasInstallerAdmin(total, unmodifiedDefault int64) bool {
	return total > 0
}

// installerAdminReady 判断是否已建立**安装者**管理员：存在密码哈希不等于默认哈希的行。
// 口径与 status 视图的 progress.admin_ready 一致（契约 13.1 补充规则 3、13.3.1）。
func installerAdminReady(total, unmodifiedDefault int64) bool {
	return total-unmodifiedDefault > 0
}

// siteConfigured 判断库内是否已写站点信息（settings.site 存在）。
func siteConfigured(ctx context.Context, st *store.Store) (bool, error) {
	_, err := st.Setting(ctx, settings.KeySite)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}

// installProgressExists 判断库内是否留有「安装进行中」进度标记。
func installProgressExists(ctx context.Context, st *store.Store) (bool, error) {
	_, err := st.Setting(ctx, settings.KeyInstallProgress)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}

// pingHandle 探测数据库连通性。
func pingHandle(ctx context.Context, gdb *gorm.DB) error {
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// isInstallPath 判断路径是否属于安装页/安装 API。
func isInstallPath(path string) bool {
	return path == PathPage || strings.HasPrefix(path, PathPage+"/")
}

// acceptsHTML 判断请求是否来自浏览器导航（用于决定 302 还是 JSON 503）。
func acceptsHTML(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

// clientIP 取客户端 IP（去掉端口）。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// levelForStatus 按响应状态码选择日志级别（与 router 包一致）。
func levelForStatus(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// writeEnvelope 以统一响应包写出 JSON。
func writeEnvelope(w http.ResponseWriter, status int, envelope response.Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(envelope)
}

// sanitizeDBError 抹掉错误文本中可能出现的 DSN 密码（错误信息会进日志与 HTTP 响应，
// 契约 13.7 要求密钥永不外泄；此处做防御性二次保证）。
func sanitizeDBError(err error, dsn string) string {
	message := err.Error()
	if password := dsnPassword(dsn); password != "" {
		message = strings.ReplaceAll(message, password, "****")
	}
	return message
}

// dsnPassword 解析 DSN 取出密码；解析失败返回空串。
func dsnPassword(dsn string) string {
	parsed, err := gomysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return ""
	}
	return parsed.Passwd
}
