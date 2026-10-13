package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KeyDBBackup 数据库备份设置键（值结构见 DBBackup，契约 21.6）。
const KeyDBBackup = "db_backup"

// DBBackup 是 db_backup 设置的取值结构（数据库备份，R14 / 契约 21）。
//
// 本批只承载备份目录一项：**留空表示用缺省目录**（安装目录下的 backups/，契约 21.5）。
// 目录解析与会话/请求无关的合法性校验（是否落在 Web 根内、是否可写）由 internal/dbbackup 在
// **启动与每次使用**时执行——这里只负责取值与形状校验，不引入对文件系统的依赖。
type DBBackup struct {
	// Dir 是备份目录：留空走缺省；可写绝对路径，也可写相对路径（相对安装目录解析）。
	Dir string `json:"dir"`
}

// DBBackupState 是 db_backup 的读取结果。
type DBBackupState struct {
	Value DBBackup
	Meta  Meta
}

// DBBackup 读取 db_backup 设置；未配置时返回空目录（走缺省目录）。
func (r *Reader) DBBackup(ctx context.Context) (DBBackupState, error) {
	raw, meta, err := r.raw(ctx, KeyDBBackup)
	if err != nil {
		return DBBackupState{}, err
	}
	value, err := decodeDBBackup(raw)
	if err != nil {
		return DBBackupState{}, err
	}
	return DBBackupState{Value: value, Meta: meta}, nil
}

// Validate 校验备份目录取值：留空合法（走缺省）；非空时不得含 NUL 等控制字符。
//
// 路径本身的解析、Web 根判定与可写性属于环境相关校验，由 dbbackup 包在**启动与使用**时执行
// （契约 21.5：落在 Web 根内的配置一律拒绝）。
func (b DBBackup) Validate() error {
	dir := strings.TrimSpace(b.Dir)
	for _, r := range dir {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: dir 不能包含控制字符", ErrFormat)
		}
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (b DBBackup) Encode() (string, error) { return encode(b) }

// decodeDBBackup 解析库内 JSON；空值按未配置处理。
func decodeDBBackup(raw string) (DBBackup, error) {
	value := DBBackup{}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return DBBackup{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyDBBackup, err)
	}
	value.Dir = strings.TrimSpace(value.Dir)
	if err := value.Validate(); err != nil {
		return DBBackup{}, fmt.Errorf("%w: %s 的 dir 值不合法", ErrCorrupt, KeyDBBackup)
	}
	return value, nil
}
