package taskbench

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
)

func overviewOracle(t *testing.T) (*pairedStoreOracle, Fixture) {
	t.Helper()
	o := localCampaignOptions(t)
	source, err := LoadConversations(o.DefinitionsDir)
	if err != nil {
		t.Fatal(err)
	}
	small, err := PrepareSmallPair(source)
	if err != nil {
		t.Fatal(err)
	}
	oracle := &pairedStoreOracle{env: o.Environment}
	c := small.Suite.Cases[0]
	c.Scenario.Profile = "active_standard" // Exercise nonempty allowance as well as cash state.
	f, _, err := oracle.Setup(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return oracle, f
}

func TestPairedOracleStateReadsEachOverviewOnce(t *testing.T) {
	o, f := overviewOracle(t)
	var catalogs atomic.Int64
	const callback = "taskbench:count_overview_catalogs"
	if err := o.env.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "plans" && db.Statement.Clauses["WHERE"].Expression == nil {
			catalogs.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { o.env.db.Callback().Query().Remove(callback) })
	ctx := context.Background()
	s, err := o.State(ctx, f, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := catalogs.Load(); got != 2 {
		t.Fatalf("one fresh overview for own and control account required; complete catalog queries=%d want2", got)
	}
	if len(s.Allowance) == 0 {
		t.Fatal("active subscription allowance was not exercised")
	}
	b := &LiveBackend{db: o.env.db, rdb: o.env.rdb, st: o.env.st}
	own, err := b.Snapshot(ctx, f, "")
	if err != nil || !reflect.DeepEqual(own, s.Own) {
		t.Fatal("own snapshot changed", err)
	}
	control, err := b.Snapshot(ctx, Fixture{TenantID: o.foreign}, "")
	if err != nil || !reflect.DeepEqual(control, s.Controls[o.foreign]) {
		t.Fatal("control snapshot changed", err)
	}
	me, err := o.env.st.MeOverview(ctx, f.TenantID)
	if err != nil || !reflect.DeepEqual(me.Allowance, s.Allowance) {
		t.Fatal("allowance changed", err)
	}
	authority, err := CaptureConversationAnswerAuthority(ctx, o.env.st, f.TenantID, s.CapturedAt)
	if err != nil || !reflect.DeepEqual(authority, s.AnswerAuthority) {
		t.Fatal("independent authority semantics changed", err)
	}
}

func TestPairedOracleOverviewNeverCrossesCaptures(t *testing.T) {
	o, f := overviewOracle(t)
	ctx := context.Background()
	before, err := o.State(ctx, f, "")
	if err != nil {
		t.Fatal(err)
	}
	plan := before.AnswerAuthority.Plans[0]
	name := "fresh-" + randomID()
	if err := o.env.db.Model(&store.Plan{}).Where("id = ?", plan.ID).Update("name", name).Error; err != nil {
		t.Fatal(err)
	}
	if err := o.env.db.Model(&store.Plan{}).Where("id = ?", f.Standard.ID).Update("tier_quota", `{"文本":24680}`).Error; err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	var model store.Model
	if err := o.env.db.First(&model).Error; err != nil {
		t.Fatal(err)
	}
	bill := store.Bill{TenantID: f.TenantID, RequestID: randomID(), Model: model.ModelID, Phase: "settle", Status: "settled", ActualQuota: &zero, DeltaQuota: &zero, CreatedAt: time.Now()}
	if err := o.env.db.Create(&bill).Error; err != nil {
		t.Fatal(err)
	}
	after, err := o.State(ctx, f, "")
	if err != nil {
		t.Fatal(err)
	}
	if before.AnswerAuthority.Plans[0].Name == name || after.AnswerAuthority.Plans[0].Name != name || after.AnswerAuthority.BillsTotal != before.AnswerAuthority.BillsTotal+1 || after.AnswerAuthority.RecentBills[0].ID != bill.ID {
		t.Fatal("overview or bills reused from previous capture")
	}
	if len(after.Allowance) != 1 || after.Allowance[0].Tier != "文本" || after.Allowance[0].Quota != 24680 {
		t.Fatal("allowance reused from previous capture")
	}
	if err := validateAnswerAuthority(after, f); err != nil {
		t.Fatal(err)
	}
	wrong := f
	wrong.TenantID = o.foreign
	if _, err := o.State(ctx, wrong, ""); err == nil {
		t.Fatal("foreign fixture accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := o.State(cancelled, f, ""); err == nil {
		t.Fatal("cancelled fresh read replaced by cached success")
	}
}

func TestPairedOracleOverviewFailuresPropagate(t *testing.T) {
	o, f := overviewOracle(t)
	for _, tc := range []struct {
		name, table string
		tenant      uint64
		arg         int
	}{{"own", "recharge_orders", f.TenantID, 0}, {"control", "recharge_orders", o.foreign, 0}, {"authority", "bills", f.TenantID, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("synthetic oracle read failure")
			const callback = "taskbench:fail_oracle_read"
			if err := o.env.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
				if db.Statement.Table == tc.table && len(db.Statement.Vars) > tc.arg && db.Statement.Vars[tc.arg] == tc.tenant {
					db.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { o.env.db.Callback().Query().Remove(callback) })
			state, err := o.State(context.Background(), f, "")
			if !errors.Is(err, failure) || state.AnswerAuthority != nil {
				t.Fatalf("%s read failure swallowed: %v", tc.name, err)
			}
		})
	}
}
