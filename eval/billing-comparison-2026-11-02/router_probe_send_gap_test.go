package comparison

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// PASS demonstrates a dangerous interleaving in the test-only prototype.
func TestBoundProbeSenderPausedPastReplacement(t *testing.T) {
	db, _ := isolatedDB(t)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_probe_generation (id BIGINT PRIMARY KEY, generation BIGINT NOT NULL, token VARBINARY(64), expires DATETIME(6), state VARCHAR(16) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_bound_probe_attempt (id VARBINARY(32) PRIMARY KEY, token VARBINARY(64) NOT NULL, generation BIGINT NOT NULL, state VARCHAR(16) NOT NULL, outcome BOOLEAN NULL) ENGINE=InnoDB",
		"DELETE FROM eval_probe_generation", "INSERT INTO eval_probe_generation(id,generation,state) VALUES(1,0,'open')", "DELETE FROM eval_bound_probe_attempt",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer upstream.Close()
	if gen, err := allocateBoundProbe(ctx, db, "old", "old-token"); err != nil || gen != 1 {
		t.Fatalf("old allocation=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "old"); err != nil || !ok {
		t.Fatalf("old send permission=%v err=%v", ok, err)
	}
	// Old sender is paused after its committed sending transition, before HTTP.
	if err := db.Exec("UPDATE eval_probe_generation SET expires=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if gen, err := allocateBoundProbe(ctx, db, "new", "new-token"); err != nil || gen != 2 {
		t.Fatalf("new allocation=%d err=%v", gen, err)
	}
	if ok, err := beginBoundProbe(ctx, db, "new"); err != nil || !ok {
		t.Fatalf("new send permission=%v err=%v", ok, err)
	}
	// Neither the prototype nor this ordinary HTTP upstream provides fencing.
	for _, name := range []string{"new", "old"} {
		resp, err := upstream.Client().Post(upstream.URL, "text/plain", nil)
		if err != nil {
			t.Fatalf("%s request: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("%s status=%d", name, resp.StatusCode)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
	t.Logf("old and replacement both entered sending and local upstream received %d requests", calls.Load())
}
