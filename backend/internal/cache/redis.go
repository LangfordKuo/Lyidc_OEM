package cache

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 驱动的连接口径（契约 24.2）：
//   - 用官方客户端 github.com/redis/go-redis/v9；
//   - 超时**短**（缺省 1 秒，设置项可调 1-10 秒）：拨号、读、写、连接池排队都用同一个超时；
//   - **不做命令重试**（MaxRetries=0）：重试会把 1 秒的超时预算放大成数秒，
//     缓存层宁可立刻失败并降级到文件缓存，也不拖住业务请求；
//   - 只操作本系统的 key_prefix 命名空间：Flush 与 Keys 都用 SCAN MATCH `<prefix>*`，
//     **绝不 FLUSHDB**（同一个 DB 里可能有别的应用的数据）。
const (
	// redisPoolSize 是连接池上限（缓存操作短小，不需要更大）。
	redisPoolSize = 16
	// redisMinIdleConns 是常驻空闲连接数（避免每次冷启动都要重新拨号）。
	redisMinIdleConns = 1
	// redisScanCount 是 SCAN / 遍历的建议批大小；DelBatch 是一次 DEL 的键数量上限。
	redisScanCount = 500
	redisDelBatch  = 500
)

// redisStore 是 Redis 缓存驱动（官方 go-redis v9 客户端）。
type redisStore struct {
	client *redis.Client
	prefix string
}

// newRedisStore 构造 Redis 驱动。**构造不连接**（go-redis 惰性建连）：
// 是否可用由调用方（Manager）用 Ping 探活判定，失败即降级到文件缓存（契约 24.5）。
func newRedisStore(cfg RedisConfig) (*redisStore, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("Redis host 不能为空")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = DefaultKeyPrefix
	}
	client := redis.NewClient(&redis.Options{
		Addr:         net.JoinHostPort(host, strconv.Itoa(cfg.Port)),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		PoolSize:     redisPoolSize,
		MinIdleConns: redisMinIdleConns,
		PoolTimeout:  timeout,
		// 缓存层不重试：保持「一次超时」的预算，失败即降级（由自愈时机切回）。
		MaxRetries: 0,
		// 拨号同样只试一次（客户端缺省会试 5 次并退避）：否则「连接被拒绝」会被退避拖成
		// 上下文超时，既慢又把可读原因变成「连接超时」（真实故障诊断更难）。
		DialerRetries: 1,
	})
	return &redisStore{client: client, prefix: prefix}, nil
}

// Get 读取一个键；键不存在返回 found=false。
func (r *redisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := r.client.Get(ctx, r.key(key)).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return value, true, nil
}

// Set 写入一个键（ttl <= 0 表示不过期）。
func (r *redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return r.client.Set(ctx, r.key(key), value, ttl).Err()
}

// Delete 删除若干键（键不存在不算失败）。
func (r *redisStore) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, 0, len(keys))
	for _, key := range keys {
		full = append(full, r.key(key))
	}
	return r.client.Del(ctx, full...).Err()
}

// Flush 清空本系统命名空间（`<prefix>*`）下的全部键：SCAN 游标推进 + 分批 DEL。
// 只删已经收集到的键，因此遍历期间删除不会漏掉后续键；**绝不 FLUSHDB**。
func (r *redisStore) Flush(ctx context.Context) error {
	cursor := uint64(0)
	for {
		keys, next, err := r.client.Scan(ctx, cursor, r.matchPattern(), redisScanCount).Result()
		if err != nil {
			return err
		}
		for start := 0; start < len(keys); start += redisDelBatch {
			end := start + redisDelBatch
			if end > len(keys) {
				end = len(keys)
			}
			if err := r.client.Del(ctx, keys[start:end]...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Ping 探活：PING 往返成功即视为可用（连接 / 认证 / 超时问题都在这里暴露）。
func (r *redisStore) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Keys 返回命名空间内的键数量（SCAN 计数；SCAN 可能返回重复键，按集合去重）。
func (r *redisStore) Keys(ctx context.Context) (int64, error) {
	seen := make(map[string]struct{})
	cursor := uint64(0)
	for {
		keys, next, err := r.client.Scan(ctx, cursor, r.matchPattern(), redisScanCount).Result()
		if err != nil {
			return 0, err
		}
		for _, key := range keys {
			seen[key] = struct{}{}
		}
		cursor = next
		if cursor == 0 {
			return int64(len(seen)), nil
		}
	}
}

// Close 关闭连接池。
func (r *redisStore) Close() error { return r.client.Close() }

// ServerVersion 取服务端版本（INFO server 的 redis_version），供状态接口回显。
// 版本只是展示信息：取不到（信息里没有该字段）时返回空串而不是错误。
func (r *redisStore) ServerVersion(ctx context.Context) (string, error) {
	info, err := r.client.Info(ctx, "server").Result()
	if err != nil {
		// 少数服务端实现不支持分区 INFO：退回整份 INFO，能解析到就解析，解析不到就空串。
		if info, err = r.client.Info(ctx).Result(); err != nil {
			return "", err
		}
	}
	return parseRedisVersion(info), nil
}

// parseRedisVersion 从 INFO 文本里取出 redis_version（取不到返回空串）。
func parseRedisVersion(info string) string {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "redis_version:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// key 拼接命名空间前缀。
func (r *redisStore) key(key string) string { return r.prefix + key }

// matchPattern 是本命名空间的 SCAN 匹配模式。
func (r *redisStore) matchPattern() string { return r.prefix + "*" }

// describeRedisError 把驱动错误翻译成可读的中文原因（契约 24.5 的降级原因口径）。
func describeRedisError(err error) string {
	if err == nil {
		return ""
	}
	// 超时：上下文预算耗尽、net 层超时（i/o timeout）都算「连接超时」。
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || isNetTimeout(err) {
		return "连接超时"
	}
	// 连接类错误：sentinel 判定优先，文本兜底。
	// Windows 上必须靠文本——Go 的 syscall.ECONNREFUSED 是为兼容发明的值，
	// 与 winsock 实际返回的 WSAECONNREFUSED(10061) 并不相等，errors.Is 匹配不上。
	text := strings.ToUpper(err.Error())
	switch {
	case errors.Is(err, syscall.ECONNREFUSED),
		strings.Contains(text, "CONNECTION REFUSED"),
		strings.Contains(text, "ACTIVELY REFUSED"):
		return "连接被拒绝"
	case errors.Is(err, syscall.ECONNRESET),
		strings.Contains(text, "CONNECTION RESET"),
		strings.Contains(text, "FORCIBLY CLOSED"):
		return "连接被重置"
	}
	// 服务端错误原样返回文本：认证类错误在这里归一为「认证失败」（错误文本不含口令）。
	switch {
	case strings.Contains(text, "WRONGPASS"),
		strings.Contains(text, "NOAUTH"),
		strings.Contains(text, "CLIENT SENT AUTH"):
		return "认证失败"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "无法解析主机名"
	}
	return "连接失败：" + err.Error()
}

// isNetTimeout 判断是否为 net 层超时错误（i/o timeout）。
func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
