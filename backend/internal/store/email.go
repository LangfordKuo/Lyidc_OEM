package store

import (
	"context"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// AppendEmailLog 写一条邮件发送留痕（成功与失败都写，契约 17.3）。
//
// 本表不参与任何业务事务：调用方写入失败只记服务日志（通知不可拖垮业务）。
func (s *Store) AppendEmailLog(ctx context.Context, toAddr, subject, status, errorText string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	log := model.EmailLog{
		ToAddr:    toAddr,
		Subject:   subject,
		Status:    status,
		ErrorText: errorText,
		CreatedAt: time.Now().UTC(),
	}
	return db.Create(&log).Error
}
