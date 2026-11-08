package store

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"
)

// pumpQueue 把已经在 Redis 扣成的账发到 Kafka。includePending 为真时连尚未结算的预扣一起发。
// 消费者写出账单行之前，这些账一直留在内存里，避免用旧的 MySQL 余额回写 Redis。
func (b *ledgerBatch) pumpQueue(ctx context.Context, db *gorm.DB, includePending bool) error {
	if !b.flushBusy.CompareAndSwap(false, true) {
		return nil
	}
	defer b.flushBusy.Store(false)
	if !b.viaKafka.Load() {
		if err := b.compensateUndos(ctx, db); err != nil {
			return err
		}
	}
	if err := b.publishReverses(ctx, db); err != nil {
		return err
	}
	for {
		work := b.takeChunk(256, !includePending)
		if len(work) == 0 {
			break
		}
		events := make([]*BillingEvent, 0, len(work)*2)
		for _, one := range work {
			if one.writeReserve {
				events = append(events, queueEvent(&one.reserve, one.pre, -one.pre, PhaseReserve))
			}
			if one.writeSettle && one.settle != nil {
				events = append(events, queueEvent(one.settle, one.settleDelta, -one.settleDelta, PhaseSettle))
			}
		}
		b.mu.Lock()
		sink := b.sink
		b.mu.Unlock()
		if err := publishOrOutbox(ctx, db, sink, events); err != nil {
			b.finishChunk(work, nil, err)
			return err
		}
		b.markQueued(work)
	}
	return b.ackQueued(ctx, db)
}

func queueEvent(row *Bill, amount, ledgerDelta int64, phase string) *BillingEvent {
	ev := newBillingEvent(row, amount, phase)
	ev.ApplyLedger = true
	ev.APIKeyID = row.APIKeyID
	ev.BillStatus = row.Status
	ev.PreQuota = row.PreQuota
	ev.ActualQuota = row.ActualQuota
	ev.DeltaQuota = row.DeltaQuota
	ev.ProviderID = row.ProviderID
	ev.InPriceCent = row.InPriceCent
	ev.OutPriceCent = row.OutPriceCent
	ev.LedgerDelta = ledgerDelta
	ev.UsageReported = row.UsageReportedAt != nil
	ev.BillID = row.ID
	return ev
}

func publishOrOutbox(ctx context.Context, db *gorm.DB, sink LedgerSink, events []*BillingEvent) error {
	if len(events) == 0 {
		return nil
	}
	if sink != nil {
		if err := sink.PublishLedger(ctx, events); err == nil {
			return nil
		}
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return insertOutboxBatch(tx, events)
	})
	if err != nil && !isDupKey(err) {
		return err
	}
	return nil
}

func (b *ledgerBatch) publishReverses(ctx context.Context, db *gorm.DB) error {
	b.mu.Lock()
	events := make([]*BillingEvent, 0)
	ids := make([]string, 0)
	for id, item := range b.items {
		if !item.undo || item.reverseSent || item.settle != nil {
			continue
		}
		row := *item.row
		if !item.queued && !item.reserveWritten {
			reserve := row
			reserve.Status = StatusPending
			events = append(events, queueEvent(&reserve, item.pre, -item.pre, PhaseReserve))
		}
		row.Status = StatusReversed
		events = append(events, queueEvent(&row, -item.pre, item.pre, PhaseReverse))
		ids = append(ids, id)
	}
	sink := b.sink
	b.mu.Unlock()
	if len(events) == 0 {
		return nil
	}
	if err := publishOrOutbox(ctx, db, sink, events); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range ids {
		if item := b.items[id]; item != nil {
			item.reverseSent = true
			item.queued = true
			if item.queuedAt.IsZero() {
				item.queuedAt = time.Now()
			}
		}
	}
	return nil
}

func (b *ledgerBatch) markQueued(work []batchSnap) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for _, one := range work {
		item := b.items[one.requestID]
		if item == nil {
			continue
		}
		item.flushing = false
		item.queued = true
		item.queuedAt = now
	}
}

func (b *ledgerBatch) ackQueued(ctx context.Context, db *gorm.DB) error {
	b.mu.Lock()
	ids := make([]string, 0)
	for id, item := range b.items {
		if item.queued {
			ids = append(ids, id)
		}
	}
	b.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	found := map[string]map[string]string{}
	for start := 0; start < len(ids); start += 400 {
		end := start + 400
		if end > len(ids) {
			end = len(ids)
		}
		var rows []Bill
		if err := db.WithContext(ctx).Model(&Bill{}).Select("request_id", "phase", "status").
			Where("request_id IN ?", ids[start:end]).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			phases := found[row.RequestID]
			if phases == nil {
				phases = map[string]string{}
				found[row.RequestID] = phases
			}
			phases[row.Phase] = row.Status
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range ids {
		item := b.items[id]
		if item == nil || !item.queued {
			continue
		}
		phases := found[id]
		var done bool
		if item.undo {
			done = phases[PhaseReserve] == StatusReversed
		} else {
			_, done = phases[PhaseReserve]
			if item.settle != nil {
				_, settle := phases[PhaseSettle]
				done = done && settle
			}
		}
		if !done {
			if time.Since(item.queuedAt) > 5*time.Second {
				if item.undo {
					item.reverseSent = false
				} else {
					item.queued = false
				}
			}
			continue
		}
		tenantID := item.row.TenantID
		b.pending[tenantID] += item.pre
		if item.settle != nil {
			b.pending[tenantID] += item.settleDelta
		}
		if b.pending[tenantID] == 0 {
			delete(b.pending, tenantID)
		}
		b.forget(id, tenantID)
	}
	return nil
}

// ApplyLedgerEvents 把队列里的账写成 MySQL。重复消息撞上唯一键后不再改余额。
func (s *Store) ApplyLedgerEvents(ctx context.Context, events []*BillingEvent) error {
	applicable := make([]*BillingEvent, 0, len(events))
	for _, ev := range events {
		if ev == nil || !ev.ApplyLedger {
			continue
		}
		if ev.Phase != PhaseReserve && ev.Phase != PhaseSettle && ev.Phase != PhaseReverse {
			continue
		}
		applicable = append(applicable, ev)
	}
	if len(applicable) == 0 {
		return nil
	}
	sort.SliceStable(applicable, func(i, j int) bool {
		return phaseRank(applicable[i].Phase) < phaseRank(applicable[j].Phase)
	})
	refunds := make([]*BillingEvent, 0)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, ev := range applicable {
			if ev.Phase == PhaseReverse {
				if err := applyReverse(tx, ev); err != nil {
					return err
				}
				refunds = append(refunds, ev)
				continue
			}
			bill := billFromEvent(ev)
			if ev.Phase == PhaseReserve {
				var settles int64
				if err := tx.Model(&Bill{}).
					Where("request_id = ? AND phase = ?", ev.RequestID, PhaseSettle).
					Count(&settles).Error; err != nil {
					return err
				}
				if settles > 0 {
					bill.Status = StatusSettled
				}
			}
			inserted := true
			if err := tx.Create(&bill).Error; err != nil {
				if !isDupKey(err) {
					return err
				}
				inserted = false
			}
			if ev.Phase == PhaseSettle {
				if _, err := markReserveSettled(tx, ev.RequestID, &attachedUsage{
					prompt:     deref64(ev.PromptTokens),
					completion: deref64(ev.CompletionTokens),
					provider:   ev.ProviderID,
				}); err != nil {
					return err
				}
			}
			if !inserted {
				continue
			}
			if err := addLedgerBalance(tx, ev.TenantID, ev.LedgerDelta); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, ev := range refunds {
		amount := ev.LedgerDelta
		if amount == 0 {
			amount = ev.PreQuota
		}
		if amount == 0 {
			continue
		}
		if _, _, err := s.applyDelta(ctx, ev.TenantID, ev.RequestID, -amount); err != nil {
			return err
		}
	}
	return nil
}

func phaseRank(phase string) int {
	switch phase {
	case PhaseReserve:
		return 0
	case PhaseSettle:
		return 1
	case PhaseReverse:
		return 2
	default:
		return 3
	}
}

func applyReverse(tx *gorm.DB, ev *BillingEvent) error {
	res := tx.Model(&Bill{}).
		Where("request_id = ? AND phase = ? AND status = ?", ev.RequestID, PhaseReserve, StatusPending).
		Update("status", StatusReversed)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return addLedgerBalance(tx, ev.TenantID, ev.LedgerDelta)
	}
	row := billFromEvent(ev)
	row.Phase = PhaseReserve
	row.Status = StatusReversed
	if row.PreQuota == 0 {
		row.PreQuota = ev.LedgerDelta
	}
	if err := tx.Create(&row).Error; err != nil && !isDupKey(err) {
		return err
	}
	return nil
}

func billFromEvent(ev *BillingEvent) Bill {
	created := ev.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	bill := Bill{
		ID:               ev.BillID,
		RequestID:        ev.RequestID,
		Phase:            ev.Phase,
		Status:           ev.BillStatus,
		TenantID:         ev.TenantID,
		APIKeyID:         ev.APIKeyID,
		ProviderID:       ev.ProviderID,
		Model:            ev.Model,
		Funding:          ev.Funding,
		PreQuota:         ev.PreQuota,
		ActualQuota:      ev.ActualQuota,
		DeltaQuota:       ev.DeltaQuota,
		PromptTokens:     ev.PromptTokens,
		CompletionTokens: ev.CompletionTokens,
		InPriceCent:      ev.InPriceCent,
		OutPriceCent:     ev.OutPriceCent,
		CreatedAt:        created,
	}
	if bill.Status == "" {
		bill.Status = StatusPending
	}
	if bill.Funding == "" {
		bill.Funding = FundingBalance
	}
	if ev.UsageReported {
		now := time.Now()
		bill.UsageReportedAt = &now
	}
	return bill
}
