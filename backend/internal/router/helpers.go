package router

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// bindJSON 解析请求体；失败时写出 40001 并返回 false。
func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		response.Fail(c, response.CodeInvalidParam, "请求体不是合法的 JSON 或字段类型不匹配")
		return false
	}
	return true
}

// failDB 记录并以数据库错误码（50001）响应。
func failDB(c *gin.Context, logger *slog.Logger, err error) {
	logger.Error("数据库操作失败", "error", err, "method", c.Request.Method, "path", c.Request.URL.Path)
	response.Fail(c, response.CodeDatabaseError, response.Message(response.CodeDatabaseError))
}

// failInternal 记录并以内部错误码（500）响应。
func failInternal(c *gin.Context, logger *slog.Logger, err error) {
	logger.Error("服务器内部错误", "error", err, "method", c.Request.Method, "path", c.Request.URL.Path)
	response.Fail(c, response.CodeInternalError, response.Message(response.CodeInternalError))
}

// pagingParams 解析分页参数（page / page_size，缺省 1 / 20，上限 100）；
// 非法时写出 40001 并返回 ok=false（分页口径全站统一，契约 1.3）。
func pagingParams(c *gin.Context) (int, int, bool) {
	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return 0, 0, false
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return 0, 0, false
	}
	return page, pageSize, true
}

// paramError 描述查询参数非法（供 message 拼接）。
type paramError struct {
	field    string
	min, max int
}

// Error 实现 error。
func (e *paramError) Error() string {
	return e.field + " 需为 " + strconv.Itoa(e.min) + "-" + strconv.Itoa(e.max) + " 之间的整数"
}

// intQuery 解析整型查询参数：缺省返回 def，非法或越界返回 *paramError。
func intQuery(c *gin.Context, key string, def, min, max int) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, &paramError{field: key, min: min, max: max}
	}
	return value, nil
}
