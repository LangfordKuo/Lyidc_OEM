package install

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 管理员账号规则（与账号体系一致，契约 13.3）。
const (
	minAdminUsernameLength = 3
	maxAdminUsernameLength = 32
	maxAdminNicknameRunes  = 32
)

// defaultAdminUsername / defaultAdminPassword 是迁移 0003 写入的默认凭据，
// 安装向导拒绝把它们作为最终账号（契约 13.3：库内不得残留未修改的默认管理员）。
const (
	defaultAdminUsername = "admin"
	defaultAdminPassword = "admin123456"
)

var adminUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

// newInstallEngine 构造安装页与安装 API 的路由（不挂任何鉴权：安装模式下才可达）。
func (s *Supervisor) newInstallEngine() *gin.Engine {
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.Use(gin.Recovery())

	page := engine.Group(PathPage)
	{
		page.GET("", s.handlePage)
		page.GET("/", s.handlePage)

		api := page.Group("/api")
		{
			api.GET("/status", s.handleStatus)
			api.GET("/environment", s.handleEnvironment)
			api.POST("/database/test", s.handleDatabaseTest)
			api.POST("/database", s.handleDatabaseSave)
			api.POST("/initialize", s.handleInitialize)
			api.POST("/admin", s.handleAdmin)
			api.POST("/site", s.handleSite)
			api.POST("/complete", s.handleComplete)
		}
	}

	engine.NoRoute(func(c *gin.Context) { response.Fail(c, response.CodeNotFound, "接口不存在") })
	engine.NoMethod(func(c *gin.Context) { response.Fail(c, response.CodeNotFound, "接口不存在") })
	return engine
}

// handlePage 渲染安装向导页；已安装时渲染「系统已安装」提示页，不重入安装流程。
func (s *Supervisor) handlePage(c *gin.Context) {
	if s.IsInstalled() {
		state, detail := s.Snapshot()
		c.Data(http.StatusOK, "text/html; charset=utf-8",
			renderInstalledPage(installedPageData{
				State:      string(state),
				Detail:     detail,
				ConfigPath: s.configPath,
			}))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderInstallPage(s.configPath))
}

// requireWizard 是安装 API 的统一前置检查：已安装时返回 50302 并终止处理。
func (s *Supervisor) requireWizard(c *gin.Context) bool {
	if s.IsInstalled() {
		response.Fail(c, response.CodeInstallClosed,
			response.Message(response.CodeInstallClosed)+"：如需重装，请先清空数据库（删除库或删表）后重启服务")
		return false
	}
	return true
}

// handleStatus 返回当前安装状态、进度与生效路径（安装页每次加载与每步之后都会调用）。
func (s *Supervisor) handleStatus(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}
	if !s.IsInstalled() {
		s.mu.Lock()
		s.refreshLocked(c.Request.Context(), false)
		s.mu.Unlock()
	}
	response.Success(c, s.buildStatus(c.Request.Context()))
}

// handleEnvironment 返回环境检查结果与运行信息（向导第 1 步）。
func (s *Supervisor) handleEnvironment(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}

	state, detail := s.Snapshot()
	view := environmentView{
		OK: true,
		Runtime: runtimeView{
			GoVersion:    runtime.Version(),
			OS:           runtime.GOOS,
			Arch:         runtime.GOARCH,
			PID:          os.Getpid(),
			WorkingDir:   workingDir(),
			ConfigPath:   s.configPath,
			ConfigSource: s.opts.Config.SourcePath,
			ServerAddr:   s.opts.Config.Server.Addr,
			State:        string(state),
		},
	}

	writeCheck := envCheck{Key: "config_write", Name: "配置写入点可写", OK: true}
	if err := config.CheckWritable(s.configPath); err != nil {
		writeCheck.OK = false
		writeCheck.Detail = err.Error()
		writeCheck.Advice = "请确保服务运行账号对配置文件所在目录有写权限；" +
			"Linux 下可用 chown/chmod 调整，Windows 下检查目录是否只读或改用手动指定的 -config 路径。"
	} else {
		writeCheck.Detail = "安装完成后将把数据库连接与 JWT 密钥写入：" + s.configPath
	}
	view.Checks = append(view.Checks, writeCheck)

	migrationsCheck := envCheck{Key: "migrations", Name: "迁移资源完整", OK: true}
	steps, err := availableMigrations()
	if err != nil {
		migrationsCheck.OK = false
		migrationsCheck.Detail = err.Error()
		migrationsCheck.Advice = "内嵌迁移文件缺失，通常说明二进制不是用仓库代码构建的：请重新构建后再试。"
	} else {
		names := make([]string, 0, len(steps))
		for _, step := range steps {
			names = append(names, step.Name)
		}
		migrationsCheck.Detail = fmt.Sprintf("共 %d 个版本：%s", len(steps), strings.Join(names, "、"))
		view.Runtime.Migrations = steps
	}
	view.Checks = append(view.Checks, migrationsCheck)

	view.Checks = append(view.Checks, envCheck{
		Key:  "runtime",
		Name: "运行环境",
		OK:   true,
		Detail: fmt.Sprintf("%s %s/%s，PID %d，运行目录 %s，监听 %s；当前状态：%s（%s）",
			runtime.Version(), runtime.GOOS, runtime.GOARCH, os.Getpid(),
			workingDir(), s.opts.Config.Server.Addr, string(state), detail),
	})

	for _, check := range view.Checks {
		if !check.OK {
			view.OK = false
		}
	}
	response.Success(c, view)
}

// handleDatabaseTest 测试数据库连接（第 2 步的「测试连接」，可顺带自动建库）。
func (s *Supervisor) handleDatabaseTest(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}
	req, ok := bindDatabaseRequest(c)
	if !ok {
		return
	}

	view, err := testDatabaseConnection(c.Request.Context(), req)
	if err != nil {
		failConnect(c, s.logger, err, req)
		return
	}
	response.Success(c, view)
}

// handleDatabaseSave 保存数据库连接参数到**安装会话（进程内存）**并立即复验连接。
// 参数在「完成」步骤才会写入配置文件（契约 13.4）。
func (s *Supervisor) handleDatabaseSave(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}
	req, ok := bindDatabaseRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	view, err := testDatabaseConnection(ctx, req)
	if err != nil {
		failConnect(c, s.logger, err, req)
		return
	}

	s.mu.Lock()
	s.dsn = req.dsn()
	s.dsnConfigured = true
	s.refreshLocked(ctx, false)
	state := s.state
	firstStep := s.state.FirstStep()
	installed := s.state == StateInstalled
	s.mu.Unlock()

	s.logger.Info("安装向导：数据库参数已保存到安装会话",
		"host", req.Host, "port", req.Port, "database", req.Database, "state", string(state))

	response.Success(c, databaseSaveView{
		databaseView: view,
		State:        string(state),
		FirstStep:    firstStep,
		Installed:    installed,
	})
}

// handleInitialize 执行迁移建表（第 3 步）。可重复调用：已是最新版本时返回 no_change。
func (s *Supervisor) handleInitialize(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := c.Request.Context()
	if !s.dsnConfigured {
		response.Fail(c, response.CodeValidationFailed, "请先完成第 2 步：数据库配置")
		return
	}
	s.refreshLocked(ctx, false)
	switch s.state {
	case StateDBUnreachable:
		response.FailWithData(c, http.StatusServiceUnavailable, response.CodeDBConnectFailed,
			"数据库当前不可达："+s.detail, gin.H{"advice": "请回到第 2 步检查数据库参数并重新测试连接。"})
		return
	case StateInstalled:
		response.Fail(c, response.CodeInstallClosed, response.Message(response.CodeInstallClosed))
		return
	}

	result, err := runMigrations(ctx, s.dsn)
	if err != nil {
		s.logger.Error("安装向导：执行迁移失败", "error", sanitizeDBError(err, s.dsn))
		response.FailWithData(c, http.StatusInternalServerError, response.CodeDatabaseError,
			"执行迁移失败："+sanitizeDBError(err, s.dsn),
			gin.H{"advice": "请确认账号有建表权限、MySQL 版本为 5.7+；若迁移表处于 dirty 状态，请按迁移文件手工修复数据库。"})
		return
	}

	s.markProgressLocked(ctx, settings.StageInitialized)
	s.refreshLocked(ctx, false)

	s.logger.Info("安装向导：数据库初始化完成",
		"from_version", result.FromVersion, "to_version", result.ToVersion, "applied", len(result.Applied))
	response.Success(c, initView{
		migrationResult: result,
		State:           string(s.state),
		FirstStep:       s.state.FirstStep(),
	})
}

// adminRequest 是第 4 步（管理员账号）的请求体。
type adminRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Nickname        string `json:"nickname"`
}

// handleAdmin 创建（或改写默认管理员行）安装者管理员账号。
func (s *Supervisor) handleAdmin(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}

	var req adminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.CodeInvalidParam, "请求体不是合法的 JSON 或字段类型不匹配")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Nickname = strings.TrimSpace(req.Nickname)
	if err := validateAdminRequest(req); err != nil {
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	}
	if req.Nickname == "" {
		req.Nickname = req.Username
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		failInternal(c, s.logger, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := c.Request.Context()
	s.refreshLocked(ctx, false)
	if s.gdb == nil || s.state == StateDBUnreachable || s.state == StateUnconfigured {
		response.Fail(c, response.CodeValidationFailed, "请先完成第 2、3 步：数据库配置与初始化建表")
		return
	}
	if s.state == StateInstalled {
		response.Fail(c, response.CodeInstallClosed, response.Message(response.CodeInstallClosed))
		return
	}

	st := store.New(s.gdb)
	view := adminView{Username: req.Username}

	switch {
	case s.adminID != 0:
		// 同一会话内重复执行第 4 步：改写本会话已建立的账号（幂等）。
		if err := st.UpdateAdminAccount(ctx, s.adminID, req.Username, hash, req.Nickname); err != nil {
			failStore(c, s.logger, err)
			return
		}
		view.AdminID = s.adminID
		view.Updated = true
	default:
		result, err := st.EnsureInstallerAdmin(ctx, req.Username, hash, req.Nickname)
		if err != nil {
			failStore(c, s.logger, err)
			return
		}
		s.adminID = result.AdminID
		view.AdminID = result.AdminID
		view.Created = result.Created
		view.ReplacedDefaultAdmin = result.ReplacedDefault
	}

	s.markProgressLocked(ctx, settings.StageAdmin)
	s.refreshLocked(ctx, false)
	view.State = string(s.state)
	view.FirstStep = s.state.FirstStep()

	s.logger.Info("安装向导：管理员账号已建立",
		"admin_id", view.AdminID, "username", view.Username,
		"replaced_default_admin", view.ReplacedDefaultAdmin, "created", view.Created)
	response.Success(c, view)
}

// siteRequest 是第 5 步（站点信息）的请求体。
type siteRequest struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	AdminEmail string `json:"admin_email"`
}

// handleSite 写入站点信息（settings 表 site 键）。
func (s *Supervisor) handleSite(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}

	var req siteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.CodeInvalidParam, "请求体不是合法的 JSON 或字段类型不匹配")
		return
	}
	site := settings.Site{
		Name:       strings.TrimSpace(req.Name),
		URL:        strings.TrimRight(strings.TrimSpace(req.URL), "/"),
		AdminEmail: strings.TrimSpace(req.AdminEmail),
	}
	if err := site.Validate(); err != nil {
		failSettings(c, err)
		return
	}
	encoded, err := site.Encode()
	if err != nil {
		failInternal(c, s.logger, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := c.Request.Context()
	if s.gdb == nil {
		response.Fail(c, response.CodeValidationFailed, "请先完成第 2、3 步：数据库配置与初始化建表")
		return
	}
	st := store.New(s.gdb)

	actor := s.adminID
	if actor == 0 {
		resolved, err := st.InstallerAdminID(ctx)
		if err != nil {
			failDB(c, s.logger, err)
			return
		}
		actor = resolved
	}
	var updatedBy *uint64
	if actor != 0 {
		updatedBy = &actor
	}
	if _, err := st.UpsertSettingBy(ctx, settings.KeySite, encoded, updatedBy); err != nil {
		failDB(c, s.logger, err)
		return
	}

	s.markProgressLocked(ctx, settings.StageSite)
	s.refreshLocked(ctx, false)

	s.logger.Info("安装向导：站点信息已写入", "key", settings.KeySite, "url", site.URL)
	response.Success(c, siteView{
		Name:       site.Name,
		URL:        site.URL,
		AdminEmail: site.AdminEmail,
		State:      string(s.state),
		FirstStep:  s.state.FirstStep(),
	})
}

// handleComplete 完成安装（第 6 步）：生成 JWT 密钥 → 写配置文件 → 写 installed 标记 → 热切换。
func (s *Supervisor) handleComplete(c *gin.Context) {
	if !s.requireWizard(c) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := c.Request.Context()
	s.refreshLocked(ctx, false)
	switch s.state {
	case StateUnconfigured, StateDBUnreachable:
		response.Fail(c, response.CodeValidationFailed, "请先完成第 2 步：数据库配置（当前数据库不可用）")
		return
	case StateTablesMissing:
		response.Fail(c, response.CodeValidationFailed, "请先完成第 3 步：初始化建表")
		return
	case StateInstalled:
		response.Fail(c, response.CodeInstallClosed, response.Message(response.CodeInstallClosed))
		return
	}

	st := store.New(s.gdb)
	total, unmodifiedDefault, err := st.AdminStats(ctx)
	if err != nil {
		failDB(c, s.logger, err)
		return
	}
	if total == 0 || total == unmodifiedDefault {
		response.Fail(c, response.CodeValidationFailed, "请先完成第 4 步：创建管理员账号（库内仍是未修改的默认管理员）")
		return
	}

	site, err := settings.NewReader(st).Site(ctx)
	if err != nil || strings.TrimSpace(site.Value.Name) == "" {
		response.Fail(c, response.CodeValidationFailed, "请先完成第 5 步：站点信息")
		return
	}

	secret, err := generateJWTSecret()
	if err != nil {
		failInternal(c, s.logger, err)
		return
	}

	// 1) 写配置文件（合并写入生效路径；密钥只进文件与内存，不进日志/响应）。
	writtenPath, err := config.SaveMerged(s.configPath, config.SaveInput{
		DSN:       s.dsn,
		JWTSecret: secret,
	})
	if err != nil {
		s.logger.Error("安装向导：写入配置文件失败", "error", err, "config_path", s.configPath)
		response.FailWithData(c, http.StatusInternalServerError, response.CodeInternalError,
			"写入配置文件失败："+err.Error(),
			gin.H{"advice": "请确认运行账号对该路径有写权限（可在第 1 步环境检查里确认），或改用 -config 指定可写路径后重启服务。"})
		return
	}

	// 2) 写 installed 标记：条件插入，跨进程保证只有一次安装生效（契约 13.5）。
	marker := settings.NewInstalled(settings.SourceWizard)
	encoded, err := marker.Encode()
	if err != nil {
		failInternal(c, s.logger, err)
		return
	}
	written, err := st.InsertSettingIfAbsent(ctx, settings.KeyInstalled, encoded)
	if err != nil {
		failDB(c, s.logger, err)
		return
	}
	if !written {
		s.logger.Warn("安装向导：installed 标记已存在，本次完成请求被拒绝（可能由其它进程并发完成）")
		response.Fail(c, response.CodeInstallClosed,
			response.Message(response.CodeInstallClosed)+"：检测到已存在安装标记")
		return
	}

	// 3) 清理「安装进行中」进度标记（best effort，失败不影响可用性）。
	clearInstallProgressLocked(ctx, st, s.logger)

	// 4) 热切换：换成新的 JWT 密钥与正常模式引擎，无需重启。
	expire := s.opts.Config.JWT.ExpireHours
	if expire <= 0 || expire > config.MaxJWTExpireHours {
		expire = config.DefaultJWTExpireHours
	}
	s.jwt = config.JWTConfig{Secret: secret, ExpireHours: expire}
	s.configPath = writtenPath
	s.state = StateInstalled
	s.detail = "系统已安装（" + marker.At + "）"
	s.engine = s.opts.BuildEngine(s.gdb, s.jwt)

	s.logger.Info("安装向导：安装完成，已切换到正常模式（免重启）",
		"config_path", writtenPath, "installed_at", marker.At,
		"listen", s.opts.Config.Server.Addr, "admin_console", adminConsolePath)

	response.Success(c, completeView{
		Installed:        true,
		ConfigPath:       writtenPath,
		ConfigWritten:    true,
		JWTSecretWritten: true,
		MarkerWritten:    true,
		RestartRequired:  false,
		Site:             site.Value,
		AdminConsole:     adminConsolePath,
		AdminConsoleHint: "前端页面尚未部署：可用 curl 或浏览器插件调用 " + adminConsolePath +
			"（POST，body {\"username\":\"...\",\"password\":\"...\"}）获取管理员 token，随后即可访问 /api/v1/admin/** 接口。",
		InstalledAt: marker.At,
	})
}

// adminConsolePath 是管理员登录接口（完成页提示用）。
const adminConsolePath = "/api/v1/admin/auth/login"

// bindDatabaseRequest 解析并校验数据库表单；失败时写出响应并返回 false。
func bindDatabaseRequest(c *gin.Context) (databaseRequest, bool) {
	var req databaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.CodeInvalidParam, "请求体不是合法的 JSON 或字段类型不匹配")
		return req, false
	}
	req.normalize()
	if err := req.validate(); err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return req, false
	}
	return req, true
}

// validateAdminRequest 校验管理员表单（规则与账号体系一致）。
func validateAdminRequest(req adminRequest) error {
	if !adminUsernamePattern.MatchString(req.Username) {
		return fmt.Errorf("用户名需为 %d-%d 位字母、数字或下划线", minAdminUsernameLength, maxAdminUsernameLength)
	}
	if err := auth.ValidatePasswordLength(req.Password); err != nil {
		return err
	}
	if req.Password != req.ConfirmPassword {
		return errors.New("两次输入的密码不一致")
	}
	if req.Password == defaultAdminPassword {
		return fmt.Errorf("密码不能使用系统自带的默认密码 %s，请另设强密码", defaultAdminPassword)
	}
	if utf8.RuneCountInString(req.Nickname) > maxAdminNicknameRunes {
		return fmt.Errorf("昵称长度不能超过 %d 个字符", maxAdminNicknameRunes)
	}
	return nil
}

// markProgressLocked 写入「安装进行中」进度标记（失败只告警：它只用于区分中间态）。
func (s *Supervisor) markProgressLocked(ctx context.Context, stage string) {
	if s.gdb == nil {
		return
	}
	encoded, err := settings.NewInstallProgress(stage).Encode()
	if err != nil {
		return
	}
	if _, err := store.New(s.gdb).UpsertSettingBy(ctx, settings.KeyInstallProgress, encoded, nil); err != nil {
		s.logger.Warn("安装向导：写入安装进度标记失败（不影响安装流程）", "stage", stage, "error", err)
	}
}

// clearInstallProgressLocked 删除安装进度标记（安装完成后不再需要）。
func clearInstallProgressLocked(ctx context.Context, st *store.Store, logger *slog.Logger) {
	if err := st.DeleteSetting(ctx, settings.KeyInstallProgress); err != nil {
		logger.Warn("安装向导：清理安装进度标记失败（不影响使用）", "error", err)
	}
}

// generateJWTSecret 生成 32 字节随机 JWT 密钥（base64url，43 字符）。
func generateJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 JWT 密钥失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// workingDir 返回运行目录（失败时返回空串）。
func workingDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// failConnect 以 50303 写数据库连接失败（响应里带处置建议，日志不含密码）。
func failConnect(c *gin.Context, logger *slog.Logger, err error, req databaseRequest) {
	var failure *connectFailure
	if !errors.As(err, &failure) {
		failInternal(c, logger, err)
		return
	}
	logger.Warn("安装向导：数据库连接失败",
		"host", req.Host, "port", req.Port, "database", req.Database, "reason", failure.Message)
	response.FailWithData(c, http.StatusServiceUnavailable, response.CodeDBConnectFailed, failure.Message,
		gin.H{
			"advice":               failure.Advice,
			"need_create_database": failure.NeedCreateDatabase,
		})
}

// failStore 把存储层错误映射成响应（用户名冲突 → 409，其余 → 50001）。
func failStore(c *gin.Context, logger *slog.Logger, err error) {
	if errors.Is(err, store.ErrUsernameTaken) {
		response.Fail(c, response.CodeConflict, "该用户名已被其它管理员占用，请换一个用户名")
		return
	}
	failDB(c, logger, err)
}

// failSettings 把设置项校验错误映射成响应（规则类 → 40002，格式类 → 40001）。
func failSettings(c *gin.Context, err error) {
	if errors.Is(err, settings.ErrRule) {
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	}
	response.Fail(c, response.CodeInvalidParam, err.Error())
}

// failInternal 记录并以内部错误码（500）响应。
func failInternal(c *gin.Context, logger *slog.Logger, err error) {
	logger.Error("服务器内部错误", "error", err, "method", c.Request.Method, "path", c.Request.URL.Path)
	response.Fail(c, response.CodeInternalError, response.Message(response.CodeInternalError))
}

// failDB 记录并以数据库错误码（50001）响应。
func failDB(c *gin.Context, logger *slog.Logger, err error) {
	logger.Error("数据库操作失败", "error", err, "method", c.Request.Method, "path", c.Request.URL.Path)
	response.Fail(c, response.CodeDatabaseError, response.Message(response.CodeDatabaseError))
}

// pingWithTimeout 带超时探测数据库句柄。
func pingWithTimeout(ctx context.Context, gdb *gorm.DB, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pingHandle(pingCtx, gdb)
}

// parseDSNTarget 解析 DSN 的连接目标（不含密码），供状态视图展示。
func parseDSNTarget(dsn string) (host string, port int, username, database string) {
	parsed, err := gomysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return "", 0, "", ""
	}
	host, portText, err := splitHostPortLoose(parsed.Addr)
	if err != nil {
		return parsed.Addr, 0, parsed.User, parsed.DBName
	}
	return host, portText, parsed.User, parsed.DBName
}

// splitHostPortLoose 解析 host:port（端口非法时按 0 返回）。
func splitHostPortLoose(addr string) (string, int, error) {
	index := strings.LastIndex(addr, ":")
	if index < 0 {
		return addr, 0, errors.New("缺少端口")
	}
	port := 0
	if _, err := fmt.Sscanf(addr[index+1:], "%d", &port); err != nil {
		return addr[:index], 0, err
	}
	return addr[:index], port, nil
}
