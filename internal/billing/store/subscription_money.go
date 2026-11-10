package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const monetarySubscriptionTier = "__money"

var errMonetaryQuota = errors.New("subscription money quota exceeded")

type MoneyAllowance struct {
	Period        string     `json:"period"`
	QuotaCent     int64      `json:"quota_cent"`
	ConsumedCent  int64      `json:"consumed_cent"`
	RemainingCent int64      `json:"remaining_cent"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
}

func (s *Store) subscriptionMoneyConsumed(ctx context.Context, tenantID uint64, since time.Time) (int64, error) {
	var rows []Bill
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND phase = ? AND funding = ? AND reserved_cost_cent IS NOT NULL AND status IN (?,?) AND (created_at >= ? OR (status = ? AND usage_reported_at IS NULL))", tenantID, PhaseReserve, FundingSubscription, StatusPending, StatusSettled, since, StatusPending).Find(&rows).Error
	if err != nil {
		return 0, err
	}
	var total int64
	for _, row := range rows {
		amount := *row.ReservedCostCent
		if row.UsageReportedAt != nil {
			if row.PromptTokens == nil || row.CompletionTokens == nil || row.InPriceCent == nil || row.OutPriceCent == nil {
				return 0, errors.New("subscription price or usage missing")
			}
			amount, err = quota.CostCentChecked(*row.PromptTokens, *row.CompletionTokens, *row.InPriceCent, *row.OutPriceCent)
			if err != nil {
				return 0, err
			}
		}
		if amount < 0 || total > math.MaxInt64-amount {
			return 0, errors.New("subscription cost overflow")
		}
		total += amount
	}
	return total, nil
}

func (s *Store) SubscriptionMoneyAllowance(ctx context.Context, sub *Subscription) ([]MoneyAllowance, error) {
	if sub == nil || sub.PlanType != PlanTypeRecurring {
		return nil, nil
	}
	plan, err := s.GetPlan(ctx, sub.PlanID)
	if err != nil || plan == nil {
		return nil, err
	}
	if plan.WindowQuotaCent == 0 && plan.WeekQuotaCent == 0 && plan.CycleQuotaCent == 0 {
		return nil, nil
	}
	if sub.CycleStart == nil {
		return nil, errors.New("subscription cycle missing")
	}
	periods := []struct {
		name  string
		limit int64
		since time.Time
	}{
		{"window", plan.WindowQuotaCent, time.Now().Add(-time.Duration(plan.RefreshHours) * time.Hour)},
		{"week", plan.WeekQuotaCent, time.Now().Add(-7 * 24 * time.Hour)},
		{"cycle", plan.CycleQuotaCent, *sub.CycleStart},
	}
	result := make([]MoneyAllowance, 0, 2)
	for _, period := range periods {
		if period.limit == 0 {
			continue
		}
		consumed, err := s.subscriptionMoneyConsumed(ctx, sub.TenantID, period.since)
		if err != nil {
			return nil, err
		}
		remaining := period.limit - consumed
		if remaining < 0 {
			remaining = 0
		}
		var resetAt *time.Time
		if period.name == "cycle" {
			resetAt = sub.CycleEnd
		} else if consumed > 0 {
			var first Bill
			err := s.db.WithContext(ctx).Where("tenant_id = ? AND phase = ? AND funding = ? AND reserved_cost_cent IS NOT NULL AND status IN (?,?) AND created_at >= ?", sub.TenantID, PhaseReserve, FundingSubscription, StatusPending, StatusSettled, period.since).Order("created_at ASC").First(&first).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			if err == nil {
				duration := time.Duration(plan.RefreshHours) * time.Hour
				if period.name == "week" {
					duration = 7 * 24 * time.Hour
				}
				next := first.CreatedAt.Add(duration)
				resetAt = &next
			}
		}
		result = append(result, MoneyAllowance{Period: period.name, QuotaCent: period.limit, ConsumedCent: consumed, RemainingCent: remaining, ResetsAt: resetAt})
	}
	return result, nil
}

func (s *Store) reserveMonetarySubscription(ctx context.Context, req *billingv1.ReserveRequest, model *Model, fingerprint string, reservedCost int64) (*billingv1.ReserveResponse, error) {
	tenantID := uint64(req.TenantId)
	inPrice, outPrice := model.InputPriceCent, model.OutputPriceCent
	row := &Bill{RequestID: req.RequestId, ReserveFingerprint: &fingerprint, Phase: PhaseReserve, Status: StatusPending, TenantID: tenantID, APIKeyID: uint64(req.ApiKeyId), Model: req.Model, Funding: FundingSubscription, ReservedCostCent: &reservedCost, InPriceCent: &inPrice, OutPriceCent: &outPrice, CreatedAt: time.Now()}
	if req.ProviderId > 0 {
		id := uint64(req.ProviderId)
		row.ProviderID = &id
	}
	if err := s.assignBillID(s.db, row); err != nil {
		return nil, err
	}
	err := s.db.WithContext(s.cashSQLContext()).Transaction(func(tx *gorm.DB) error {
		txStore := *s
		txStore.db = tx
		// Lock the initial lookup. A nonlocking lookup would establish an RR
		// snapshot before waiting, hiding the preceding owner's reservation.
		lookupStore := txStore
		lookupStore.db = tx.Clauses(clause.Locking{Strength: "UPDATE"})
		sub, err := lookupStore.GetActiveSubscription(ctx, tenantID)
		if err != nil {
			return err
		}
		if sub == nil {
			return errors.New("subscription unavailable")
		}
		if sub.CycleEnd == nil || !time.Now().Before(*sub.CycleEnd) || (sub.Status != SubActive && sub.Status != SubCancelled) {
			return errMonetaryQuota
		}
		allowances, err := txStore.SubscriptionMoneyAllowance(ctx, sub)
		if err != nil {
			return err
		}
		if len(allowances) == 0 {
			return errors.New("subscription budget unavailable")
		}
		for _, allowance := range allowances {
			if reservedCost > allowance.RemainingCent {
				return errMonetaryQuota
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		return insertOutbox(tx, newBillingEvent(row, 0, PhaseReserve))
	})
	if errors.Is(err, errMonetaryQuota) {
		return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_SUB_QUOTA}, nil
	}
	if err != nil {
		if isDupKey(err) {
			if prior, lookupErr := s.getReserve(s.cashSQLContext(), req.RequestId); lookupErr == nil && prior != nil {
				if replayErr := verifyReserveReplayWithContext(ctx, prior, req); replayErr != nil {
					return nil, replayErr
				}
				return &billingv1.ReserveResponse{Code: reserveReplayCode(ctx), BillId: int64(prior.ID)}, nil
			}
		}
		return nil, err
	}
	balance, err := s.getBalance(s.cashSQLContext(), tenantID)
	return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(row.ID), BalanceAfter: balance}, err
}
