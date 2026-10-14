package oplog

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 写入器缺省参数（契约 23.6 的异步写入口径）。
const (
	// DefaultQueueSize 是入队缓冲的条数上限：满则丢弃新条目并记服务日志（绝不阻塞业务请求）。
	DefaultQueueSize = 4096
	// DefaultBatchSize 是单次落库的最大条数（一条多行 INSERT）。
	DefaultBatchSize = 100
	// DefaultFlushInterval 是「攒批」的最长等待：即使未攒满也按此周期落库，保证日志的可见延迟。
	DefaultFlushInterval = 200 * time.Millisecond
	// writeTimeout 是单次落库的超时（数据库故障时快速失败，不长期占用后台协程）。
	writeTimeout = 5 * time.Second
	// closeTimeout 是 Close 等待「冲出剩余缓冲」的上限。
	closeTimeout = 5 * time.Second
)

// Options 是写入器的构造参数。
type Options struct {
	// Store 是落库句柄；Insert 为 nil 时用它写库。
	Store *store.Store
	// Logger 为 nil 时使用 slog.Default()。
	Logger *slog.Logger
	// Insert 是落库函数（测试注入用）；nil 时走 Store.AppendOperationLogs。
	Insert func(ctx context.Context, entries []store.OperationLogInput) error
	// QueueSize / BatchSize / FlushInterval 为 0 时取缺省值。
	QueueSize     int
	BatchSize     int
	FlushInterval time.Duration
}

// Writer 是异步操作日志写入器：Enqueue 只做入队（不阻塞），后台协程攒批落库。
//
// 生命周期：NewWriter 启动后台协程；Close 停止并冲出剩余缓冲（幂等）。
// Close 之后的 Enqueue 直接丢弃（不 panic）。
type Writer struct {
	logger *slog.Logger
	insert func(ctx context.Context, entries []store.OperationLogInput) error

	queue     chan Entry
	flushReqs chan chan struct{}
	stop      chan struct{}
	done      chan struct{}

	discard   bool
	closeOnce sync.Once
}

// NewWriter 构造写入器并启动后台落库协程。
func NewWriter(opts Options) *Writer {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	queueSize := opts.QueueSize
	if queueSize <= 0 {
		queueSize = DefaultQueueSize
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > queueSize {
		batchSize = queueSize
	}
	interval := opts.FlushInterval
	if interval <= 0 {
		interval = DefaultFlushInterval
	}
	insert := opts.Insert
	if insert == nil {
		st := opts.Store
		insert = func(ctx context.Context, entries []store.OperationLogInput) error {
			if st == nil {
				return store.ErrUnavailable
			}
			return st.AppendOperationLogs(ctx, entries)
		}
	}

	writer := &Writer{
		logger:    logger,
		insert:    insert,
		queue:     make(chan Entry, queueSize),
		flushReqs: make(chan chan struct{}),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go writer.run(batchSize, interval)
	return writer
}

// Discard 返回一个丢弃全部条目的写入器（没有数据库时的兜底，不启动后台协程）。
func Discard(logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{logger: logger, discard: true}
}

// Enqueue 把一条日志放入缓冲；**永不阻塞业务请求**。
//
// 缓冲满或写入器已关闭时丢弃条目并记服务日志（契约 23.6：日志是尽力而为的留痕，
// 不能因为日志问题改变业务结果）。
func (w *Writer) Enqueue(entry Entry) {
	if w == nil || w.discard {
		return
	}
	select {
	case <-w.done:
		return
	default:
	}
	select {
	case w.queue <- entry:
	default:
		w.logger.Warn("操作日志缓冲已满，本条日志被丢弃",
			"channel", entry.Channel, "action", entry.Action, "path", entry.Path)
	}
}

// Flush 等待「调用之前已入队的条目」全部落库（清空接口在删数据前调用，保证清空后列表真的为空）。
func (w *Writer) Flush(ctx context.Context) error {
	if w == nil || w.discard {
		return nil
	}
	ack := make(chan struct{})
	select {
	case w.flushReqs <- ack:
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close 停止后台协程并冲出剩余缓冲（幂等；超过 closeTimeout 时放弃剩余条目并记服务日志）。
func (w *Writer) Close() {
	if w == nil || w.discard {
		return
	}
	w.closeOnce.Do(func() {
		close(w.stop)
		select {
		case <-w.done:
		case <-time.After(closeTimeout):
			w.logger.Warn("操作日志写入器关闭超时，剩余缓冲可能未落库")
		}
	})
}

// run 是后台落库循环：攒批到 BatchSize 或等待满 FlushInterval 即落库；收到 flush 请求先排空再落库。
func (w *Writer) run(batchSize int, interval time.Duration) {
	defer close(w.done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	batch := make([]store.OperationLogInput, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		entries := batch
		batch = make([]store.OperationLogInput, 0, batchSize)
		w.write(entries)
	}

	for {
		select {
		case entry := <-w.queue:
			batch = append(batch, toInput(normalize(entry)))
			if len(batch) >= batchSize {
				flush()
			}
		case ack := <-w.flushReqs:
			// 先把已在缓冲里的条目全部收进来，再落库。
		drain:
			for {
				select {
				case entry := <-w.queue:
					batch = append(batch, toInput(normalize(entry)))
					if len(batch) >= batchSize {
						flush()
					}
				default:
					break drain
				}
			}
			flush()
			close(ack)
		case <-ticker.C:
			flush()
		case <-w.stop:
		drainStop:
			for {
				select {
				case entry := <-w.queue:
					batch = append(batch, toInput(normalize(entry)))
					if len(batch) >= batchSize {
						flush()
					}
				default:
					break drainStop
				}
			}
			flush()
			return
		}
	}
}

// write 落库一批条目；失败只记服务日志（条目丢弃，不影响业务，也不无限重试阻塞队列）。
func (w *Writer) write(entries []store.OperationLogInput) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := w.insert(ctx, entries); err != nil {
		w.logger.Warn("操作日志写入失败（该批已丢弃）", "error", err, "count", len(entries))
	}
}

// toInput 把归一后的条目转换为落库输入。
func toInput(entry Entry) store.OperationLogInput {
	return store.OperationLogInput{
		Channel:    entry.Channel,
		ActorID:    entry.ActorID,
		ActorName:  entry.ActorName,
		Action:     entry.Action,
		Method:     entry.Method,
		Path:       entry.Path,
		HTTPStatus: entry.HTTPStatus,
		Status:     entry.Status,
		IP:         entry.IP,
		DurationMS: entry.DurationMS,
		Summary:    entry.Summary,
		CreatedAt:  entry.CreatedAt,
	}
}
