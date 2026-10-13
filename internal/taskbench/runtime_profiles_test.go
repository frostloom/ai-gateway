package taskbench

import (
	"context"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestSixRuntimeProfilesInstantiateRealIsolatedAccounts(t *testing.T) {
	db, rdb, st := runtimeProfileTestDatabase(t)
	profiles := loadRuntimeProfileArchive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	clock := time.Now()
	plans := seedRuntimeCatalogue(t, db, profiles)
	seen := map[uint64]bool{}
	for _, name := range []string{"no_subscription", "active_standard", "active_flagship", "cancelled_standard", "low_balance", "empty_balance"} {
		t.Run(name, func(t *testing.T) {
			p := profiles.Accounts[name]
			f, foreign := instantiateRuntimeAccount(t, ctx, db, rdb, st, name, p, plans, clock)
			if seen[f.TenantID] || seen[foreign] || f.TenantID == foreign || f.APIKey == "" {
				t.Fatal("accounts must have distinct tenant/key identity")
			}
			seen[f.TenantID], seen[foreign] = true, true
			backend := &LiveBackend{db: db, rdb: rdb, st: st}
			snap, err := backend.Snapshot(ctx, f, "")
			if err != nil {
				t.Fatal(err)
			}
			if !snap.Consistent || snap.BalanceCent != p.Balance || snap.LedgerBalance != p.Balance {
				t.Fatalf("wallet/ledger mismatch: %+v", snap)
			}
			if len(snap.Orders) != 1 || snap.Orders[0].Status != store.RechargePaid || snap.Orders[0].AmountCent != p.Order.AmountCent || snap.Orders[0].AmountMoney*100 != snap.Orders[0].AmountCent || snap.Orders[0].TenantID != f.TenantID {
				t.Fatal("owned integer-yuan paid order mismatch")
			}
			verifyRuntimeReads(t, ctx, db, st, f, p, plans, clock)
			foreignView, err := st.MeOverview(ctx, foreign)
			if err != nil || !foreignView.Consistent || foreignView.BalanceCent != 123400 {
				t.Fatalf("foreign control mismatch: %v %+v", err, foreignView)
			}
		})
	}
	if len(seen) != 12 {
		t.Fatal("expected six own accounts and six foreign controls")
	}
	if t.Failed() {
		return
	}
	t.Log("Verified persistence for six runtime profiles and six foreign controls; cancelled read-projection gap separately recorded; zero model/JEV calls; disposable database cleanup registered")
}
