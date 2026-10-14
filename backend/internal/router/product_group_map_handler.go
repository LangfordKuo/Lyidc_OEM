package router

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// groupMapHandler 承载 R12「上游分组 → 本地子分类」映射的管理端接口（契约 10.4）。
//
// 权限：查看类（GET）三角色均可读（与分类树同口径）；写操作（PUT / DELETE）沿用
// categories.write —— **仅 admin**，由路由注册处的 requireAdminRole 兜底。
type groupMapHandler struct {
	store  *store.Store
	logger *slog.Logger
}

// groupMapView 是 GET /admin/product-group-maps 的列表项（也是 PUT 的成功响应）。
type groupMapView struct {
	UpstreamGroupID    int    `json:"upstream_group_id"`
	UpstreamGroupName  string `json:"upstream_group_name"`
	CategoryID         uint64 `json:"category_id"`
	CategoryName       string `json:"category_name"`
	CategoryParentName string `json:"category_parent_name"`
}

// groupMapListView 是 GET /admin/product-group-maps 响应（不分页，数量级 = 上游分组数）。
type groupMapListView struct {
	Items []groupMapView `json:"items"`
}

// upsertGroupMapRequest 是 PUT /admin/product-group-maps/:upstream_group_id 请求体。
type upsertGroupMapRequest struct {
	CategoryID *uint64 `json:"category_id"`
}

// listGroupMaps 处理 GET /api/v1/admin/product-group-maps（契约 10.4）：
// 列出全部映射，按 upstream_group_id 升序；目标分类已被删除时名字字段留空供前端提示。
func (h *groupMapHandler) listGroupMaps(c *gin.Context) {
	ctx := c.Request.Context()

	maps, err := h.store.ListGroupCategoryMaps(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	groups, err := h.store.ListProductGroups(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	categories, err := h.store.ListProductCategories(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	groupNames := make(map[int]string, len(groups))
	for _, group := range groups {
		groupNames[group.UpstreamGroupID] = group.Name
	}
	byID := make(map[uint64]*model.ProductCategory, len(categories))
	for i := range categories {
		byID[categories[i].ID] = &categories[i]
	}

	items := make([]groupMapView, 0, len(maps))
	for _, item := range maps {
		items = append(items, buildGroupMapView(item.UpstreamGroupID, item.CategoryID,
			groupNames[item.UpstreamGroupID], byID))
	}
	response.Success(c, groupMapListView{Items: items})
}

// upsertGroupMap 处理 PUT /api/v1/admin/product-group-maps/:upstream_group_id（契约 10.4）：
// 新增或更新映射。校验目标分类存在且是**子分类**，且上游分组已导入本地。
func (h *groupMapHandler) upsertGroupMap(c *gin.Context) {
	upstreamGroupID, ok := groupMapUpstreamIDParam(c)
	if !ok {
		return
	}

	var req upsertGroupMapRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.CategoryID == nil || *req.CategoryID == 0 {
		response.Fail(c, response.CodeInvalidParam, "category_id 必须是正整数（目标子分类 ID）")
		return
	}

	ctx := c.Request.Context()

	// 目标必须是**存在的子分类**（契约 10.4：主分类不可作为映射目标）。
	category, err := h.store.ProductCategoryByID(ctx, *req.CategoryID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeValidationFailed, "分类不存在")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	if category.IsMain() {
		response.Fail(c, response.CodeValidationFailed, "只能映射到子分类（主分类不可作为映射目标）")
		return
	}

	// 上游分组必须已导入本地（否则映射无从展示、也无从应用）。
	group, err := h.store.ProductGroupByUpstreamID(ctx, upstreamGroupID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeValidationFailed, "上游分组不存在（请先导入该分组的商品）")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	saved, err := h.store.UpsertGroupCategoryMap(ctx, upstreamGroupID, *req.CategoryID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	byID := map[uint64]*model.ProductCategory{category.ID: category}
	if category.ParentID != nil {
		if parent, err := h.store.ProductCategoryByID(ctx, *category.ParentID); err == nil {
			byID[parent.ID] = parent
		}
	}
	setOplogSummary(c, fmt.Sprintf("已保存分组映射：上游分组「%s」→ 分类 #%d（重新导入后生效）",
		group.Name, saved.CategoryID))
	response.Success(c, buildGroupMapView(saved.UpstreamGroupID, saved.CategoryID, group.Name, byID))
}

// deleteGroupMap 处理 DELETE /api/v1/admin/product-group-maps/:upstream_group_id（契约 10.4）：
// 删除映射（不存在返回 404）。删除后该分组回到自动派生（需重新导入才生效）。
func (h *groupMapHandler) deleteGroupMap(c *gin.Context) {
	upstreamGroupID, ok := groupMapUpstreamIDParam(c)
	if !ok {
		return
	}

	err := h.store.DeleteGroupCategoryMap(c.Request.Context(), upstreamGroupID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, "该分组没有映射")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	setOplogSummary(c, fmt.Sprintf("已删除上游分组 #%d 的映射（该分组回到自动派生，需重新导入才生效）", upstreamGroupID))
	response.Success(c, nil)
}

// buildGroupMapView 组装单个映射视图；目标分类缺失或是主分类时名字字段留空（契约 10.4）。
func buildGroupMapView(upstreamGroupID int, categoryID uint64, groupName string,
	byID map[uint64]*model.ProductCategory) groupMapView {
	view := groupMapView{
		UpstreamGroupID:   upstreamGroupID,
		UpstreamGroupName: groupName,
		CategoryID:        categoryID,
	}
	category, ok := byID[categoryID]
	if !ok || category.IsMain() {
		return view
	}
	view.CategoryName = category.Name
	if parent, ok := byID[category.ParentIDValue()]; ok {
		view.CategoryParentName = parent.Name
	}
	return view
}

// groupMapUpstreamIDParam 解析并校验路径参数 :upstream_group_id（正整数）。
func groupMapUpstreamIDParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("upstream_group_id")))
	if err != nil || id <= 0 {
		response.Fail(c, response.CodeInvalidParam, "上游分组 ID 必须为正整数")
		return 0, false
	}
	return id, true
}
