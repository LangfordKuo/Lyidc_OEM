package router

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

const (
	statusOK       = "ok"
	statusDegraded = "degraded"
	dbUp           = "up"
	dbDown         = "down"
)

// errDatabaseUnavailable 表示数据库未配置或探测失败。
var errDatabaseUnavailable = errors.New("数据库不可用")

// healthData 是健康检查的数据体。
type healthData struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Time   string `json:"time"`
}

// healthHandler 处理 GET /api/v1/health：
// 数据库可用返回 HTTP 200 + data{status:"ok",db:"up",time}；
// 数据库不可用返回 HTTP 503 + data{status:"degraded",db:"down",time}。
func healthHandler(opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), opts.HealthTimeout)
		defer cancel()

		data := healthData{
			Status: statusOK,
			DB:     dbUp,
			Time:   time.Now().Format(time.RFC3339),
		}

		if err := pingDatabase(ctx, opts); err != nil {
			data.Status = statusDegraded
			data.DB = dbDown
			response.FailWithData(c, http.StatusServiceUnavailable, response.CodeInternalError,
				response.Message(response.CodeInternalError), data)
			return
		}

		response.Success(c, data)
	}
}

func pingDatabase(ctx context.Context, opts Options) error {
	if opts.Ping == nil {
		return errDatabaseUnavailable
	}
	if err := opts.Ping(ctx); err != nil {
		return errors.Join(errDatabaseUnavailable, err)
	}
	return nil
}
