package comparison

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// The candidate covers only the pre-forward admission boundary; it does not
// implement the complete billing lifecycle or external response replay.
func TestSQLGateClaimRecoveryAndForwardFence(t *testing.T) {
	db, _ := isolatedDB(t)
	g := sqlIdemGate{db: db}
	if err := g.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := fmt.Sprintf("gate-%d", time.Now().UnixNano())
	fingerprint := "tenant1-body1"
	defer db.Where("request_id = ?", key).Delete(&sqlIdemIntent{})
	if state, e := g.claim(ctx, key, fingerprint, "old", 10*time.Second); e != nil || state != gateOwned {
		t.Fatalf("first %s %v", state, e)
	}
	if state, e := g.claim(ctx, key, "different", "other", time.Second); e != nil || state != gateConflict {
		t.Fatalf("conflict %s %v", state, e)
	}
	if state, e := g.claim(ctx, key, fingerprint, "other", time.Second); e != nil || state != gateBusy {
		t.Fatalf("before expiry %s %v", state, e)
	}
	// Test-only clock manipulation: expire the claim without depending on host scheduling.
	if e := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", key).Error; e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	wins := make(chan string, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := fmt.Sprintf("new-%d", i)
			state, e := g.claim(ctx, key, fingerprint, owner, 10*time.Second)
			if e != nil {
				errs <- e
				return
			}
			if state == gateOwned {
				wins <- owner
			} else if state != gateBusy {
				errs <- fmt.Errorf("unexpected state %s", state)
			}
		}(i)
	}
	wg.Wait()
	close(wins)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	var winner string
	count := 0
	for x := range wins {
		winner = x
		count++
	}
	if count != 1 {
		t.Fatalf("winners=%d", count)
	}
	if ok, e := g.beginForward(ctx, key, "old"); e != nil || ok {
		t.Fatalf("expired owner forwarded: %v %v", ok, e)
	}
	if ok, e := g.beginForward(ctx, key, winner); e != nil || !ok {
		t.Fatalf("winner cannot forward: %v %v", ok, e)
	}
	if e := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", key).Error; e != nil {
		t.Fatal(e)
	}
	if state, e := g.claim(ctx, key, fingerprint, "later", time.Second); e != nil || state != gateForwardUnknown {
		t.Fatalf("uncertain forward must not replay %s %v", state, e)
	}
	if ok, e := g.beginForward(ctx, key, "later"); e != nil || ok {
		t.Fatalf("second forwarded: %v %v", ok, e)
	}
}

func TestSQLGateConcurrentForwardAndCanceledContext(t *testing.T) {
	db, _ := isolatedDB(t)
	g := sqlIdemGate{db: db}
	if err := g.migrate(); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("gate-forward-%d", time.Now().UnixNano())
	defer db.Where("request_id = ?", key).Delete(&sqlIdemIntent{})
	ctx := context.Background()
	if state, e := g.claim(ctx, key, "same-params", "owner", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	var wg sync.WaitGroup
	success := make(chan bool, 32)
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := g.beginForward(ctx, key, "owner")
			if e != nil {
				errs <- e
			} else {
				success <- ok
			}
		}()
	}
	wg.Wait()
	close(success)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	n := 0
	for ok := range success {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("forward winners=%d", n)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if ok, e := g.beginForward(canceled, key, "owner"); e == nil || ok {
		t.Fatalf("canceled operation authorized: %v %v", ok, e)
	}
}
