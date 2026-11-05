package comparison

import (
	"context"
	"fmt"
	"testing"
)

// A conflict is synthetic corruption of the isolated tables, not a production observation.
func TestProbeDiscoveryClassifiesConflictAndAdvances(t *testing.T) {
	db := setupHeldFailure(t)
	for _, q := range []string{
		"DELETE FROM eval_probe_generation_event",
		"DELETE FROM eval_bound_probe_outbox",
		"DELETE FROM eval_bound_probe_attempt",
		"INSERT INTO eval_bound_probe_attempt(id,token,generation,state,outcome) VALUES ('a-conflict','orig',1,'done',0),('b-done','token',2,'done',1),('c-pending','token',3,'done',0),('d-pending','token',4,'done',1)",
		"INSERT INTO eval_bound_probe_outbox(id,token,generation,outcome) VALUES ('a-conflict','orig',1,0),('b-done','token',2,1),('c-pending','token',3,0),('d-pending','token',4,1)",
		"INSERT INTO eval_probe_generation_event(event_id,token,generation,success) VALUES ('a-conflict','wrong',1,0),('b-done','token',2,1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	cursor := ""
	var got []string
	for i := 0; i < 3; i++ {
		page, err := scanProbeOutbox(ctx, db, cursor, 1)
		if err != nil || len(page) != 1 {
			t.Fatalf("page %d: %+v %v", i, page, err)
		}
		if page[0].ID <= cursor {
			t.Fatalf("cursor failed to advance: %q %+v", cursor, page)
		}
		got = append(got, fmt.Sprintf("%s:%s", page[0].ID, page[0].Status))
		cursor = page[0].ID
	}
	if fmt.Sprint(got) != "[a-conflict:conflict c-pending:pending d-pending:pending]" {
		t.Fatalf("rows=%v", got)
	}
	page, err := scanProbeOutbox(ctx, db, cursor, 1)
	if err != nil || len(page) != 0 {
		t.Fatalf("tail=%+v err=%v", page, err)
	}
	// An ongoing scan is finite. A subsequent scan starting at the beginning
	// must find an event inserted below the old cursor.
	if err := db.Exec("INSERT INTO eval_bound_probe_attempt(id,token,generation,state,outcome) VALUES ('aa-new','token',5,'done',0)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO eval_bound_probe_outbox(id,token,generation,outcome) VALUES ('aa-new','token',5,0)").Error; err != nil {
		t.Fatal(err)
	}
	page, err = scanProbeOutbox(ctx, db, "", 2)
	if err != nil || len(page) != 2 || page[0].ID != "a-conflict" || page[1].ID != "aa-new" {
		t.Fatalf("rescan=%+v err=%v", page, err)
	}
}

// A completed event matching the outbox still must not conceal a damaged attempt.
func TestProbeDiscoveryDetectsAttemptMismatch(t *testing.T) {
	db := setupHeldFailure(t)
	for _, q := range []string{
		"DELETE FROM eval_probe_generation_event", "DELETE FROM eval_bound_probe_outbox", "DELETE FROM eval_bound_probe_attempt",
		"INSERT INTO eval_bound_probe_attempt(id,token,generation,state,outcome) VALUES ('attempt-mismatch','wrong',1,'done',0),('null-outcome','right',1,'done',NULL)",
		"INSERT INTO eval_bound_probe_outbox(id,token,generation,outcome) VALUES ('attempt-mismatch','right',1,0),('missing-attempt','right',1,0),('null-outcome','right',1,0)",
		"INSERT INTO eval_probe_generation_event(event_id,token,generation,success) VALUES ('attempt-mismatch','right',1,0),('missing-attempt','right',1,0),('null-outcome','right',1,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := scanProbeOutbox(context.Background(), db, "", 10)
	if err != nil || len(rows) != 3 || rows[0].ID != "attempt-mismatch" || rows[0].Status != "conflict" || rows[1].ID != "missing-attempt" || rows[1].Status != "conflict" || rows[2].ID != "null-outcome" || rows[2].Status != "conflict" {
		t.Fatalf("mismatched attempt was hidden: rows=%+v err=%v", rows, err)
	}
}
