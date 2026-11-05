package comparison

import (
	"context"
	"errors"
	"fmt"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// Test-only durable pre-forward gate. Does not execute the gateway/billing.
type sqlIdemIntent struct {
	RequestID   string    `gorm:"primaryKey;size:128"`
	Fingerprint string    `gorm:"size:128;not null"`
	State       string    `gorm:"size:24;not null"`
	Owner       string    `gorm:"size:128;not null"`
	LeaseUntil  time.Time `gorm:"precision:6;not null"`
}

func (sqlIdemIntent) TableName() string { return "eval_idem_gate" }

type sqlIdemGate struct{ db *gorm.DB }

const (
	gateOwned          = "owned"
	gateBusy           = "busy"
	gateConflict       = "conflict"
	gateForwardUnknown = "forward_unknown"
)

func (g *sqlIdemGate) migrate() error { return g.db.AutoMigrate(&sqlIdemIntent{}) }
func (g *sqlIdemGate) claim(ctx context.Context, request, fingerprint, owner string, lease time.Duration) (string, error) {
	if request == "" || fingerprint == "" || owner == "" || lease < time.Microsecond {
		return "", fmt.Errorf("invalid candidate input")
	}
	q := g.db.WithContext(ctx).Exec("INSERT INTO eval_idem_gate (request_id,fingerprint,state,owner,lease_until) VALUES (?,?,'claimed',?,DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND))", request, fingerprint, owner, lease.Microseconds())
	if q.Error == nil {
		return gateOwned, nil
	}
	var me *drivermysql.MySQLError
	if !errors.As(q.Error, &me) || me.Number != 1062 {
		return "", q.Error
	}
	var row struct {
		Fingerprint, State, Owner string
		LeaseLive                 bool
	}
	res := g.db.WithContext(ctx).Raw("SELECT fingerprint,state,owner,lease_until > CURRENT_TIMESTAMP(6) AS lease_live FROM eval_idem_gate WHERE request_id=?", request).Scan(&row)
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected != 1 {
		return "", fmt.Errorf("intent disappeared")
	}
	if row.Fingerprint != fingerprint {
		return gateConflict, nil
	}
	if row.State == "forwarding" {
		return gateForwardUnknown, nil
	}
	if row.State != "claimed" {
		return "", fmt.Errorf("unknown intent state")
	}
	if row.LeaseLive {
		if row.Owner == owner {
			return gateOwned, nil
		}
		return gateBusy, nil
	}
	takeover := g.db.WithContext(ctx).Exec("UPDATE eval_idem_gate SET owner=?,lease_until=DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND) WHERE request_id=? AND fingerprint=? AND state='claimed' AND lease_until <= CURRENT_TIMESTAMP(6)", owner, lease.Microseconds(), request, fingerprint)
	if takeover.Error != nil {
		return "", takeover.Error
	}
	if takeover.RowsAffected == 1 {
		return gateOwned, nil
	}
	return gateBusy, nil
}
func (g *sqlIdemGate) beginForward(ctx context.Context, request, owner string) (bool, error) {
	result := g.db.WithContext(ctx).Exec("UPDATE eval_idem_gate SET state='forwarding' WHERE request_id=? AND owner=? AND state='claimed' AND lease_until > CURRENT_TIMESTAMP(6)", request, owner)
	return result.RowsAffected == 1, result.Error
}
