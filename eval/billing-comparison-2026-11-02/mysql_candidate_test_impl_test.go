package comparison

// Experimental cash-only candidate. It is not wired to the application.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type candidateAccount struct {
	ID      uint64 `gorm:"primaryKey"`
	Initial int64
	Balance int64
}
type candidate struct {
	db     *gorm.DB
	fail   bool
	intent *candidateIntent
	strict bool
}

// Test-only; not a production authorization protocol.
type candidateIntent struct {
	ownerHash string
	lease     time.Duration
}

func isolatedDB(t *testing.T) (*gorm.DB, *redis.Client) {
	t.Helper()
	dsn, addr := os.Getenv("COMPARISON_MYSQL_DSN"), os.Getenv("COMPARISON_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("disposable services required")
	}
	db, e := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	sqlDB.SetMaxOpenConns(40)
	sqlDB.SetMaxIdleConns(10)
	t.Cleanup(func() { sqlDB.Close() })
	r := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { r.Close() })
	return db, r
}
func (c candidate) migrate() error {
	return c.db.AutoMigrate(&candidateAccount{}, &store.Bill{}, &store.OutboxEvent{}, &store.Model{}, &store.Tenant{}, &store.Topup{}, &store.Provider{})
}
func (c candidate) seed(id uint64, amount int64) error {
	return c.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&store.Tenant{ID: id, Name: fmt.Sprintf("candidate-%d", id), InitialQuota: amount}).Error; err != nil {
			return err
		}
		return tx.Create(&candidateAccount{ID: id, Initial: amount, Balance: amount}).Error
	})
}
func (c candidate) balance(ctx context.Context, id uint64) (int64, error) {
	var a candidateAccount
	e := c.db.WithContext(ctx).First(&a, id).Error
	return a.Balance, e
}
func fp(req *billingv1.ReserveRequest) string {
	b, _ := json.Marshal([]any{"reserve-v1", req.TenantId, req.ApiKeyId, req.ProviderId, req.Model, req.EstPromptTokens, req.MaxCompletionTokens})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func candidateEvent(tx *gorm.DB, row *store.Bill, phase string, amount int64) error {
	ev := store.BillingEvent{EventID: row.RequestID + ":" + phase, TenantID: row.TenantID, RequestID: row.RequestID, Phase: phase, AmountCents: amount, Model: row.Model, Funding: row.Funding, PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens, BillID: row.ID, CreatedAt: time.Now()}
	data, e := json.Marshal(ev)
	if e != nil {
		return e
	}
	return tx.Create(&store.OutboxEvent{Topic: store.TopicBillingEvents, EventKey: ev.EventID, Payload: string(data), CreatedAt: time.Now()}).Error
}
func (c candidate) reserve(ctx context.Context, r *billingv1.ReserveRequest) (out *billingv1.ReserveResponse, err error) {
	if c.strict && c.intent == nil {
		return nil, status.Error(codes.FailedPrecondition, "execution intent required")
	}
	if c.intent != nil {
		decoded, err := hex.DecodeString(c.intent.ownerHash)
		if err != nil || len(decoded) != 32 || c.intent.lease < time.Microsecond || c.intent.lease > time.Hour {
			return nil, status.Error(codes.InvalidArgument, "invalid execution intent")
		}
	}
	f := fp(r)
	err = c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a candidateAccount
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, uint64(r.TenantId)).Error; e != nil {
			return e
		}
		var prior store.Bill
		e := tx.Where("request_id=? AND phase=?", r.RequestId, store.PhaseReserve).First(&prior).Error
		if e == nil {
			if prior.ReserveFingerprint == nil || *prior.ReserveFingerprint != f {
				return status.Error(codes.FailedPrecondition, "conflict")
			}
			code := billingv1.ReserveCode_RESERVE_OK
			if c.intent != nil {
				code = billingv1.ReserveCode_RESERVE_DUPLICATE
			}
			out = &billingv1.ReserveResponse{Code: code, BillId: int64(prior.ID), BalanceAfter: a.Balance}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var model store.Model
		if e = tx.Where("model_id=? AND status=0", r.Model).First(&model).Error; e != nil {
			return e
		}
		max := r.MaxCompletionTokens
		if max < 0 {
			max = 0
		}
		pre := quota.ReserveCostCent(r.EstPromptTokens, max, model.InputPriceCent, model.OutputPriceCent)
		if pre < 0 {
			pre = 0
		}
		if pre == 0 && (model.InputPriceCent > 0 || model.OutputPriceCent > 0) {
			pre = 1
		}
		if a.Balance < pre {
			out = &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_INSUFFICIENT, BalanceAfter: a.Balance}
			return nil
		}
		if e = tx.Model(&a).Update("balance", gorm.Expr("balance - ?", pre)).Error; e != nil {
			return e
		}
		row := store.Bill{RequestID: r.RequestId, ReserveFingerprint: &f, Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: uint64(r.TenantId), APIKeyID: uint64(r.ApiKeyId), Model: r.Model, Funding: store.FundingBalance, PreQuota: pre, InPriceCent: &model.InputPriceCent, OutPriceCent: &model.OutputPriceCent, CreatedAt: time.Now()}
		if r.ProviderId > 0 {
			id := uint64(r.ProviderId)
			row.ProviderID = &id
		}
		if e = tx.Create(&row).Error; e != nil {
			return e
		}
		if c.intent != nil {
			if e = tx.Model(&row).Updates(map[string]any{"execution_state": "reserved", "execution_owner_hash": c.intent.ownerHash, "execution_lease_until": gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND)", c.intent.lease.Microseconds())}).Error; e != nil {
				return e
			}
		}
		if e = candidateEvent(tx, &row, store.PhaseReserve, pre); e != nil {
			return e
		}
		if c.fail {
			return errors.New("injected rollback")
		}
		out = &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(row.ID), BalanceAfter: a.Balance - pre}
		return nil
	})
	var duplicate *mysqldriver.MySQLError
	if err != nil && errors.As(err, &duplicate) && duplicate.Number == 1062 {
		var prior store.Bill
		if e := c.db.WithContext(ctx).Where("request_id=? AND phase=?", r.RequestId, store.PhaseReserve).First(&prior).Error; e == nil {
			if prior.ReserveFingerprint == nil || *prior.ReserveFingerprint != f {
				return nil, status.Error(codes.FailedPrecondition, "conflict")
			}
			balance, e := c.balance(ctx, uint64(r.TenantId))
			if e != nil {
				return nil, e
			}
			code := billingv1.ReserveCode_RESERVE_OK
			if c.intent != nil {
				code = billingv1.ReserveCode_RESERVE_DUPLICATE
			}
			return &billingv1.ReserveResponse{Code: code, BillId: int64(prior.ID), BalanceAfter: balance}, nil
		}
	}
	return
}
func (c candidate) usage(ctx context.Context, r *billingv1.ReportUsageRequest) error {
	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row store.Bill
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=? AND phase=?", r.RequestId, store.PhaseReserve).First(&row).Error; e != nil {
			return e
		}
		if r.PromptTokens < 0 || r.CompletionTokens < 0 || r.ProviderId < 0 {
			return status.Error(codes.InvalidArgument, "negative usage")
		}
		if row.UsageReportedAt != nil {
			if row.PromptTokens != nil && row.CompletionTokens != nil && *row.PromptTokens == r.PromptTokens && *row.CompletionTokens == r.CompletionTokens && (r.ProviderId == 0 || (row.ProviderID != nil && *row.ProviderID == uint64(r.ProviderId))) {
				return nil
			}
			return status.Error(codes.FailedPrecondition, "usage conflict")
		}
		if row.Status != store.StatusPending {
			return status.Error(codes.FailedPrecondition, "terminal")
		}
		if c.intent != nil || c.strict {
			var state string
			if e := tx.Raw("SELECT execution_state FROM bills WHERE id=?", row.ID).Scan(&state).Error; e != nil {
				return e
			}
			if state != "forwarding" {
				return status.Error(codes.FailedPrecondition, "forwarding required")
			}
		}
		row.PromptTokens = &r.PromptTokens
		row.CompletionTokens = &r.CompletionTokens
		now := time.Now()
		row.UsageReportedAt = &now
		if r.ProviderId > 0 {
			var p store.Provider
			if e := tx.First(&p, uint64(r.ProviderId)).Error; e != nil {
				return e
			}
			id := uint64(r.ProviderId)
			row.ProviderID = &id
		}
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		return candidateEvent(tx, &row, store.PhaseUsage, 0)
	})
}
func (c candidate) terminal(ctx context.Context, id string, settle bool) (bool, error) {
	var initial store.Bill
	if e := c.db.WithContext(ctx).Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&initial).Error; e != nil {
		return false, e
	}
	ok := false
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a candidateAccount
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, initial.TenantID).Error; e != nil {
			return e
		}
		var row store.Bill
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; e != nil {
			return e
		}
		if row.Status != store.StatusPending {
			ok = (settle && row.Status == store.StatusSettled) || (!settle && row.Status == store.StatusReversed)
			return nil
		}
		if (c.intent != nil || c.strict) && !settle {
			var state string
			if e := tx.Raw("SELECT execution_state FROM bills WHERE id=?", row.ID).Scan(&state).Error; e != nil {
				return e
			}
			if state != "reversing" || row.UsageReportedAt != nil {
				return status.Error(codes.FailedPrecondition, "reversing without usage required")
			}
		}
		delta := -row.PreQuota
		phase := store.PhaseReverse
		if settle {
			if row.UsageReportedAt == nil || row.PromptTokens == nil || row.CompletionTokens == nil {
				return status.Error(codes.FailedPrecondition, "usage not reported")
			}
			actual := quota.CostCent(*row.PromptTokens, *row.CompletionTokens, *row.InPriceCent, *row.OutPriceCent)
			delta = actual - row.PreQuota
			if delta > a.Balance {
				delta = a.Balance
			}
			collected := row.PreQuota + delta
			short := actual - collected
			sr := row
			sr.ID = 0
			sr.Phase = store.PhaseSettle
			sr.Status = store.StatusSettled
			sr.ActualQuota = &collected
			sr.DeltaQuota = &delta
			sr.ReserveFingerprint = nil
			if short > 0 {
				sr.ErrorCode = fmt.Sprintf("shortfall:%d", short)
			}
			if e := tx.Create(&sr).Error; e != nil {
				return e
			}
			phase = store.PhaseSettle
			row.Status = store.StatusSettled
			if e := candidateEvent(tx, &sr, phase, delta); e != nil {
				return e
			}
		} else {
			row.Status = store.StatusReversed
			if e := candidateEvent(tx, &row, phase, delta); e != nil {
				return e
			}
		}
		if e := tx.Model(&a).Update("balance", gorm.Expr("balance - ?", delta)).Error; e != nil {
			return e
		}
		updates := map[string]any{"status": row.Status}
		if (c.intent != nil || c.strict) && !settle {
			updates["execution_state"] = "reversed"
		}
		if e := tx.Model(&row).Updates(updates).Error; e != nil {
			return e
		}
		if c.fail {
			return errors.New("injected rollback")
		}
		ok = true
		return nil
	})
	return ok, err
}
func (c candidate) settle(ctx context.Context, id string) (bool, error) {
	return c.terminal(ctx, id, true)
}
func (c candidate) reverse(ctx context.Context, id string) (bool, error) {
	return c.terminal(ctx, id, false)
}
func (c candidate) audit(ctx context.Context) error {
	var accounts []candidateAccount
	if e := c.db.WithContext(ctx).Find(&accounts).Error; e != nil {
		return e
	}
	for _, a := range accounts {
		balance, e := store.LedgerBalance(ctx, c.db, a.ID)
		if e != nil {
			return e
		}
		if balance != a.Balance {
			return fmt.Errorf("account %d balance %d ledger %d", a.ID, a.Balance, balance)
		}
	}
	return nil
}
