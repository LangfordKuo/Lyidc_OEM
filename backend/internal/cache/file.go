package cache

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 文件缓存的落盘口径（契约 24.2）：
//
//	<目录>/<sha256(键).hex>.cache —— 数据文件，内容是文件头 + 键字节 + 值字节：
//	  偏移 0-7：  magic "LYIDCC02"（识别与格式版本）
//	  偏移 8-15： 过期时刻（Unix 纳秒，大端；0 = 永不过期）
//	  偏移 16-17：键长度（uint16 大端，键按 UTF-8 字节计）
//	  偏移 18..： 键字节
//	  随后：      值字节（按字节原样）
//	<目录>/.tmp-* —— 写入中的临时文件（同目录临时文件 + rename 原子替换）
//
// 键**不参与文件名**（文件名只取哈希）：任何键（含 `/`、`..`、盘符、Windows 保留名、超长键）
// 都不可能逃出缓存目录，也绝不会因非法字符写失败（契约 24.2）。
// 键记在**文件内容里**（R19 批 2）——这是 DeletePrefix（按键族前缀主动失效）的唯一依据：
// 文件名是哈希、不可反推，只有把键落在文件里，重启后与新进程才能按前缀命中要删的文件。
// 读到 magic 或 sha256(键) 与文件名不符的条目一律按**损坏**处理（删掉并按未命中返回），
// 因此批 1 写的旧格式文件会在升级后自然失效、被清掉——缓存内容本就随时可弃。
//
// 过期判定以文件头为准（不依赖 mtime：mtime 会被复制 / 解包 / 备份改写），
// 删除策略是**惰性删除（读到过期即删）+ 定期清理（后台协程扫目录删过期文件）**（契约 24.2）。
const (
	// DefaultCleanupInterval 是文件缓存定期清理过期文件的后台间隔（契约 24.2）。
	DefaultCleanupInterval = 5 * time.Minute

	// fileSuffix 是数据文件后缀；tempPrefix 是写入中临时文件的前缀（两者之外的条目不动）。
	fileSuffix = ".cache"
	tempPrefix = ".tmp-"

	// fileMagic 是文件头前 8 字节；headerSize 是文件头总长度；keyLenSize 是键长度字段宽度。
	fileMagic  = "LYIDCC02"
	headerSize = 16
	keyLenSize = 2

	// maxKeyBytes 是单个键的长度上限（受 uint16 的键长度字段约束）。
	maxKeyBytes = 1<<16 - 1

	// dirMode 是缓存目录权限（只给本进程用户：缓存里有业务数据，不共享）。
	dirMode = 0o700

	// renameRetries 是原子替换的重试次数：Windows 上目标文件正被读取时 rename 会瞬时被拒。
	renameRetries = 5

	// readRetries 是读取重试次数：Windows 上并发删除 / 替换会让 open 瞬时失败（删除挂起等）。
	readRetries = 5

	// removeRetries 是删除重试次数：Windows 上文件正被读取时删除会瞬时被拒（共享冲突）。
	removeRetries = 5

	// writeStripes 是写锁分片数：同一键（按哈希分片）的写入 / 删除串行化。
	// 无锁时两个 goroutine 同时对同一键做「临时文件 + rename」，Windows 上会撞出
	// 「Access is denied」；缓存写入本就幂等，串行化的代价可忽略。
	writeStripes = 64
)

// fileStore 是文件缓存驱动：键 → 哈希文件名，值按字节存，文件头记过期时刻，写入原子替换。
// 并发安全：写是「同目录临时文件 + rename」的原子替换，读是普通的文件读；
// 过期清理与读写可并发（删除失败不影响读写结果）。
type fileStore struct {
	dir    string
	logger *slog.Logger

	// writeLocks 按哈希分片保护写入 / 删除（读者不需要锁：Windows 下读者以共享删除模式打开，
	// 与「删除 / 改名替换」并存；其余平台本就并存）。
	writeLocks [writeStripes]sync.Mutex

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// newFileStore 构造文件缓存驱动：目录不存在时自动创建，并启动定期清理过期文件的后台协程。
// 启动时不做任何预热（契约 24.1：不需要预热）。
func newFileStore(dir string, cleanupInterval time.Duration, logger *slog.Logger) (*fileStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败：%w", err)
	}
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultCleanupInterval
	}
	store := &fileStore{
		dir:    dir,
		logger: logger,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go store.cleanupLoop(cleanupInterval)
	return store, nil
}

// Get 读取一个键：不存在返回 found=false；已过期 / 头部损坏 / 键与文件名不符则删除该文件并按未命中返回。
//
// 实现细节（Windows）：**先把整个文件读完并关闭，再决定删不删**——文件仍打开时删除会被
// 系统拒绝（删除挂起 / 共享冲突），先读后删才能保证惰性删除真的落地。
func (f *fileStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	path := f.filePath(key)
	data, err := readFileRetry(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("读取缓存文件失败：%w", err)
	}
	entry, ok := decodeEntry(data)
	if !ok || entry.key != key {
		// 头部不全 / magic 不对 / 键与文件名不符 = 文件损坏（写入被中断的残留、外部改写，
		// 或批 1 的旧格式文件）。这里用**传入的键**再校验一次文件名哈希，杜绝读串。
		f.removeQuietly(path)
		return nil, false, nil
	}
	if !entry.expiry.IsZero() && !time.Now().Before(entry.expiry) {
		// 惰性删除：读到过期文件立即删掉，不等定期清理。
		f.removeQuietly(path)
		return nil, false, nil
	}
	value := make([]byte, len(entry.value))
	copy(value, entry.value)
	return value, true, nil
}

// Set 写入一个键：ttl <= 0 表示不过期；写入走「临时文件 + rename」的原子替换，
// 同一键的写入按分片锁串行（避免并发替换互相撞出系统级错误）。
func (f *fileStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(key) > maxKeyBytes {
		return fmt.Errorf("缓存键过长：%d 字节超过上限 %d", len(key), maxKeyBytes)
	}
	unlock := f.lockKey(key)
	defer unlock()

	header := make([]byte, headerSize+keyLenSize+len(key))
	copy(header, fileMagic)
	if ttl > 0 {
		binary.BigEndian.PutUint64(header[8:], uint64(time.Now().Add(ttl).UnixNano()))
	}
	binary.BigEndian.PutUint16(header[headerSize:], uint16(len(key)))
	copy(header[headerSize+keyLenSize:], key)

	temp, err := os.CreateTemp(f.dir, tempPrefix+"*")
	if err != nil {
		return fmt.Errorf("创建缓存临时文件失败：%w", err)
	}
	tempPath := temp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = temp.Close()
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := temp.Write(header); err != nil {
		return fmt.Errorf("写入缓存文件失败：%w", err)
	}
	if _, err := temp.Write(value); err != nil {
		return fmt.Errorf("写入缓存文件失败：%w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("写入缓存文件失败：%w", err)
	}
	if err := renameWithRetry(tempPath, f.filePath(key)); err != nil {
		return fmt.Errorf("替换缓存文件失败：%w", err)
	}
	cleanupTemp = false
	return nil
}

// Delete 删除若干键（键不存在不算失败）。
func (f *fileStore) Delete(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var firstErr error
	for _, key := range keys {
		// 与同一键的写入互斥：避免「删除挂起」与「改名替换」相撞（Windows 下会 ACCESS_DENIED）。
		unlock := f.lockKey(key)
		err := removeWithRetry(f.filePath(key))
		unlock()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("删除缓存文件失败：%w", err)
		}
	}
	return firstErr
}

// DeletePrefix 删除**键以 prefix 开头**的全部缓存条目（R19 批 2 的按键族主动失效，契约 24.6）。
//
// 文件名是键的哈希、不能反推，因此这里扫目录、逐个读文件头里的键来匹配（只读文件头与键字节，
// 不读值字节）。损坏 / 过期的条目若键匹配同样删掉；键不匹配的一律不动。
// prefix 为空串表示「全部」，调用方要用它应显式走 Flush —— 这里直接拒绝，避免误清空。
func (f *fileStore) DeletePrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return errEmptyPrefix
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return fmt.Errorf("读取缓存目录失败：%w", err)
	}
	var firstErr error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fileSuffix) {
			continue
		}
		path := filepath.Join(f.dir, entry.Name())
		key, ok := f.readKey(path)
		if !ok || !strings.HasPrefix(key, prefix) {
			continue
		}
		// 与同一键的写入互斥：避免「删除挂起」与「改名替换」相撞（Windows 下会 ACCESS_DENIED）。
		unlock := f.lockKey(key)
		err := removeWithRetry(path)
		unlock()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("删除缓存文件失败：%w", err)
		}
	}
	return firstErr
}

// Flush 清空本驱动写入的全部缓存（数据文件 + 残留临时文件）；目录里其它文件不动。
// 清空期间独占全部写锁，避免与在途写入互相干扰。
func (f *fileStore) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlockAll := f.lockAll()
	defer unlockAll()

	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return fmt.Errorf("读取缓存目录失败：%w", err)
	}
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() || !isCacheFile(entry.Name()) {
			continue
		}
		if err := removeWithRetry(filepath.Join(f.dir, entry.Name())); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("删除缓存文件失败：%w", err)
		}
	}
	return firstErr
}

// Ping 探活：目录可创建（存在且可写目录项）即视为可用。
func (f *fileStore) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(f.dir, dirMode); err != nil {
		return fmt.Errorf("缓存目录不可用：%w", err)
	}
	info, err := os.Stat(f.dir)
	if err != nil {
		return fmt.Errorf("缓存目录不可用：%w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("缓存目录不可用：%s 不是目录", f.dir)
	}
	return nil
}

// Keys 返回当前缓存键数量（**计数口径**：目录里未过期的数据文件数；过期文件按惰性删除的
// 同一判定视作不存在；只读文件头与键字节，不读值字节）。
func (f *fileStore) Keys(ctx context.Context) (int64, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return 0, fmt.Errorf("读取缓存目录失败：%w", err)
	}
	var count int64
	now := time.Now()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fileSuffix) {
			continue
		}
		_, expiry, ok := f.readEntryMeta(filepath.Join(f.dir, entry.Name()))
		if !ok || (!expiry.IsZero() && !now.Before(expiry)) {
			continue
		}
		count++
	}
	return count, nil
}

// Close 停止后台清理协程（可重复调用）。
func (f *fileStore) Close() error {
	f.closeOnce.Do(func() {
		close(f.stop)
		<-f.done
	})
	return nil
}

// cleanupLoop 定期清理过期文件（惰性删除之外的兜底）。
func (f *fileStore) cleanupLoop(interval time.Duration) {
	defer close(f.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
			f.cleanupExpired()
		}
	}
}

// cleanupExpired 扫一遍目录删除过期文件（读文件头判定，不读值字节）；失败只记服务日志。
func (f *fileStore) cleanupExpired() {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		f.logger.Warn("缓存目录定期清理失败", "dir", f.dir, "error", err)
		return
	}
	now := time.Now()
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fileSuffix) {
			continue
		}
		path := filepath.Join(f.dir, entry.Name())
		_, expiry, ok := f.readEntryMeta(path)
		switch {
		case !ok:
			// 头部损坏的残留（写入被中断 / 外部改写）：与 Get 的处置一致，删掉。
			f.removeQuietly(path)
			removed++
		case !expiry.IsZero() && !now.Before(expiry):
			f.removeQuietly(path)
			removed++
		}
	}
	if removed > 0 {
		f.logger.Debug("缓存定期清理完成", "dir", f.dir, "removed", removed)
	}
}

// lockKey 锁住该键所属的写分片，返回解锁函数。
func (f *fileStore) lockKey(key string) func() {
	sum := sha256.Sum256([]byte(key))
	stripe := &f.writeLocks[int(sum[0])%writeStripes]
	stripe.Lock()
	return stripe.Unlock
}

// lockAll 锁住全部分片（清空用），返回统一解锁函数。
func (f *fileStore) lockAll() func() {
	for i := range f.writeLocks {
		f.writeLocks[i].Lock()
	}
	return func() {
		for i := range f.writeLocks {
			f.writeLocks[i].Unlock()
		}
	}
}

// filePath 返回键对应的数据文件路径（键只以 sha256 十六进制参与文件名，杜绝路径穿越）。
func (f *fileStore) filePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(f.dir, hex.EncodeToString(sum[:])+fileSuffix)
}

// readEntryMeta 读取数据文件的「键 + 过期时刻」（只读文件头与键字节，不读值字节）；
// 文件缺失 / 头部不全 / magic 不对 / 键与文件名哈希不符时返回 ok=false。
func (f *fileStore) readEntryMeta(path string) (string, time.Time, bool) {
	file, err := openRead(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, headerSize+keyLenSize)
	if _, err := io.ReadFull(file, header); err != nil {
		return "", time.Time{}, false
	}
	if string(header[:8]) != fileMagic {
		return "", time.Time{}, false
	}
	keyBytes := make([]byte, binary.BigEndian.Uint16(header[headerSize:]))
	if _, err := io.ReadFull(file, keyBytes); err != nil {
		return "", time.Time{}, false
	}
	key := string(keyBytes)
	if !entryKeyMatchesPath(key, path) {
		return "", time.Time{}, false
	}
	return key, expiryOf(header[:headerSize]), true
}

// readKey 读取数据文件里记录的键（供 DeletePrefix 按前缀匹配）；失败返回 ok=false。
func (f *fileStore) readKey(path string) (string, bool) {
	key, _, ok := f.readEntryMeta(path)
	return key, ok
}

// entryKeyMatchesPath 校验「文件内容里的键」与「文件名（键的 sha256）」一致：
// 不一致说明文件被外部改名 / 损坏，按损坏处理（绝不按未验证的内容作答）。
func entryKeyMatchesPath(key, path string) bool {
	sum := sha256.Sum256([]byte(key))
	expected := hex.EncodeToString(sum[:]) + fileSuffix
	return filepath.Base(path) == expected
}

// cacheEntry 是一条缓存条目的解析结果（Get 用）。
type cacheEntry struct {
	key    string
	expiry time.Time
	value  []byte
}

// decodeEntry 解析缓存文件内容（文件头 + 键 + 值）；格式不符 / 键哈希与文件名不符时返回 ok=false。
// 这里不校验文件名（调用方用**请求的键**比对，见 Get：读串比读不到更危险）。
func decodeEntry(data []byte) (cacheEntry, bool) {
	if len(data) < headerSize+keyLenSize || string(data[:8]) != fileMagic {
		return cacheEntry{}, false
	}
	keyLen := int(binary.BigEndian.Uint16(data[headerSize:]))
	start := headerSize + keyLenSize
	if len(data) < start+keyLen {
		return cacheEntry{}, false
	}
	return cacheEntry{
		key:    string(data[start : start+keyLen]),
		expiry: expiryOf(data[:headerSize]),
		value:  data[start+keyLen:],
	}, true
}

// removeQuietly 删除文件并吞掉错误（过期清理与损坏文件处置都不该影响调用方）。
func (f *fileStore) removeQuietly(path string) {
	if err := removeWithRetry(path); err != nil {
		f.logger.Warn("删除缓存文件失败", "path", path, "error", err)
	}
}

// removeWithRetry 删除文件（键不存在不算失败），失败按短退避重试：
// Windows 上文件正被其它 goroutine 读取时删除会瞬时被拒（共享冲突）。
func removeWithRetry(path string) error {
	var err error
	for attempt := 0; attempt < removeRetries; attempt++ {
		err = os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}

// expiryOf 解析文件头里的过期时刻（0 = 永不过期）。
func expiryOf(header []byte) time.Time {
	nanos := int64(binary.BigEndian.Uint64(header[8:16]))
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// isCacheFile 判断目录项是否属于本驱动（数据文件或写入中的临时文件）。
func isCacheFile(name string) bool {
	return strings.HasSuffix(name, fileSuffix) || strings.HasPrefix(name, tempPrefix)
}

// renameWithRetry 原子替换目标文件，失败按短退避重试：
// Windows 上目标文件正被读取（或被杀毒软件扫描）时 rename 会瞬时被拒绝。
func renameWithRetry(oldPath, newPath string) error {
	var err error
	for attempt := 0; attempt < renameRetries; attempt++ {
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}

// readFileRetry 读整个文件并在返回前关闭句柄。并发删除 / 替换会让 open 瞬时失败
// （删除挂起、共享冲突、杀毒软件扫描），按短退避重试几次；仍失败则返回最后一次错误
// （调用方按未命中处理）。
func readFileRetry(path string) ([]byte, error) {
	var err error
	for attempt := 0; attempt < readRetries; attempt++ {
		var data []byte
		data, err = readAll(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return data, err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return nil, err
}

// readAll 读整个文件（Windows 下用允许并发删除 / 改名的共享模式打开，见 openRead）。
func readAll(path string) ([]byte, error) {
	file, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}
