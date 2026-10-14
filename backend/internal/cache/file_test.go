package cache

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 文件缓存驱动（契约 24.2）：Set/Get/TTL/删除/Flush/并发/键数量/路径穿越
// ---------------------------------------------------------------------------

// silentTestLogger 返回丢弃全部日志的 logger（避免测试输出噪音）。
func silentTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestFileStore 构造一个文件缓存驱动（清理间隔很长，过期清理在用例里手动触发）。
func newTestFileStore(t *testing.T, dir string) *fileStore {
	t.Helper()
	store, err := newFileStore(dir, time.Hour, silentTestLogger())
	if err != nil {
		t.Fatalf("构造文件缓存失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// cacheFileCount 数目录里的数据文件个数（用于断言惰性删除与定期清理）。
func cacheFileCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取缓存目录失败: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), fileSuffix) {
			count++
		}
	}
	return count
}

// TestFileStoreSetGetDelete 覆盖最基本的读写与删除：读命中、读未命中、覆盖、删除幂等。
func TestFileStoreSetGetDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestFileStore(t, t.TempDir())

	if _, found, err := store.Get(ctx, "missing"); err != nil || found {
		t.Fatalf("未写入的键应未命中: found=%v, err=%v", found, err)
	}
	if err := store.Set(ctx, "greeting", []byte("你好"), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	value, found, err := store.Get(ctx, "greeting")
	if err != nil || !found || string(value) != "你好" {
		t.Fatalf("读回失败: found=%v, value=%q, err=%v", found, value, err)
	}
	// 覆盖写：值替换为新值，不残留旧内容。
	if err := store.Set(ctx, "greeting", []byte("world"), 0); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}
	if value, _, _ = store.Get(ctx, "greeting"); string(value) != "world" {
		t.Fatalf("覆盖后应读到新值，得到 %q", value)
	}
	// 删除：删除后未命中；重复删除不报错。
	if err := store.Delete(ctx, "greeting"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, found, _ := store.Get(ctx, "greeting"); found {
		t.Fatal("删除后不应再命中")
	}
	if err := store.Delete(ctx, "greeting"); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
}

// TestFileStoreExpiryLazyDelete 验证 TTL 过期：过期后读不到，且**惰性删除**立即清掉文件。
func TestFileStoreExpiryLazyDelete(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := newTestFileStore(t, dir)

	if err := store.Set(ctx, "short", []byte("v"), 40*time.Millisecond); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := store.Set(ctx, "forever", []byte("v"), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, found, _ := store.Get(ctx, "short"); !found {
		t.Fatal("未过期前应命中")
	}
	time.Sleep(80 * time.Millisecond)
	if _, found, err := store.Get(ctx, "short"); err != nil || found {
		t.Fatalf("过期后应未命中: found=%v, err=%v", found, err)
	}
	if count := cacheFileCount(t, dir); count != 1 {
		t.Fatalf("惰性删除后应只剩 1 个数据文件，得到 %d", count)
	}
	if _, found, _ := store.Get(ctx, "forever"); !found {
		t.Fatal("不过期的键不应受影响")
	}
}

// TestFileStoreCleanupExpired 验证定期清理：只删过期文件，不过期的与目录里其它文件都不动。
func TestFileStoreCleanupExpired(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := newTestFileStore(t, dir)

	// 目录里放一个与缓存无关的文件：清理绝不能碰它。
	foreign := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(foreign, []byte("别删我"), 0o600); err != nil {
		t.Fatalf("写入外部文件失败: %v", err)
	}
	if err := store.Set(ctx, "expired", []byte("v"), 30*time.Millisecond); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := store.Set(ctx, "alive", []byte("v"), time.Hour); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := store.Set(ctx, "forever", []byte("v"), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	time.Sleep(60 * time.Millisecond)

	store.cleanupExpired()
	if count := cacheFileCount(t, dir); count != 2 {
		t.Fatalf("定期清理后应剩 2 个未过期文件，得到 %d", count)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("定期清理不应删除缓存目录里的其它文件: %v", err)
	}
}

// TestFileStoreFlush 验证清空：数据文件与残留临时文件都清掉，目录里其它文件不动。
func TestFileStoreFlush(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := newTestFileStore(t, dir)

	foreign := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(foreign, []byte("别删我"), 0o600); err != nil {
		t.Fatalf("写入外部文件失败: %v", err)
	}
	// 伪造一个写入中被中断的临时文件：Flush 应把它一并清掉。
	leftover := filepath.Join(dir, tempPrefix+"leftover")
	if err := os.WriteFile(leftover, []byte("半截"), 0o600); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := store.Set(ctx, fmt.Sprintf("key-%d", i), []byte("v"), 0); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	if keys, err := store.Keys(ctx); err != nil || keys != 3 {
		t.Fatalf("清空前应有 3 个键: keys=%d, err=%v", keys, err)
	}

	if err := store.Flush(ctx); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if keys, err := store.Keys(ctx); err != nil || keys != 0 {
		t.Fatalf("清空后应无键: keys=%d, err=%v", keys, err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("清空应删除残留临时文件: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("清空不应删除缓存目录里的其它文件: %v", err)
	}
}

// TestFileStoreKeysCountsOnlyUnexpired 验证键数量口径：只数未过期的数据文件。
func TestFileStoreKeysCountsOnlyUnexpired(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := newTestFileStore(t, dir)

	for i := 0; i < 3; i++ {
		if err := store.Set(ctx, fmt.Sprintf("key-%d", i), []byte("v"), 0); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	if err := store.Set(ctx, "short", []byte("v"), 30*time.Millisecond); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if keys, err := store.Keys(ctx); err != nil || keys != 4 {
		t.Fatalf("应数到 4 个键: keys=%d, err=%v", keys, err)
	}
	time.Sleep(60 * time.Millisecond)
	if keys, err := store.Keys(ctx); err != nil || keys != 3 {
		t.Fatalf("过期键不应计入: keys=%d, err=%v", keys, err)
	}
}

// TestFileStoreConcurrent 覆盖并发读写：同一批键被多个 goroutine 交替读、写、删，不 panic 不报错。
func TestFileStoreConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newTestFileStore(t, t.TempDir())

	const goroutines = 8
	const rounds = 40
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				key := fmt.Sprintf("shared-%d", i%4) // 一半的键是共享的，制造真实争用
				if i%3 == 0 {
					key = fmt.Sprintf("own-%d-%d", g, i)
				}
				if err := store.Set(ctx, key, []byte(fmt.Sprintf("g%d-i%d", g, i)), time.Minute); err != nil {
					errCh <- err
					return
				}
				if _, _, err := store.Get(ctx, key); err != nil {
					errCh <- err
					return
				}
				if i%5 == 0 {
					if err := store.Delete(ctx, key); err != nil {
						errCh <- err
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发读写失败: %v", err)
	}
	// 收尾：清空后再数一次键数量（0），确认并发后目录状态仍然一致。
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("并发后的清空失败: %v", err)
	}
	if keys, err := store.Keys(ctx); err != nil || keys != 0 {
		t.Fatalf("并发后清空应无残留: keys=%d, err=%v", keys, err)
	}
}

// TestFileStoreKeyCannotEscapeDirectory 覆盖路径穿越：任何键（`..`、斜杠、反斜杠、盘符、
// Windows 保留名、超长键、Unicode）都只会映射成缓存目录里的哈希文件名，绝不逃出目录。
func TestFileStoreKeyCannotEscapeDirectory(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	dir := filepath.Join(parent, "cache")
	store := newTestFileStore(t, dir)

	before, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("读取父目录失败: %v", err)
	}

	keys := []string{
		"../../escape",
		"..\\..\\escape",
		"a/b/c",
		"C:\\Windows\\System32\\evil",
		"CON",
		"nul",
		".",
		"..",
		strings.Repeat("k", 4096),
		"键-中文-🔒",
		"key with space\tand\ttab",
	}
	for _, key := range keys {
		if err := store.Set(ctx, key, []byte("v"), time.Hour); err != nil {
			t.Fatalf("写入键 %q 失败: %v", key, err)
		}
		if _, found, err := store.Get(ctx, key); err != nil || !found {
			t.Fatalf("读回键 %q 失败: found=%v, err=%v", key, found, err)
		}
	}

	// 1) 缓存目录里的条目全部是哈希文件名（无子目录、无原始键片段）。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取缓存目录失败: %v", err)
	}
	if len(entries) != len(keys) {
		t.Fatalf("缓存目录应有 %d 个文件，得到 %d", len(keys), len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("缓存目录里不应出现子目录: %s", entry.Name())
		}
		name := entry.Name()
		if !strings.HasSuffix(name, fileSuffix) || len(name) != 64+len(fileSuffix) {
			t.Fatalf("缓存文件名应是 64 位十六进制哈希: %s", name)
		}
		if strings.Contains(name, "escape") || strings.Contains(name, "evil") {
			t.Fatalf("缓存文件名不得包含键片段: %s", name)
		}
	}

	// 2) 父目录除了缓存目录本身，没有多出任何条目（没有文件被写到目录外）。
	after, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("读取父目录失败: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("父目录条目数从 %d 变为 %d：疑似有文件被写到缓存目录之外", len(before), len(after))
	}
	// 3) 缓存目录里没有残留临时文件。
	for _, entry := range after {
		if strings.HasPrefix(entry.Name(), tempPrefix) {
			t.Fatalf("父目录出现临时文件残留: %s", entry.Name())
		}
	}
}

// TestFileStoreCorruptFileTreatedAsMiss 覆盖损坏文件：头部不全 / magic 不对都按未命中处理并清掉。
func TestFileStoreCorruptFileTreatedAsMiss(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := newTestFileStore(t, dir)

	path := store.filePath("broken")
	if err := os.WriteFile(path, []byte("这不是缓存文件格式"), 0o600); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}
	if _, found, err := store.Get(ctx, "broken"); err != nil || found {
		t.Fatalf("损坏文件应视为未命中: found=%v, err=%v", found, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("损坏文件应被删除: %v", err)
	}
}

// TestFileStoreLargeValue 覆盖较大值（1 MiB）按字节原样往返。
func TestFileStoreLargeValue(t *testing.T) {
	ctx := context.Background()
	store := newTestFileStore(t, t.TempDir())

	value := make([]byte, 1<<20)
	for i := range value {
		value[i] = byte(i % 251)
	}
	if err := store.Set(ctx, "big", value, 0); err != nil {
		t.Fatalf("写入大值失败: %v", err)
	}
	got, found, err := store.Get(ctx, "big")
	if err != nil || !found || len(got) != len(value) {
		t.Fatalf("读回大值失败: found=%v, len=%d, err=%v", found, len(got), err)
	}
	for i := range got {
		if got[i] != value[i] {
			t.Fatalf("大值第 %d 字节不一致", i)
		}
	}
}

// TestFileStoreDeletePrefix 覆盖按键族前缀删除（R19 批 2 的主动失效，契约 24.6）：
// 只删键以该前缀开头的条目，其它键一个都不动；空前缀直接拒绝（清空全部请用 Flush）。
func TestFileStoreDeletePrefix(t *testing.T) {
	ctx := context.Background()
	store := newTestFileStore(t, t.TempDir())

	keys := []string{
		"products:catalog",
		"products:detail:1",
		"products:admin:list:p1/s20",
		"products:admin:groups",
		"categories:list",
		"settings:site",
	}
	for _, key := range keys {
		if err := store.Set(ctx, key, []byte("v-"+key), time.Hour); err != nil {
			t.Fatalf("写入键 %q 失败: %v", key, err)
		}
	}

	if err := store.DeletePrefix(ctx, "products:"); err != nil {
		t.Fatalf("按前缀删除失败: %v", err)
	}
	for _, key := range keys[:4] {
		if _, found, err := store.Get(ctx, key); err != nil || found {
			t.Fatalf("前缀内的键 %q 应已删除: found=%v, err=%v", key, found, err)
		}
	}
	for _, key := range keys[4:] {
		if value, found, err := store.Get(ctx, key); err != nil || !found || string(value) != "v-"+key {
			t.Fatalf("前缀外的键 %q 必须原样保留: found=%v, value=%q, err=%v", key, found, value, err)
		}
	}
	// 幂等：再删一次（此时无匹配）不报错。
	if err := store.DeletePrefix(ctx, "products:"); err != nil {
		t.Fatalf("重复按前缀删除应幂等: %v", err)
	}
	// 空前缀拒绝（语义等于全清，必须显式走 Flush）。
	if err := store.DeletePrefix(ctx, ""); err == nil {
		t.Fatal("空前缀必须拒绝")
	}
	if keys, err := store.Keys(ctx); err != nil || keys != 2 {
		t.Fatalf("空前缀被拒后不应影响任何键: keys=%d, err=%v", keys, err)
	}
}

// TestFileStoreDeletePrefixSurvivesReopen 覆盖「键记在文件里」这一点：
// **重新打开**（新驱动实例，内存里没有任何键表）之后，按前缀仍能删掉旧条目——
// 这正是批 2 需要把键写进文件（而不是只留哈希文件名）的原因。
func TestFileStoreDeletePrefixSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first := newTestFileStore(t, dir)
	if err := first.Set(ctx, "products:detail:7", []byte("旧值"), time.Hour); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := first.Set(ctx, "categories:list", []byte("保留"), time.Hour); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	second := newTestFileStore(t, dir)
	if err := second.DeletePrefix(ctx, "products:"); err != nil {
		t.Fatalf("重新打开后按前缀删除失败: %v", err)
	}
	if _, found, _ := second.Get(ctx, "products:detail:7"); found {
		t.Fatal("重新打开后应能按前缀删掉旧条目（键记在文件里）")
	}
	if _, found, _ := second.Get(ctx, "categories:list"); !found {
		t.Fatal("别的键串不应被删")
	}
}

// TestFileStoreOldFormatTreatedAsMiss 覆盖格式升级：批 1 的旧格式（magic LYIDCC01、无键字段）
// 文件一律按损坏处理——删掉并按未命中返回，绝不把旧内容当值读出来。
func TestFileStoreOldFormatTreatedAsMiss(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := newTestFileStore(t, dir)

	// 手工构造一份批 1 格式的文件（16 字节头 + 值），放在新驱动该键的哈希文件名上。
	header := make([]byte, headerSize)
	copy(header, "LYIDCC01")
	old := append(header, []byte("上一版缓存的值")...)
	path := store.filePath("products:catalog")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatalf("写入旧格式文件失败: %v", err)
	}
	if _, found, err := store.Get(ctx, "products:catalog"); err != nil || found {
		t.Fatalf("旧格式文件应视为未命中: found=%v, err=%v", found, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("旧格式文件应被清掉: %v", err)
	}
}

// TestFileStorePingCreatesDir 覆盖探活：目录不存在时探活会创建它。
func TestFileStorePingCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	store := newTestFileStore(t, dir)

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("探活失败: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("探活应确保目录存在: %v", err)
	}
}
