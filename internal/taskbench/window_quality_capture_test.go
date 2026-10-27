package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestWindowQualityCaptureActualOwnedDatabase(t *testing.T) {
	permit, source, _, calls := fixtureWindowOwnedPermit(t)
	slot := permit.campaign.prepared.scope.Slots[0]
	closed := closedWindowOwnedRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Error("capture called provider")
		return nil, fmt.Errorf("closed")
	})
	d, err := newWindowOwnedDriver(permit, source, slot, slot.ArmOrder[0], permit.environment, permit.observer, "closed-model-key", "closed-judge-key", closed, closed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.Setup(context.Background(), d.caseDefinition); err != nil {
		t.Fatal(err)
	}
	first, err := captureWindowQualityEffects(context.Background(), d)
	if err != nil || len(first.Tables) != 16 || first.Audits == nil {
		t.Fatalf("complete owned capture missing: tables=%d error=%v", len(first.Tables), err)
	}
	second, err := captureWindowQualityEffects(context.Background(), d)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("stable capture changed", err)
	}
	raw, err := json.Marshal(first.Tables)
	if err != nil || bytes.Contains(raw, []byte(d.fixture.APIKey)) {
		t.Fatal("table capture contains raw fixture key")
	}
	for name, table := range first.Tables {
		if len(table.Columns) == 0 || table.Rows == nil {
			t.Fatal("incomplete table", name)
		}
	}
	audit := store.AgentAuditLog{SessionID: "owned-capture", TenantID: d.fixture.TenantID, Tool: "get_balance", Args: "{}", Result: "controlled synthetic"}
	if err = permit.environment.db.Create(&audit).Error; err != nil {
		t.Fatal(err)
	}
	third, err := captureWindowQualityEffects(context.Background(), d)
	// Fresh audit rows must not be silently removed to match the previous observation.
	if err == nil || third.Tables != nil {
		t.Fatal("live audit drift hidden")
	}
	if *calls != 0 {
		t.Fatal("capture sent model")
	}
}

func TestWindowQualityCaptureSessionBytesAndOwnership(t *testing.T) {
	permit, source, _, _ := fixtureWindowOwnedPermit(t)
	slot := permit.campaign.prepared.scope.Slots[0]
	closed := closedWindowOwnedRoundTripper(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("closed") })
	d, err := newWindowOwnedDriver(permit, source, slot, slot.ArmOrder[0], permit.environment, permit.observer, "closed-model", "closed-judge", closed, closed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.Setup(context.Background(), d.caseDefinition); err != nil {
		t.Fatal(err)
	}
	raw, pending, err := captureWindowQualitySession(context.Background(), d, true)
	if err != nil || pending != nil || string(raw) != "{}" {
		t.Fatal("first absent session not explicit", err)
	}
	d.session = "captured-private-session"
	if _, _, err = captureWindowQualitySession(context.Background(), d, true); err == nil {
		t.Fatal("missing existing session normalized")
	}
	body := fmt.Sprintf(`{"history":[],"pending_action":{"tool":"recharge","args":{"amount_money":100},"tenant_id":%d,"preview":"controlled","idem_key":"owned-idem"}}`, d.fixture.TenantID)
	if err = permit.environment.rdb.Set(context.Background(), "agent:session:"+d.session, body, 0).Err(); err != nil {
		t.Fatal(err)
	}
	raw, pending, err = captureWindowQualitySession(context.Background(), d, false)
	if err != nil || string(raw) != body || pending == nil || pending.IdemKey != "owned-idem" || pending.Pending.TenantID != d.fixture.TenantID || pending.SessionID != d.session {
		t.Fatal("session bytes not retained", err)
	}
	duplicate := `{"history":[],"pending_action":null,"pending_action":null}`
	if err = permit.environment.rdb.Set(context.Background(), "agent:session:"+d.session, duplicate, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = captureWindowQualitySession(context.Background(), d, false); err == nil {
		t.Fatal("duplicate raw session accepted")
	}
}

func TestWindowQualityCaptureClockCannotHideDrift(t *testing.T) {
	base := qualityPreviewExample(t).Before.State
	later, err := cloneWindowOwned(base)
	if err != nil {
		t.Fatal(err)
	}
	later.CapturedAt = later.CapturedAt.Add(time.Second)
	later.AnswerAuthority.CapturedAt = later.CapturedAt
	if !sameWindowQualityCapturedState(base, later) {
		t.Fatal("capture annotation alone rejected")
	}
	zone, err := cloneWindowOwned(base)
	if err != nil {
		t.Fatal(err)
	}
	zone.CapturedAt = zone.CapturedAt.In(time.FixedZone("different-capture-zone", 3600))
	zone.AnswerAuthority.CapturedAt = zone.CapturedAt
	if sameWindowQualityCapturedState(base, zone) {
		t.Fatal("changed capture timezone hidden")
	}
}
