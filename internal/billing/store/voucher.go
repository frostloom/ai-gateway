package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BalanceVoucher stores only a digest. The bearer code is returned once at issuance.
type BalanceVoucher struct {
	ID               uint64     `gorm:"primaryKey" json:"id"`
	CodeHash         string     `gorm:"type:char(64);not null;uniqueIndex:uk_voucher_hash" json:"-"`
	Prefix           string     `gorm:"size:16;not null" json:"prefix"`
	AmountCent       int64      `gorm:"not null" json:"amount_cent"`
	ExpiresAt        time.Time  `gorm:"not null" json:"expires_at"`
	RedeemedTenantID *uint64    `gorm:"index" json:"redeemed_tenant_id"`
	RedeemedAt       *time.Time `json:"redeemed_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

type IssuedBalanceVoucher struct {
	Code string         `json:"code"`
	Item BalanceVoucher `json:"item"`
}

type VoucherRedemption struct {
	AmountCent      int64 `json:"amount_cent"`
	BalanceCent     int64 `json:"balance_cent"`
	AlreadyRedeemed bool  `json:"already_redeemed"`
}

func (s *Store) CreateBalanceVouchers(ctx context.Context, amountMoney int64, count, validityDays int) ([]IssuedBalanceVoucher, error) {
	if amountMoney < 1 || amountMoney > 1000000 || count < 1 || count > 100 || validityDays < 1 || validityDays > 366 {
		return nil, errors.New("金额须为 1–1000000 元，数量 1–100，有效期 1–366 天")
	}
	issued := make([]IssuedBalanceVoucher, count)
	rows := make([]BalanceVoucher, count)
	now := time.Now()
	for i := range rows {
		random := make([]byte, 20)
		if _, err := rand.Read(random); err != nil {
			return nil, err
		}
		code := "AGV-" + strings.ToUpper(hex.EncodeToString(random))
		digest := sha256.Sum256([]byte(code))
		rows[i] = BalanceVoucher{CodeHash: hex.EncodeToString(digest[:]), Prefix: code[:12], AmountCent: amountMoney * quota.CentPerYuan, ExpiresAt: now.Add(time.Duration(validityDays) * 24 * time.Hour), CreatedAt: now}
		issued[i].Code = code
	}
	if err := s.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		issued[i].Item = rows[i]
	}
	return issued, nil
}

func (s *Store) ListBalanceVouchers(ctx context.Context) ([]BalanceVoucher, error) {
	rows := make([]BalanceVoucher, 0)
	err := s.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&rows).Error
	return rows, err
}

func (s *Store) RedeemBalanceVoucher(ctx context.Context, tenantID uint64, rawCode string) (*VoucherRedemption, error) {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if tenantID == 0 || len(code) != 44 || !strings.HasPrefix(code, "AGV-") {
		return nil, errors.New("兑换码无效、已使用或已过期")
	}
	if _, err := hex.DecodeString(code[4:]); err != nil {
		return nil, errors.New("兑换码无效、已使用或已过期")
	}
	attempts, err := s.rdb.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; return n`, []string{fmt.Sprintf("billing:voucher-attempt:%d", tenantID)}).Int64()
	if err != nil {
		return nil, err
	}
	if attempts > 10 {
		return nil, errors.New("兑换操作频繁，请稍后重试")
	}
	locked, release, err := s.lockCash(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer release()
	// A batch with pending debits cannot be rebuilt from SQL. Wait for that
	// tenant's existing flush before applying a durable external credit.
	if locked.batch != nil && locked.batch.behind(tenantID) {
		return nil, errors.New("账户结算中，请稍后重新兑换")
	}
	dbctx := locked.cashSQLContext()
	digest := sha256.Sum256([]byte(code))
	if locked.batch != nil {
		ledger, err := LedgerBalance(dbctx, locked.db, tenantID)
		if err != nil {
			return nil, err
		}
		observed, err := locked.getBalance(dbctx, tenantID)
		if err != nil {
			return nil, err
		}
		if observed != ledger {
			// A committed credit awaiting synchronization is indistinguishable
			// from a debit lost from this process's batch after restart.
			return nil, errors.New("账户余额待核对，请稍后重新兑换")
		}
	}
	locked.batch = nil
	if err := locked.requireCashMatch(dbctx, tenantID); err != nil {
		return nil, err
	}
	live, err := locked.getBalance(dbctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := &VoucherRedemption{}
	if err := locked.cashMutationAllowed(); err != nil {
		return nil, err
	}
	err = locked.db.WithContext(dbctx).Transaction(func(tx *gorm.DB) error {
		var tenant Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&tenant, tenantID).Error; err != nil {
			return err
		}
		if tenant.Status != 0 {
			return errors.New("账户已停用")
		}
		var voucher BalanceVoucher
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", hex.EncodeToString(digest[:])).First(&voucher).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("兑换码无效、已使用或已过期")
			}
			return err
		}
		if voucher.RedeemedTenantID != nil {
			if *voucher.RedeemedTenantID != tenantID {
				return errors.New("兑换码无效、已使用或已过期")
			}
			result.AmountCent = voucher.AmountCent
			result.AlreadyRedeemed = true
			return nil
		}
		if !time.Now().Before(voucher.ExpiresAt) || voucher.AmountCent <= 0 {
			return errors.New("兑换码无效、已使用或已过期")
		}
		if live < 0 || live > quota.MaxCashLua-voucher.AmountCent {
			return errors.New("兑换后余额超出安全范围")
		}
		inserted, err := insertTopup(tx, &Topup{TenantID: tenantID, Source: "voucher", Amount: voucher.AmountCent, RefType: "voucher", RefID: fmt.Sprint(voucher.ID), CreatedAt: time.Now()})
		if err != nil {
			return err
		}
		if !inserted {
			return errors.New("兑换流水冲突")
		}
		if err := addLedgerBalance(tx, tenantID, voucher.AmountCent); err != nil {
			return err
		}
		now := time.Now()
		updated := tx.Model(&BalanceVoucher{}).Where("id = ? AND redeemed_tenant_id IS NULL", voucher.ID).Updates(map[string]any{"redeemed_tenant_id": tenantID, "redeemed_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("兑换码已使用")
		}
		result.AmountCent = voucher.AmountCent
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := locked.requireCashMatch(dbctx, tenantID); err != nil {
		return nil, fmt.Errorf("兑换已入账，余额同步待恢复，请使用同一码重试：%w", err)
	}
	result.BalanceCent, err = locked.getBalance(dbctx, tenantID)
	return result, err
}
