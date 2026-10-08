// Package install 实现「首次访问进入安装向导」的部署级安装能力（契约见 docs/api-contract.md 第 13 节）。
//
// 设计要点：
//   - 安装状态机由**每次探测的实际环境**判定（配置文件、数据库连通性、核心表、installed 标记、
//     admins 表内容），不依赖任何本地文件标志位，因此换机器/换进程也能得到一致结论；
//   - 安装模式下仅放行安装页、安装 API 与健康检查；装完后 /install 永久关闭（见 13.1、13.5）；
//   - 安装完成**免重启**：Supervisor 在进程内换掉数据库句柄、JWT 配置与正常模式引擎（见 13.6）。
package install

// State 是安装状态机的判定结果（契约 13.1 的五种场景）。
type State string

const (
	// StateUnconfigured 场景 1：没有配置文件或配置文件未显式提供 database.dsn。
	StateUnconfigured State = "unconfigured"
	// StateDBUnreachable 场景 2：配置了 DSN 但数据库不可达（安装/修复模式）。
	StateDBUnreachable State = "db_unreachable"
	// StateTablesMissing 场景 3：数据库可达但核心表缺失。
	StateTablesMissing State = "tables_missing"
	// StateAdminMissing 场景 4：表齐全、无 installed 标记、且没有安装者管理员账号。
	StateAdminMissing State = "admin_missing"
	// StateSiteMissing 续装态（场景 4 之后）：安装者管理员已建，等待站点信息（第 5 步）。
	StateSiteMissing State = "site_missing"
	// StatePending 续装态（场景 4 之后）：管理员与站点信息都已就绪，等待完成安装（第 6 步）。
	StatePending State = "pending"
	// StateInstalled 场景 5 与正常态：已安装（installed 标记存在，或存量库已自动补标记）。
	StateInstalled State = "installed"
)

// NeedsWizard 判断该状态是否需要进入安装向导。
func (s State) NeedsWizard() bool { return s != StateInstalled }

// FirstStep 返回该状态下向导应当从第几步开始（1-6；已安装返回 0 表示向导已关闭）。
//
// 步骤编号：1 环境检查 / 2 数据库配置 / 3 初始化建表 / 4 管理员账号 / 5 站点信息 / 6 完成。
// 续装态（site_missing / pending）让向导在任意步刷新页面或重启进程后都能落回正确的步骤（契约 13.1）。
func (s State) FirstStep() int {
	switch s {
	case StateUnconfigured:
		return 1
	case StateDBUnreachable:
		return 2
	case StateTablesMissing:
		return 3
	case StateAdminMissing:
		return 4
	case StateSiteMissing:
		return 5
	case StatePending:
		return 6
	default:
		return 0
	}
}

// StepCount 是向导的总步骤数。
const StepCount = 6

// 安装 API 路径（供路由与文档对照）。
const (
	PathPage         = "/install"
	PathStatus       = "/install/api/status"
	PathEnvironment  = "/install/api/environment"
	PathDatabaseTest = "/install/api/database/test"
	PathDatabaseSave = "/install/api/database"
	PathInitialize   = "/install/api/initialize"
	PathAdmin        = "/install/api/admin"
	PathSite         = "/install/api/site"
	PathComplete     = "/install/api/complete"
)
