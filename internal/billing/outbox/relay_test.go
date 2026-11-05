package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// errBoom 模拟投递失败的 broker 错误。
var errBoom = errors.New("broker unreachable")

// fakeStore 内存版 Store 接口（relay 依赖接口，单测不碰 MySQL）。
type fakeStore struct {
	mu     sync.Mutex
	rows   []store.OutboxEvent // 未投行
	marked []uint64            // 已 MarkOutboxSent
	bumped map[uint64]int      // BumpOutboxAttempt 次数
	dlqed  map[uint64]string   // MarkOutboxDLQ 的 id → errMsg
}

func newFakeStore(rows ...store.OutboxEvent) *fakeStore {
	return &fakeStore{rows: rows, bumped: map[uint64]int{}, dlqed: map[uint64]string{}}
}

func (f *fakeStore) PollOutbox(ctx context.Context, limit, maxAttempts int) ([]store.OutboxEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows, nil
}

func (f *fakeStore) remove(id uint64) {
	for i, r := range f.rows {
		if r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return
		}
	}
}

func (f *fakeStore) MarkOutboxSent(ctx context.Context, id uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = append(f.marked, id)
	f.remove(id)
	return nil
}

func (f *fakeStore) BumpOutboxAttempt(ctx context.Context, id uint64, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bumped[id]++
	return nil
}

func (f *fakeStore) MarkOutboxDLQ(ctx context.Context, id uint64, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dlqed[id] = errMsg
	f.remove(id)
	return nil
}

// stubProducer 记录发布到主 topic 的 key；fail 里配置的 key 发布失败。
type stubProducer struct {
	mu   sync.Mutex
	fail map[string]error
	ok   []string
}

func (p *stubProducer) Publish(ctx context.Context, key string, value []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err, ok := p.fail[key]; ok {
		return err
	}
	p.ok = append(p.ok, key)
	return nil
}

func newRelay(st Store, main *stubProducer, dlq *stubProducer, max int) *Relay {
	return NewRelay(st, main, dlq, Config{
		Topic: store.TopicBillingEvents, DLQTopic: store.TopicBillingDLQ,
		PollInterval: time.Second, MaxAttempts: max, BatchSize: 10,
	}, logger.New("error"))
}

func evRow(id uint64, key string, attempts int) store.OutboxEvent {
	return store.OutboxEvent{
		ID: id, Topic: store.TopicBillingEvents, EventKey: key,
		Payload: `{"event_id":"` + key + `"}`, Attempts: attempts,
	}
}

type failedMarkStore struct{ *fakeStore }

func (f *failedMarkStore) MarkOutboxSent(context.Context, uint64) error {
	return errors.New("sent marker unavailable")
}
func (f *failedMarkStore) MarkOutboxDLQ(context.Context, uint64, string) error {
	return errors.New("dlq marker unavailable")
}

func TestFlushOnceReportsMarkErrorsSeparately(t *testing.T) {
	st := &failedMarkStore{newFakeStore(evRow(1, "sent", 0), evRow(2, "dlq", 4))}
	r := newRelay(st, &stubProducer{fail: map[string]error{"dlq": errBoom}}, &stubProducer{}, 5)
	stats, err := r.FlushOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if stats.Sent != 1 || stats.DLQed != 1 || fields["mark_sent_errors"] != float64(1) || fields["mark_dlq_errors"] != float64(1) {
		t.Fatalf("published counts and marker errors: %s", encoded)
	}
	if len(st.rows) != 2 {
		t.Fatalf("unmarked rows=%d", len(st.rows))
	}
}

func markErrorCounter(t *testing.T, operation string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "billing_outbox_mark_errors_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "operation" && label.GetValue() == operation {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestMarkErrorCountersSurviveSubsequentEmptyFlush(t *testing.T) {
	beforeSent, beforeDLQ := markErrorCounter(t, "sent"), markErrorCounter(t, "dlq")
	st := &failedMarkStore{newFakeStore(evRow(1, "sent", 0), evRow(2, "dlq", 4))}
	r := newRelay(st, &stubProducer{fail: map[string]error{"dlq": errBoom}}, &stubProducer{}, 5)
	if _, err := r.FlushOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := newRelay(newFakeStore(), &stubProducer{}, &stubProducer{}, 5).FlushOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := markErrorCounter(t, "sent"); got != beforeSent+1 {
		t.Fatalf("sent counter=%v want=%v", got, beforeSent+1)
	}
	if got := markErrorCounter(t, "dlq"); got != beforeDLQ+1 {
		t.Fatalf("dlq counter=%v want=%v", got, beforeDLQ+1)
	}
}

// 全部投递成功：sent 数正确、行被标 sent、主 topic 收到、DLQ 无。
func TestFlushOnceAllSuccess(t *testing.T) {
	st := newFakeStore(evRow(1, "r1:reserve", 0), evRow(2, "r2:reserve", 0))
	main, dlq := &stubProducer{fail: map[string]error{}}, &stubProducer{}
	r := newRelay(st, main, dlq, 5)

	stats, err := r.FlushOnce(context.Background())
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if stats.Polled != 2 || stats.Sent != 2 || stats.Failed != 0 || stats.DLQed != 0 {
		t.Fatalf("stats = %+v, want polled=2 sent=2", stats)
	}
	if len(st.marked) != 2 || len(main.ok) != 2 {
		t.Fatalf("marked=%d ok=%d, want 2/2", len(st.marked), len(main.ok))
	}
	if len(st.dlqed) != 0 || len(dlq.ok) != 0 {
		t.Fatalf("dlq should be empty: store=%v prod=%v", st.dlqed, dlq.ok)
	}
}

// 失败未超限：attempts+1、不标 sent、不投 DLQ，下轮可重试。
func TestFlushOnceFailBumpsAttempt(t *testing.T) {
	st := newFakeStore(evRow(1, "r1:reserve", 0))
	main := &stubProducer{fail: map[string]error{"r1:reserve": errBoom}}
	r := newRelay(st, main, &stubProducer{}, 5)

	stats, err := r.FlushOnce(context.Background())
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if stats.Failed != 1 || stats.Sent != 0 || stats.DLQed != 0 {
		t.Fatalf("stats = %+v, want failed=1", stats)
	}
	if st.bumped[1] != 1 {
		t.Fatalf("bumped[1] = %d, want 1", st.bumped[1])
	}
	if len(st.marked) != 0 || len(st.dlqed) != 0 {
		t.Fatalf("sent/dlq should be empty: marked=%v dlqed=%v", st.marked, st.dlqed)
	}
}

// 失败已超限：改投 DLQ（dlqProd 收到）+ MarkOutboxDLQ。
func TestFlushOnceFailThenDLQ(t *testing.T) {
	st := newFakeStore(evRow(9, "r9:settle", 4)) // attempts=4, max=5 → 本次第 5 次即超限
	main := &stubProducer{fail: map[string]error{"r9:settle": errBoom}}
	dlq := &stubProducer{fail: map[string]error{}}
	r := newRelay(st, main, dlq, 5)

	stats, err := r.FlushOnce(context.Background())
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if stats.Failed != 1 || stats.DLQed != 1 || stats.Sent != 0 {
		t.Fatalf("stats = %+v, want failed=1 dlqed=1", stats)
	}
	if len(dlq.ok) != 1 || dlq.ok[0] != "r9:settle" {
		t.Fatalf("dlq published = %v, want [r9:settle]", dlq.ok)
	}
	if _, ok := st.dlqed[9]; !ok {
		t.Fatalf("store MarkOutboxDLQ(9) not called: %v", st.dlqed)
	}
	if len(st.marked) != 0 {
		t.Fatalf("should not mark sent: %v", st.marked)
	}
}

// 超限但 DLQ 也投递失败（broker 宕机）→ 不标终态：attempts/sent_at 不动，行仍可重试。
func TestFlushOnceDLQPublishFailsKeepsRetrying(t *testing.T) {
	st := newFakeStore(evRow(1, "r1:settle", 4)) // attempts=4, max=5 → 本次即超限
	main := &stubProducer{fail: map[string]error{"r1:settle": errBoom}}
	dlq := &stubProducer{fail: map[string]error{"r1:settle": errBoom}} // DLQ 也失败
	r := newRelay(st, main, dlq, 5)

	stats, err := r.FlushOnce(context.Background())
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if stats.Failed != 1 || stats.DLQed != 0 {
		t.Fatalf("stats = %+v, want failed=1 dlqed=0", stats)
	}
	if len(st.dlqed) != 0 || len(st.bumped) != 0 || len(st.marked) != 0 {
		t.Fatalf("不应标终态/bump/sent：dlqed=%v bumped=%v marked=%v", st.dlqed, st.bumped, st.marked)
	}
	if len(dlq.ok) != 0 {
		t.Fatalf("dlq published = %v, want none", dlq.ok)
	}
}
