package store

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

// ledgerBatch keeps Redis as the balance authority and writes the MySQL ledger
// in one transaction per interval. A pending tenant must not be rebuilt from
// the lagging column, or the Lua debit would be refunded.
type ledgerBatch struct {
	mu        sync.Mutex
	interval  time.Duration
	items     map[string]*batchItem
	pending   map[uint64]int64
	open      map[uint64]int
	nextID    uint64
	seeded    bool
	tenants   sync.Map
	models    sync.Map
	active    sync.Map
	sink      LedgerSink
	unsunk    []*BillingEvent
	logReady  chan struct{}
	flushBusy atomic.Bool
	viaKafka  atomic.Bool
}

// LedgerSink 把每笔请求的日志送到 Kafka。请求路径只把事件放进内存，后台再送。
// 送失败时事件留在内存，下一轮再送；Kafka 仍不可用才写入 outbox。余额仍按批次写 MySQL。
type LedgerSink interface {
	PublishLedger(ctx context.Context, events []*BillingEvent) error
}

type batchItem struct {
	row            *Bill
	pre            int64
	reserveWritten bool
	settle         *Bill
	settleDelta    int64
	settleWritten  bool
	flushing       bool
	undo           bool
	queued         bool
	queuedAt       time.Time
	reverseSent    bool
}

func (s *Store) EnableLedgerBatch(interval time.Duration) {
	if interval <= 0 || s.batch != nil {
		return
	}
	s.batch = &ledgerBatch{
		interval: interval,
		items:    map[string]*batchItem{},
		pending:  map[uint64]int64{},
		open:     map[uint64]int{},
		logReady: make(chan struct{}, 1),
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.FlushLedgerBatch(context.Background()); err != nil {
				log.Printf("ledger batch flush: %v", err)
			}
		}
	}()
	go func() {
		for {
			if s.batch.viaKafka.Load() {
				time.Sleep(200 * time.Millisecond)
				if err := s.batch.pumpQueue(context.Background(), s.db, false); err != nil {
					log.Printf("ledger queue publish: %v", err)
				}
				continue
			}
			<-s.batch.logReady
			if err := s.batch.drainUnpublished(context.Background(), s.db); err != nil {
				log.Printf("ledger event publish: %v", err)
			}
		}
	}()
}

// AttachLedgerSink 在批量刷账开启后调用。Kafka 没配时不要调用，成功路径仍把事件和账单写进同一事务。
func (s *Store) AttachLedgerSink(sink LedgerSink) {
	if s.batch == nil || sink == nil {
		return
	}
	s.batch.mu.Lock()
	s.batch.sink = sink
	s.batch.mu.Unlock()
}

// EnableLedgerQueue 让 Kafka 承接账单。Redis 扣款成功后，事件进入队列，由消费者写 MySQL。
// 账单行出现之前，这个租户一直算「账还没跟上」，不能拿旧列把 Redis 加回去。
func (s *Store) EnableLedgerQueue() {
	if s.batch == nil {
		return
	}
	s.batch.viaKafka.Store(true)
}

func (s *Store) FlushLedgerBatch(ctx context.Context) error {
	if s.batch == nil {
		return nil
	}
	db := s.db
	if s.cash != nil && s.cash.root != nil {
		db = s.cash.root
	}
	return s.batch.flush(ctx, db)
}

func (b *ledgerBatch) tenantLock(tenantID uint64) *sync.Mutex {
	if v, ok := b.tenants.Load(tenantID); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := b.tenants.LoadOrStore(tenantID, mu)
	return actual.(*sync.Mutex)
}

func (b *ledgerBatch) cachedModel(modelID string) (*Model, bool) {
	v, ok := b.models.Load(modelID)
	if !ok {
		return nil, false
	}
	cp := *(v.(*Model))
	return &cp, true
}

func (b *ledgerBatch) rememberModel(modelID string, model *Model) {
	cp := *model
	b.models.Store(modelID, &cp)
}

func (b *ledgerBatch) forgetModels() {
	b.models.Range(func(key, _ any) bool {
		b.models.Delete(key)
		return true
	})
}

func (s *Store) forgetCachedModels() {
	if s.batch != nil {
		s.batch.forgetModels()
	}
}

func (b *ledgerBatch) tenantActive(tenantID uint64) bool {
	_, ok := b.active.Load(tenantID)
	return ok
}

func (b *ledgerBatch) rememberTenant(tenantID uint64) {
	b.active.Store(tenantID, struct{}{})
}

func (b *ledgerBatch) behind(tenantID uint64) bool {
	// While a flush transaction is writing the column, skip the match.
	// A tenant finished in an earlier chunk would otherwise read MySQL
	// in the middle of the remaining chunks and sit on that row lock.
	if b.flushBusy.Load() {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.open[tenantID] > 0
}

func (b *ledgerBatch) alloc(db *gorm.DB) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.allocLocked(db)
}

func (s *Store) assignBillID(db *gorm.DB, row *Bill) error {
	if s.batch == nil || row == nil || row.ID != 0 {
		return nil
	}
	id, err := s.batch.alloc(db)
	if err != nil {
		return err
	}
	row.ID = id
	return nil
}

func (b *ledgerBatch) copyReserve(requestID string) *Bill {
	b.mu.Lock()
	defer b.mu.Unlock()
	item := b.items[requestID]
	if item == nil {
		return nil
	}
	row := *item.row
	return &row
}

func (b *ledgerBatch) tracks(requestID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.items[requestID]
	return ok
}

func (b *ledgerBatch) allocLocked(db *gorm.DB) (uint64, error) {
	if !b.seeded {
		var maxID int64
		if err := db.Model(&Bill{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			return 0, err
		}
		if maxID > 0 {
			b.nextID = uint64(maxID)
		}
		b.seeded = true
	}
	b.nextID++
	return b.nextID, nil
}

func (b *ledgerBatch) enqueueReserve(db *gorm.DB, row *Bill, pre int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing := b.items[row.RequestID]; existing != nil {
		*row = *existing.row
		return nil
	}
	id, err := b.allocLocked(db)
	if err != nil {
		return err
	}
	row.ID = id
	stored := *row
	b.items[row.RequestID] = &batchItem{row: &stored, pre: pre}
	b.pending[row.TenantID] -= pre
	b.open[row.TenantID]++
	b.noteRequestLogLocked(&stored, pre, PhaseReserve)
	return nil
}

// queueSettle records a settlement that Redis has already applied.
// It returns false when the reserve was already written and SQL must persist it.
func (b *ledgerBatch) queueSettle(row *Bill, settle *Bill, delta int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	item := b.items[row.RequestID]
	if item == nil || item.settleWritten || item.undo {
		return false
	}
	if item.settle != nil {
		return true
	}
	return b.storeSettleLocked(item, row, settle, delta)
}

func (b *ledgerBatch) storeSettleLocked(item *batchItem, row *Bill, settle *Bill, delta int64) bool {
	now := time.Now()
	item.row.Status = StatusSettled
	item.row.PromptTokens = row.PromptTokens
	item.row.CompletionTokens = row.CompletionTokens
	item.row.UsageReportedAt = &now
	item.row.ProviderID = row.ProviderID
	b.nextID++
	settle.ID = b.nextID
	stored := *settle
	item.settle = &stored
	item.settleDelta = delta
	b.pending[row.TenantID] -= delta
	b.noteRequestLogLocked(&stored, delta, PhaseSettle)
	return true
}

func (b *ledgerBatch) settledView(requestID string) (int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	item := b.items[requestID]
	if item == nil || item.settle == nil || item.settle.DeltaQuota == nil {
		return 0, false
	}
	return *item.settle.DeltaQuota, true
}

const (
	batchReverseSQL = iota
	batchReverseSettled
	batchReverseDrop
	batchReverseAlready
	batchReverseDefer
)

func (b *ledgerBatch) takeReverse(requestID string) (uint64, int64, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	item := b.items[requestID]
	if b.viaKafka.Load() && item != nil {
		if item.settle != nil || item.row.Status == StatusSettled {
			return 0, 0, batchReverseSettled
		}
		if item.undo {
			return item.row.TenantID, item.pre, batchReverseAlready
		}
		item.undo = true
		return item.row.TenantID, item.pre, batchReverseDefer
	}
	if item == nil || (item.reserveWritten && !item.flushing && !item.undo) {
		return 0, 0, batchReverseSQL
	}
	if item.undo {
		return item.row.TenantID, item.pre, batchReverseAlready
	}
	if item.settle != nil || item.row.Status == StatusSettled {
		return 0, 0, batchReverseSettled
	}
	// Already handed to Kafka, or the publish is in flight. Keep the item so a
	// reverse event follows the debit. Redis is refunded once by the caller.
	if item.queued || item.flushing {
		item.undo = true
		return item.row.TenantID, item.pre, batchReverseDrop
	}
	tenantID := item.row.TenantID
	pre := item.pre
	b.noteRequestLogLocked(item.row, -pre, PhaseReverse)
	delete(b.items, requestID)
	b.pending[tenantID] += pre
	if b.pending[tenantID] == 0 {
		delete(b.pending, tenantID)
	}
	b.open[tenantID]--
	if b.open[tenantID] == 0 {
		delete(b.open, tenantID)
	}
	return tenantID, pre, batchReverseDrop
}

type batchSnap struct {
	requestID    string
	tenantID     uint64
	reserve      Bill
	pre          int64
	settle       *Bill
	settleDelta  int64
	writeReserve bool
	writeSettle  bool
}

func (b *ledgerBatch) flush(ctx context.Context, db *gorm.DB) error {
	if b.viaKafka.Load() {
		return b.pumpQueue(ctx, db, true)
	}
	b.flushBusy.Store(true)
	defer b.flushBusy.Store(false)
	for {
		if err := b.compensateUndos(ctx, db); err != nil {
			return err
		}
		work := b.takeChunk(256, false)
		if len(work) == 0 {
			return nil
		}
		b.mu.Lock()
		sink := b.sink
		b.mu.Unlock()
		columnDelta := map[uint64]int64{}
		var events []*BillingEvent
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			reserves := make([]Bill, 0, len(work))
			settles := make([]Bill, 0, len(work))
			events = make([]*BillingEvent, 0, len(work)*2)
			batched := map[uint64]int64{}
			for _, one := range work {
				if !one.writeReserve {
					rows, err := markReserveSettled(tx, one.requestID, &attachedUsage{
						prompt:     deref64(one.reserve.PromptTokens),
						completion: deref64(one.reserve.CompletionTokens),
						provider:   one.reserve.ProviderID,
					})
					if err != nil {
						return err
					}
					if rows == 1 {
						if err := addLedgerBalance(tx, one.tenantID, -one.settleDelta); err != nil {
							return err
						}
						columnDelta[one.tenantID] += -one.settleDelta
					}
					settle := *one.settle
					if err := tx.Create(&settle).Error; err != nil && !isDupKey(err) {
						return err
					}
					events = append(events, newBillingEvent(&settle, one.settleDelta, PhaseSettle))
					continue
				}
				row := one.reserve
				if one.writeSettle {
					row.Status = StatusSettled
				}
				reserves = append(reserves, row)
				delta := -one.pre
				if one.writeSettle {
					delta -= one.settleDelta
					settles = append(settles, *one.settle)
				}
				batched[one.tenantID] += delta
				columnDelta[one.tenantID] += delta
			}
			if len(reserves) > 0 {
				if err := tx.CreateInBatches(&reserves, 100).Error; err != nil {
					return err
				}
			}
			if len(settles) > 0 {
				if err := tx.CreateInBatches(&settles, 100).Error; err != nil {
					return err
				}
			}
			for i := range reserves {
				events = append(events, newBillingEvent(&reserves[i], reserves[i].PreQuota, PhaseReserve))
			}
			for i := range settles {
				amount := int64(0)
				if settles[i].DeltaQuota != nil {
					amount = *settles[i].DeltaQuota
				}
				events = append(events, newBillingEvent(&settles[i], amount, PhaseSettle))
			}
			for tenantID, delta := range batched {
				if err := addLedgerBalance(tx, tenantID, delta); err != nil {
					return err
				}
			}
			if sink != nil {
				return nil
			}
			return insertOutboxBatch(tx, events)
		})
		if err != nil {
			b.finishChunk(work, columnDelta, err)
			return err
		}
		b.finishChunk(work, columnDelta, nil)
	}
}

// noteRequestLogLocked 把这一笔日志放进待发送队列。调用方已持有 b.mu。Kafka 队列模式自己组事件，这里不重复放。
func (b *ledgerBatch) noteRequestLogLocked(row *Bill, amount int64, phase string) {
	if b.sink == nil || b.viaKafka.Load() {
		return
	}
	b.unsunk = append(b.unsunk, newBillingEvent(row, amount, phase))
	b.wakeLogLocked()
}

func (b *ledgerBatch) wakeLogLocked() {
	if b.logReady == nil {
		return
	}
	select {
	case b.logReady <- struct{}{}:
	default:
	}
}

// drainUnpublished 把已经入队的日志立刻按条送出。Producer 的 BatchSize 是 1，
// 所以一次调用里的每一条仍是独立的 Kafka 请求，并等待 RequireAll 确认。
// 确认失败时再逐条重试，仍失败的那一条才写入 outbox。
func (b *ledgerBatch) drainUnpublished(ctx context.Context, db *gorm.DB) error {
	for {
		b.mu.Lock()
		if len(b.unsunk) == 0 {
			b.mu.Unlock()
			return nil
		}
		pending := b.unsunk
		b.unsunk = nil
		sink := b.sink
		b.mu.Unlock()
		if sink != nil && sink.PublishLedger(ctx, pending) == nil {
			continue
		}
		for _, ev := range pending {
			if sink != nil && sink.PublishLedger(ctx, []*BillingEvent{ev}) == nil {
				continue
			}
			err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return insertOutboxBatch(tx, []*BillingEvent{ev})
			})
			if err != nil && !isDupKey(err) {
				b.mu.Lock()
				b.unsunk = append([]*BillingEvent{ev}, b.unsunk...)
				b.mu.Unlock()
				return err
			}
		}
	}
}

func (b *ledgerBatch) takeChunk(limit int, waitForSettle bool) []batchSnap {
	b.mu.Lock()
	defer b.mu.Unlock()
	work := make([]batchSnap, 0, limit)
	for id, item := range b.items {
		if len(work) == limit {
			break
		}
		if item.flushing || item.undo || item.queued || (item.reserveWritten && (item.settle == nil || item.settleWritten)) {
			continue
		}
		if waitForSettle && item.settle == nil && time.Since(item.row.CreatedAt) < time.Second {
			continue
		}
		one := batchSnap{requestID: id, tenantID: item.row.TenantID, reserve: *item.row, pre: item.pre, writeReserve: !item.reserveWritten}
		if item.settle != nil && !item.settleWritten {
			copied := *item.settle
			one.settle = &copied
			one.settleDelta = item.settleDelta
			one.writeSettle = true
		}
		if !one.writeReserve && !one.writeSettle {
			continue
		}
		item.flushing = true
		work = append(work, one)
	}
	return work
}

func (b *ledgerBatch) finishChunk(work []batchSnap, columnDelta map[uint64]int64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, one := range work {
		item := b.items[one.requestID]
		if item == nil {
			continue
		}
		item.flushing = false
		if err != nil {
			if item.undo {
				b.pending[item.row.TenantID] += item.pre
				b.forget(one.requestID, item.row.TenantID)
			}
			continue
		}
		item.reserveWritten = true
		if one.writeSettle {
			item.settleWritten = true
		}
		if item.undo {
			continue
		}
		if item.reserveWritten && item.settle != nil && item.settleWritten {
			b.forget(one.requestID, item.row.TenantID)
		}
	}
	if err != nil {
		return
	}
	for tenantID, delta := range columnDelta {
		b.pending[tenantID] -= delta
		if b.pending[tenantID] == 0 {
			delete(b.pending, tenantID)
		}
	}
}

func (b *ledgerBatch) forget(requestID string, tenantID uint64) {
	delete(b.items, requestID)
	b.open[tenantID]--
	if b.open[tenantID] <= 0 {
		delete(b.open, tenantID)
	}
}

func (b *ledgerBatch) compensateUndos(ctx context.Context, db *gorm.DB) error {
	b.mu.Lock()
	var ready []batchSnap
	for id, item := range b.items {
		if item.undo && item.reserveWritten && !item.flushing {
			ready = append(ready, batchSnap{requestID: id, tenantID: item.row.TenantID, reserve: *item.row, pre: item.pre})
		}
	}
	b.mu.Unlock()
	for _, one := range ready {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&Bill{}).
				Where("request_id = ? AND phase = ? AND status = ?", one.requestID, PhaseReserve, StatusPending).
				Update("status", StatusReversed)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				if err := addLedgerBalance(tx, one.tenantID, one.pre); err != nil {
					return err
				}
			}
			row := one.reserve
			return insertOutbox(tx, newBillingEvent(&row, -one.pre, PhaseReverse))
		})
		if err != nil {
			return err
		}
		b.mu.Lock()
		if item := b.items[one.requestID]; item != nil && item.undo && item.reserveWritten {
			b.forget(one.requestID, item.row.TenantID)
		}
		b.mu.Unlock()
	}
	return nil
}

func deref64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
