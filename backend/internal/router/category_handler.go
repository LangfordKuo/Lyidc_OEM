package router

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 分类字段约束（契约 10.4 的 PUT /admin/categories/:id）。
const (
	maxCategoryNameLen = 128
	maxCategorySort    = 999999
)

// categoryHandler 承载 R7 商品两级分类的管理端接口（契约 10.4）。
//
// cache 是业务缓存门面（R19 批 2，契约 24.6）：分类树 + 子分类商品计数是**与身份无关**的公开数据；
// 改分类（改名 / 改排序 / 改挂）会同时影响分类树与商品视图（商品视图带分类归属），
// 因此 categories: 与 products: 两个键族一起失效。
type categoryHandler struct {
	store  *store.Store
	cache  *bizCache
	logger *slog.Logger
}

// updateCategoryRequest 是 PUT /admin/categories/:id 请求体，字段均可选但至少提供一个。
type updateCategoryRequest struct {
	Name *string `json:"name"`
	// ParentID 仅子分类可传：改挂到另一个主分类。
	ParentID *uint64 `json:"parent_id"`
	Sort     *int    `json:"sort"`
}

// adminSubcategoryView 是管理端子分类（含商品计数）。
//
// Products 复用 adminGroupCountsView（分组列表同款），保证 products 计数的 JSON 键
// 是小写 total/on/off（契约 10.4）；store.GroupCounts 不带 json tag，不能直接下发。
type adminSubcategoryView struct {
	ID        uint64               `json:"id"`
	Key       string               `json:"key"`
	Name      string               `json:"name"`
	Sort      int                  `json:"sort"`
	Products  adminGroupCountsView `json:"products"`
	CreatedAt string               `json:"created_at"`
	UpdatedAt string               `json:"updated_at"`
}

// adminCategoryView 是管理端主分类。
type adminCategoryView struct {
	ID            uint64                 `json:"id"`
	Key           string                 `json:"key"`
	Name          string                 `json:"name"`
	Sort          int                    `json:"sort"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
	Subcategories []adminSubcategoryView `json:"subcategories"`
}

// adminCategoryListView 是 GET /admin/categories 响应。
type adminCategoryListView struct {
	Items []adminCategoryView `json:"items"`
}

// listCategories 处理 GET /api/v1/admin/categories：分类树 + 每个子分类的商品计数。
// 只读，三类角色都可访问（矩阵见契约 10.5 / 12.7）。
//
// R19 批 2：入缓存（键 categories:list，含商品计数）——与身份无关；改分类 / 导入 / 改商品整族失效。
func (h *categoryHandler) listCategories(c *gin.Context) {
	cachedJSON(c, h.cache, cacheKeyAdminCategories, cacheTTLCategories,
		func() (adminCategoryListView, bool) {
			ctx := c.Request.Context()
			categories, err := h.store.ListProductCategories(ctx)
			if err != nil {
				failDB(c, h.logger, err)
				return adminCategoryListView{}, false
			}
			counts, err := h.store.ProductCategoryCounts(ctx)
			if err != nil {
				failDB(c, h.logger, err)
				return adminCategoryListView{}, false
			}

			items := make([]adminCategoryView, 0)
			for i := range categories {
				main := &categories[i]
				if !main.IsMain() {
					continue
				}
				subs := make([]adminSubcategoryView, 0)
				for j := range categories {
					sub := &categories[j]
					if sub.IsMain() || sub.ParentIDValue() != main.ID {
						continue
					}
					count := counts[sub.ID]
					subs = append(subs, adminSubcategoryView{
						ID:   sub.ID,
						Key:  sub.Key,
						Name: sub.Name,
						Sort: sub.Sort,
						Products: adminGroupCountsView{
							Total: count.Total,
							On:    count.On,
							Off:   count.Off,
						},
						CreatedAt: formatTime(sub.CreatedAt),
						UpdatedAt: formatTime(sub.UpdatedAt),
					})
				}
				items = append(items, adminCategoryView{
					ID:            main.ID,
					Key:           main.Key,
					Name:          main.Name,
					Sort:          main.Sort,
					CreatedAt:     formatTime(main.CreatedAt),
					UpdatedAt:     formatTime(main.UpdatedAt),
					Subcategories: subs,
				})
			}
			return adminCategoryListView{Items: items}, true
		})
}

// updateCategory 处理 PUT /api/v1/admin/categories/:id：改名 / 改排序 / 子分类改挂主分类。
// 仅 admin（分类管理属目录结构，财务/客服只读，见契约 10.5 / 12.7）。
func (h *categoryHandler) updateCategory(c *gin.Context) {
	id, ok := categoryIDParam(c)
	if !ok {
		return
	}

	var req updateCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.ParentID == nil && req.Sort == nil {
		response.Fail(c, response.CodeInvalidParam, "至少提供一个字段：name / parent_id / sort")
		return
	}

	update := store.CategoryUpdate{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			response.Fail(c, response.CodeInvalidParam, "分类名不能为空")
			return
		}
		if utf8.RuneCountInString(name) > maxCategoryNameLen {
			response.Fail(c, response.CodeInvalidParam,
				fmt.Sprintf("分类名最长 %d 个字符", maxCategoryNameLen))
			return
		}
		update.Name = &name
	}
	if req.Sort != nil {
		if *req.Sort < -maxCategorySort || *req.Sort > maxCategorySort {
			response.Fail(c, response.CodeInvalidParam,
				fmt.Sprintf("sort 需为 %d 到 %d 之间的整数", -maxCategorySort, maxCategorySort))
			return
		}
		update.Sort = req.Sort
	}
	if req.ParentID != nil {
		update.ParentID = req.ParentID
	}

	ctx := c.Request.Context()
	updated, err := h.store.UpdateProductCategory(ctx, id, update)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, "分类不存在")
		return
	case errors.Is(err, store.ErrCategoryNotSub):
		response.Fail(c, response.CodeValidationFailed,
			"主分类不能设置 parent_id（只有子分类可以改挂到别的主分类）")
		return
	case errors.Is(err, store.ErrCategoryParentInvalid):
		response.Fail(c, response.CodeValidationFailed,
			"parent_id 必须指向一个存在的主分类（不能是自身或子分类）")
		return
	case errors.Is(err, store.ErrNoFieldsToUpdate):
		response.Fail(c, response.CodeInvalidParam, "至少提供一个字段：name / parent_id / sort")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	// R19 批 2：分类名 / 排序 / 归属同时出现在分类树与商品视图（商品带分类归属）——两族一起失效。
	h.cache.invalidate(ctx, cachePrefixCategories, cachePrefixProducts)
	setOplogSummary(c, fmt.Sprintf("已更新分类 #%d（%s）", updated.ID, updated.Name))
	response.Success(c, h.categoryView(c, updated))
}

// categoryView 组装单个分类的对外视图（主分类带 subcategories，子分类带商品计数）。
func (h *categoryHandler) categoryView(c *gin.Context, item *model.ProductCategory) any {
	ctx := c.Request.Context()
	if item.IsMain() {
		categories, err := h.store.ListProductCategories(ctx)
		if err != nil {
			h.logger.Warn("读取分类树失败", "error", err)
			categories = nil
		}
		subs := make([]adminSubcategoryView, 0)
		for i := range categories {
			sub := &categories[i]
			if sub.IsMain() || sub.ParentIDValue() != item.ID {
				continue
			}
			subs = append(subs, adminSubcategoryView{
				ID: sub.ID, Key: sub.Key, Name: sub.Name, Sort: sub.Sort,
				CreatedAt: formatTime(sub.CreatedAt), UpdatedAt: formatTime(sub.UpdatedAt),
			})
		}
		return adminCategoryView{
			ID: item.ID, Key: item.Key, Name: item.Name, Sort: item.Sort,
			CreatedAt: formatTime(item.CreatedAt), UpdatedAt: formatTime(item.UpdatedAt),
			Subcategories: subs,
		}
	}

	counts, err := h.store.ProductCategoryCounts(ctx)
	if err != nil {
		h.logger.Warn("读取分类商品计数失败", "error", err)
	}
	count := counts[item.ID]
	return adminSubcategoryView{
		ID: item.ID, Key: item.Key, Name: item.Name, Sort: item.Sort,
		Products:  adminGroupCountsView{Total: count.Total, On: count.On, Off: count.Off},
		CreatedAt: formatTime(item.CreatedAt), UpdatedAt: formatTime(item.UpdatedAt),
	}
}

// categoryIDParam 解析并校验路径参数 :id（正整数）。
func categoryIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "分类 ID 必须为正整数")
		return 0, false
	}
	return id, true
}
