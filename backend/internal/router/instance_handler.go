package router

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// msgInstanceMissing 是实例不存在/非本人时的统一提示（对外 404）。
const msgInstanceMissing = "实例不存在"

// instanceHandler 处理实例接口：会员端本人实例列表/详情，管理端实例列表。
//
// 敏感字段可见性（契约 14.4）：列表一律不含主机账号/密码/端口（会员端与管理端相同）；
// 详情接口仅**会员本人**可见 username / password / port / assigned_ips。
type instanceHandler struct {
	store  *store.Store
	logger *slog.Logger
}

// instanceSummaryView 是实例列表项（不含敏感字段）。
type instanceSummaryView struct {
	ID             uint64  `json:"id"`
	OrderID        uint64  `json:"order_id"`
	HostID         int     `json:"host_id"`
	ProductID      uint64  `json:"product_id"`
	ProductName    string  `json:"product_name"`
	Name           string  `json:"name"`
	BillingCycle   string  `json:"billing_cycle"`
	NextDueDate    *string `json:"next_due_date"`
	Status         string  `json:"status"`
	UpstreamStatus string  `json:"upstream_status"`
	DedicatedIP    string  `json:"dedicated_ip"`
	CreatedAt      string  `json:"created_at"`
}

// adminInstanceView 是管理端实例列表项：多 member_id，同样不含敏感字段。
type adminInstanceView struct {
	instanceSummaryView
	MemberID uint64 `json:"member_id"`
}

// instanceDetailView 是会员端实例详情（**仅本人**可见，含敏感字段）。
type instanceDetailView struct {
	instanceSummaryView
	AssignedIPs []string `json:"assigned_ips"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	UpdatedAt   string   `json:"updated_at"`
}

// instanceListView 是实例分页列表。
type instanceListView struct {
	Items    []instanceSummaryView `json:"items"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Total    int64                 `json:"total"`
}

// adminInstanceListView 是管理端实例分页列表。
type adminInstanceListView struct {
	Items    []adminInstanceView `json:"items"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int64               `json:"total"`
}

// listMyInstances 处理 GET /api/v1/instances：本人实例分页（新建在前），可按 status 过滤。
func (h *instanceHandler) listMyInstances(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	status, ok := instanceStatusQuery(c)
	if !ok {
		return
	}

	items, total, err := h.store.ListInstances(c.Request.Context(), store.InstanceFilter{
		MemberID: member.ID,
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]instanceSummaryView, 0, len(items))
	for i := range items {
		views = append(views, newInstanceSummaryView(&items[i]))
	}
	response.Success(c, instanceListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// getMyInstance 处理 GET /api/v1/instances/:id：仅本人可见，含主机账号密码等敏感字段。
func (h *instanceHandler) getMyInstance(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := instanceIDParam(c)
	if !ok {
		return
	}

	instance, err := h.store.InstanceByIDForMember(c.Request.Context(), id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgInstanceMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, newInstanceDetailView(instance))
}

// listAdminInstances 处理 GET /api/v1/admin/instances：分页 + member_id/status 过滤（全站数据）。
func (h *instanceHandler) listAdminInstances(c *gin.Context) {
	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}

	memberID, err := parseUint64Query("member_id", c.Query("member_id"))
	if err != nil {
		failRuleError(c, err)
		return
	}
	status, ok := instanceStatusQuery(c)
	if !ok {
		return
	}

	items, total, err := h.store.ListInstances(c.Request.Context(), store.InstanceFilter{
		MemberID: memberID,
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]adminInstanceView, 0, len(items))
	for i := range items {
		views = append(views, adminInstanceView{
			instanceSummaryView: newInstanceSummaryView(&items[i]),
			MemberID:            items[i].MemberID,
		})
	}
	response.Success(c, adminInstanceListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// instanceStatusQuery 解析并校验 status 查询参数；非法时写出 40001 并返回 ok=false。
func instanceStatusQuery(c *gin.Context) (string, bool) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidInstanceStatus(status) {
		response.Fail(c, response.CodeInvalidParam,
			"status 只能是 active / suspended / cancelled / terminated")
		return "", false
	}
	return status, true
}

// instanceIDParam 解析路径参数 :id。
func instanceIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "实例 ID 必须为正整数")
		return 0, false
	}
	return id, true
}

// newInstanceSummaryView 组装实例列表项（不含敏感字段）。
func newInstanceSummaryView(instance *model.Instance) instanceSummaryView {
	return instanceSummaryView{
		ID:             instance.ID,
		OrderID:        instance.OrderID,
		HostID:         instance.HostID,
		ProductID:      instance.ProductID,
		ProductName:    instance.ProductName,
		Name:           instance.Name,
		BillingCycle:   instance.BillingCycle,
		NextDueDate:    formatTimePtr(instance.NextDueDate),
		Status:         instance.Status,
		UpstreamStatus: instance.UpstreamStatus,
		DedicatedIP:    instance.DedicatedIP,
		CreatedAt:      formatTime(instance.CreatedAt),
	}
}

// newInstanceDetailView 组装实例详情（含敏感字段；仅会员本人可见）。
func newInstanceDetailView(instance *model.Instance) instanceDetailView {
	return instanceDetailView{
		instanceSummaryView: newInstanceSummaryView(instance),
		AssignedIPs:         splitIPs(instance.AssignedIPs),
		Port:                instance.Port,
		Username:            instance.Username,
		Password:            instance.Password,
		UpdatedAt:           formatTime(instance.UpdatedAt),
	}
}

// splitIPs 把库内逗号分隔的附加 IP 展开为切片（无值输出空数组）。
func splitIPs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
