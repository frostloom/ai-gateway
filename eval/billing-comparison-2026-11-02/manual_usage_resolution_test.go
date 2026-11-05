package comparison

import (
	"context"
	"encoding/hex"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type manualUsageResolution struct {
	RequestID        string
	TenantID         uint64
	BillID           int64
	ProviderID       int64
	PromptTokens     int64
	CompletionTokens int64
	EvidenceSHA      string
	Actor            string
}

var errManualConflict = errors.New("manual resolution conflict")

func recordManualUsage(ctx context.Context, db *gorm.DB, x manualUsageResolution) error {
	raw, err := hex.DecodeString(x.EvidenceSHA)
	if err != nil || len(raw) != 32 || strings.ToLower(x.EvidenceSHA) != x.EvidenceSHA || x.RequestID == "" || x.TenantID == 0 || x.BillID <= 0 || x.ProviderID <= 0 || x.PromptTokens < 0 || x.CompletionTokens < 0 || x.Actor == "" || len(x.Actor) > 128 {
		return errors.New("invalid manual resolution")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var bill struct {
			ID              int64
			TenantID        uint64
			RequestID       string
			ProviderID      *uint64
			Status          string
			ExecutionState  string
			UsageReportedAt *time.Time
		}
		if err := tx.Table("bills").Select("id,tenant_id,request_id,provider_id,status,execution_state,usage_reported_at").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND request_id=? AND phase='reserve'", x.BillID, x.TenantID, x.RequestID).Take(&bill).Error; err != nil {
			return err
		}
		var previous manualUsageResolution
		err := tx.Table("eval_manual_usage").Where("request_id=?", x.RequestID).Take(&previous).Error
		if err == nil {
			if previous != x {
				return errManualConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if bill.Status != "pending" || bill.ExecutionState != "forwarding" || bill.UsageReportedAt != nil || bill.ProviderID == nil || *bill.ProviderID != uint64(x.ProviderID) {
			return errors.New("bill not eligible for manual usage")
		}
		entry := inboxUsage{RequestID: x.RequestID, TenantID: x.TenantID, BillID: x.BillID, ProviderID: x.ProviderID, PromptTokens: x.PromptTokens, CompletionTokens: x.CompletionTokens, State: "pending"}
		if err := tx.Table("eval_usage_inbox").Create(&entry).Error; err != nil {
			return err
		}
		return tx.Table("eval_manual_usage").Create(&x).Error
	})
}
