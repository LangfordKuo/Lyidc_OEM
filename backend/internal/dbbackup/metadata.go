package dbbackup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Metadata 是备份的元信息，随备份文件以 sidecar（`<文件名>.json`）存放在同一目录（契约 21.2）。
//
// 为什么不用数据库表：恢复是**覆盖式**的——恢复一份旧备份会把库内记录一并带回旧状态，
// 于是「恢复点之后创建的备份」会在列表里凭空消失，而文件其实还在磁盘上；元信息随文件存放则
// 始终与文件一致，且库不可用（正是最需要看备份的时候）也能正常列出。
type Metadata struct {
	// Format 是元信息格式版本，恒为 "lyidc-backup/v1"。
	Format string `json:"format"`
	// FileName 是备份文件名（冗余一份，便于文件被搬动后核对）。
	FileName string `json:"file_name"`
	// CreatedAt 是备份时间（RFC3339，UTC）。
	CreatedAt string `json:"created_at"`
	// Database 是备份时的库名。
	Database string `json:"database"`
	// SchemaVersion 是备份时的 schema_migrations 版本。
	SchemaVersion int64 `json:"schema_version"`
	// TableCount / RowCount 是备份时的表数与总行数。
	TableCount int   `json:"table_count"`
	RowCount   int64 `json:"row_count"`
	// FileSize 是备份文件字节数。
	FileSize int64 `json:"file_size"`
	// Operator 是发起备份的管理员用户名。
	Operator string `json:"operator"`
	// AppVersion 是备份时的应用版本（buildinfo.Version）。
	AppVersion string `json:"app_version"`
}

// metaFormat 是元信息格式版本号。
const metaFormat = "lyidc-backup/v1"

// metaPath 返回备份 ID 对应的元信息文件路径。
func (s *Service) metaPath(dir, id string) string {
	return filepath.Join(dir, id+MetaExt)
}

// readMeta 读取备份的元信息（不存在时返回包装后的 os.ErrNotExist）。
func readMeta(dir, id string) (Metadata, error) {
	path := filepath.Join(dir, id+MetaExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Metadata{}, fmt.Errorf("元信息不是合法 JSON: %w", err)
	}
	if meta.Format != metaFormat {
		return Metadata{}, errors.New("元信息格式版本不符")
	}
	return meta, nil
}

// writeMeta 原子写入备份元信息（同目录临时文件 + rename，权限 0640）。
func writeMeta(dir string, record Backup) error {
	meta := Metadata{
		Format:        metaFormat,
		FileName:      record.FileName,
		CreatedAt:     record.CreatedAt,
		Database:      record.Database,
		SchemaVersion: record.SchemaVersion,
		TableCount:    record.TableCount,
		RowCount:      record.RowCount,
		FileSize:      record.FileSize,
		Operator:      record.Operator,
		AppVersion:    record.AppVersion,
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元信息失败: %w", err)
	}
	tmp := filepath.Join(dir, record.ID+MetaExt+".tmp")
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, record.ID+MetaExt)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
