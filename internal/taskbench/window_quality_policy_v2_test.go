package taskbench

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"testing"
	"time"
)

func qualityPreviewExample(t *testing.T) qualityPreviewInput {
	t.Helper()
	c, d := rechargeConversation(), rechargeDriver()
	d.initial.CapturedAt = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	d.initial.AnswerAuthority = &ConversationAnswerAuthority{TenantID: 19, CapturedAt: d.initial.CapturedAt, CatalogComplete: true, Plans: []store.Plan{}, BillsLimit: 10, BillsPhase: "settle", BillsStatus: "settled", ConsumptionComplete: true, ConsumptionDays: 7, RefreshHours: 24}
	c.Scenario.Turns = c.Scenario.Turns[1:]
	c.Scenario.Terminal = map[string]any{"business_state_unchanged": true}
	obs := d.steps[1]
	obs.Pending.Args = map[string]any{"amount_money": float64(200)}
	obs.Turn.Response.Preview = "充值200元"
	obs.Turn.Response.Trace = []Trace{{Tool: "recharge", Args: `{"amount_money":200}`, ResolvedArgs: `{"amount_money":200}`, Preview: "充值200元", Result: "等待确认", Phase: "preview", Outcome: "pending"}}
	obs.Turn.Response.Telemetry = &Telemetry{TurnTelemetry: agent.TurnTelemetry{LangfuseTraceID: "trace", Runtime: &agent.RuntimeMetadata{}, LLMCalls: 1, LLMUsageResponses: 1}}
	raw, _ := json.Marshal(obs.Turn.Response)
	obs.Turn.RawBody = string(raw)
	obs.Turn.TraceRaw, _ = json.Marshal(obs.Turn.Response.Trace)
	obs.State = cloneConversationState(d.initial)
	obs.State.Own.Audits = []store.AgentAuditLog{{ID: 4, SessionID: "session-19", TenantID: 19, Tool: "recharge", Args: `{"amount_money":200}`, Preview: "充值200元", Result: "pending_confirmation"}}
	d.steps = []ConversationObservation{obs}
	record := recordCollectionConversation(context.Background(), c, d)
	tables := map[string]qualityTableCapture{}
	for _, key := range []string{"tenants", "api_keys", "providers", "models", "bills", "plans", "subscriptions", "recharge_orders", "topups", "agent_sessions", "outbox_events", "audit_events", "admin_users", "admin_sessions", "portal_users", "portal_sessions"} {
		tables[key] = qualityTableCapture{Columns: []string{"id"}, Rows: []string{}}
	}
	sessionColumns := []string{"id", "session_id", "tenant_id", "api_key_id", "created_at", "last_active_at"}
	beforeSessions := []store.AgentSession{}
	afterSessions := []store.AgentSession{{ID: 7, SessionID: "session-19", TenantID: 19, CreatedAt: d.initial.CapturedAt, LastActiveAt: d.initial.CapturedAt}}
	tables["agent_sessions"], _ = qualitySessionTableCapture(beforeSessions, sessionColumns)
	afterTables := map[string]qualityTableCapture{}
	for name, value := range tables {
		afterTables[name] = value
	}
	afterTables["agent_sessions"], _ = qualitySessionTableCapture(afterSessions, sessionColumns)
	return qualityPreviewInput{Record: record, Turn: 1, BeforeSession: json.RawMessage(`{}`), Pending: &qualityPendingCapture{Pending: *obs.Pending, Preview: "充值200元", IdemKey: "recharge-id", SessionID: "session-19", Raw: json.RawMessage(`{"history":[],"pending_action":{"tool":"recharge","args":{"amount_money":200},"preview":"充值200元","idem_key":"recharge-id","tenant_id":19}}`)}, Before: qualityEffectCapture{State: d.initial, Tables: tables, Audits: []store.AgentAuditLog{}, Sessions: beforeSessions}, After: qualityEffectCapture{State: obs.State, Tables: afterTables, Audits: obs.State.Own.Audits, Sessions: afterSessions}}
}
func TestQualityPreviewV2WrongAmountIsCandidateOnly(t *testing.T) {
	in := qualityPreviewExample(t)
	got, err := qualityPreviewCandidate(in)
	if err != nil {
		t.Fatalf("known wrong owned no-effect preview should produce data-only candidate: %v", err)
	}
	if got.Disposition != "candidate_precleanup" || !got.AuditOnly || got.CompletionProof != "unavailable_from_recording" {
		t.Fatalf("candidate implies authority: %+v", got)
	}
	if len(got.Unrun) != 1 || got.Unrun[0].Turn != 2 || got.Unrun[0].Rule.User != "确认" {
		t.Fatalf("lost original confirmation suffix: %+v", got.Unrun)
	}
	if got.Evidence.Record.Result.StructuralPass || got.Evidence.Record.Result.Success || got.Evidence.Record.Disposition != "halt" {
		t.Fatal("raw v1 negative was promoted")
	}
}

func qualityRebind(t *testing.T, in *qualityPreviewInput) {
	t.Helper()
	r := &in.Record.Result
	obs := &r.Observations[len(r.Observations)-1]
	raw, _ := json.Marshal(obs.Turn.Response)
	obs.Turn.RawBody = string(raw)
	obs.Turn.TraceRaw, _ = json.Marshal(obs.Turn.Response.Trace)
	in.After.State = obs.State
	d := &replayConversationDriver{episode: ConversationEpisode{Key: r.Case.Key(), Fixture: r.Fixture, Initial: r.Initial, Final: obs.State, Steps: r.Observations}}
	in.Record = recordCollectionConversation(context.Background(), r.Case, d)
}
func TestQualityPreviewV2RejectsUnsafeEvidence(t *testing.T) {
	for _, name := range []string{"foreign", "pending-inconsistent", "unknown", "failure", "multiple", "execute", "effect", "audit-missing", "audit-extra", "audit-old-changed", "money-fraction", "money-negative", "money-zero", "money-overflow", "duplicate-key", "missing-table", "missing-rows", "prior-pending", "fake-v1", "correct-preview", "raw-response", "idem-missing"} {
		t.Run(name, func(t *testing.T) {
			in := qualityPreviewExample(t)
			o := &in.Record.Result.Observations[0]
			switch name {
			case "foreign":
				in.Pending.Pending.TenantID = 20
			case "pending-inconsistent":
				in.Pending.Pending.Args = map[string]any{"amount_money": float64(300)}
			case "unknown":
				o.Turn.Response.Trace[0].Tool = "other"
			case "failure":
				o.Turn.Response.Trace[0].Outcome = "failure"
			case "multiple":
				o.Turn.Response.Trace = append(o.Turn.Response.Trace, o.Turn.Response.Trace[0])
			case "execute":
				o.Turn.Response.Trace[0].Phase = "execute"
			case "effect":
				in.After.State.Own.BalanceCent++
			case "audit-missing":
				in.After.Audits = []store.AgentAuditLog{}
			case "audit-extra":
				in.After.Audits = append(in.After.Audits, store.AgentAuditLog{ID: 9, Tool: "refund_recharge", Confirmed: true})
			case "audit-old-changed":
				in.Before.Audits = []store.AgentAuditLog{{ID: 4, Tool: "recharge", Result: "different"}}
			case "money-fraction":
				o.Turn.Response.Trace[0].Args = `{"amount_money":200.5}`
			case "money-negative":
				o.Turn.Response.Trace[0].Args = `{"amount_money":-1}`
			case "money-zero":
				o.Turn.Response.Trace[0].Args = `{"amount_money":0}`
			case "money-overflow":
				o.Turn.Response.Trace[0].Args = `{"amount_money":92233720368547759}`
			case "duplicate-key":
				o.Turn.Response.Trace[0].Args = `{"amount_money":100,"amount_money":200}`
			case "missing-table":
				delete(in.Before.Tables, "bills")
			case "missing-rows":
				v := in.Before.Tables["bills"]
				v.Rows = nil
				in.Before.Tables["bills"] = v
			case "prior-pending":
				in.BeforeSession = in.Pending.Raw
			case "fake-v1":
				in.Record.Disposition = "continue_recorded"
			case "correct-preview":
				in.Record.Result.Case.Scenario.Operation.Arguments["amount_money"] = float64(200)
			case "raw-response":
				o.Turn.RawBody = `{}`
			case "idem-missing":
				in.Pending.IdemKey = ""
			}
			// Do not rebind: even coherent later callers must pass frozen raw v1 replay.
			if _, err := qualityPreviewCandidate(in); err == nil {
				t.Fatal("unsafe or unknown candidate accepted")
			}
		})
	}
}

func TestQualityPreviewV2SidecarAuditsOnlyAndBindsSuffix(t *testing.T) {
	got, err := qualityPreviewCandidate(qualityPreviewExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyQualityPreviewSidecar(got); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var roundtrip qualityPreviewSidecar
	if json.Unmarshal(raw, &roundtrip) != nil {
		t.Fatal("decode")
	}
	if err := verifyQualityPreviewSidecar(roundtrip); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"suffix", "sha", "v1sha", "proof", "source", "turn", "raw-v1"} {
		t.Run(fault, func(t *testing.T) {
			var bad qualityPreviewSidecar
			_ = json.Unmarshal(raw, &bad)
			switch fault {
			case "suffix":
				bad.Unrun = nil
			case "sha":
				bad.PolicySHA256 = collectionPolicySHA()
			case "v1sha":
				bad.Evidence.Record.PolicySHA256 = bad.PolicySHA256
			case "proof":
				bad.CompletionProof = "owned"
			case "source":
				bad.SourceSHA256 = ""
			case "turn":
				bad.Evidence.Turn++
			case "raw-v1":
				bad.Evidence.Record.Result.Success = true
			}
			if verifyQualityPreviewSidecar(bad) == nil {
				t.Fatal("tampered read-only sidecar accepted")
			}
		})
	}
	if collectionPolicySHA() != "604c68b1c192c17594ba422b85367579cabd60c31623af6a9632278e401ae970" {
		t.Fatal("old v1 identity changed")
	}
}
func TestQualityPreviewV2RejectsCoherentUnsafeArguments(t *testing.T) {
	for _, raw := range []string{`{"amount_money":200.5}`, `{"amount_money":"200"}`, `{"amount_money":0}`, `{"amount_money":-1}`, `{"amount_money":92233720368547759}`, `{"amount_money":200,"unknown":1}`, `{"amount_money":100,"amount_money":200}`, `{"amount_money":200,"tenant_id":20}`} {
		t.Run(raw, func(t *testing.T) {
			in := qualityPreviewExample(t)
			var args map[string]any
			_ = json.Unmarshal([]byte(raw), &args)
			o := &in.Record.Result.Observations[0]
			o.Turn.Response.Trace[0].Args = raw
			o.Turn.Response.Trace[0].ResolvedArgs = raw
			o.Pending.Args = args
			in.Pending.Pending.Args = args
			in.Pending.Raw, _ = json.Marshal(map[string]any{"history": []any{}, "pending_action": map[string]any{"tool": "recharge", "args": args, "preview": in.Pending.Preview, "idem_key": in.Pending.IdemKey, "tenant_id": 19}})
			in.After.Audits[0].Args = raw
			o.State.Own.Audits = in.After.Audits
			qualityRebind(t, &in)
			if _, err := qualityPreviewCandidate(in); err == nil {
				t.Fatal("coherent unsafe money accepted")
			}
		})
	}
}
func TestQualityPreviewV2TargetSchemas(t *testing.T) {
	f, s := conversationFixture()
	s.Own.Orders = []store.RechargeOrder{{ID: 9, TenantID: 19, OrderNo: "another-own", Status: store.RechargePaid, AmountMoney: 200, AmountCent: 20000}}
	s.Own.Subscriptions = []store.Subscription{{ID: 1, TenantID: 19, PlanID: 47, PlanType: store.PlanTypeRecurring, Status: store.SubActive}}
	plans := []store.Plan{{ID: 47, Name: "月度·标准", PlanType: store.PlanTypeRecurring}, {ID: 48, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring}}
	tests := []struct {
		tool, raw, resolved string
		want                bool
	}{
		{"recharge", `{"amount_money":200}`, `{"amount_money":200}`, true},
		{"refund_recharge", `{"amount_money":200}`, `{"amount_money":200}`, true},
		{"refund_recharge", `{"order_no":"another-own"}`, `{"order_no":"another-own"}`, true},
		{"refund_recharge", `{"order_no":"foreign"}`, `{"order_no":"foreign"}`, false},
		{"refund_recharge", `{"amount_money":200,"order_no":"another-own"}`, `{"amount_money":200,"order_no":"another-own"}`, false},
		{"subscribe_plan", `{"plan_name":"旗舰套餐"}`, `{"plan_id":48,"plan_name":"月度·旗舰"}`, true},
		{"change_plan", `{"plan_id":48}`, `{"plan_id":48,"plan_name":"月度·旗舰"}`, true},
		{"subscribe_plan", `{"plan_id":47}`, `{"plan_id":48,"plan_name":"月度·旗舰"}`, false},
		{"change_plan", `{"plan_name":"月度"}`, `{"plan_id":48,"plan_name":"月度·旗舰"}`, false},
		{"cancel_subscription", `{}`, `{}`, true},
		{"cancel_subscription", `{"unknown":1}`, `{"unknown":1}`, false},
	}
	for _, test := range tests {
		t.Run(test.tool+test.raw, func(t *testing.T) {
			raw, e := windowJSONObject([]byte(test.raw))
			if e != nil {
				t.Fatal(e)
			}
			resolved, e := windowJSONObject([]byte(test.resolved))
			if e != nil {
				t.Fatal(e)
			}
			err := qualityPreviewTarget(test.tool, raw, resolved, f, s, plans)
			if (err == nil) != test.want {
				t.Fatalf("allowed=%v err=%v", test.want, err)
			}
		})
	}
	t.Run("cancel-no-owned-subscription", func(t *testing.T) {
		s.Own.Subscriptions = nil
		if qualityPreviewTarget("cancel_subscription", map[string]any{}, map[string]any{}, f, s, plans) == nil {
			t.Fatal("cancel target missing but accepted")
		}
	})
}
func TestQualityPreviewV2RejectsAuditProjectionContradiction(t *testing.T) {
	in := qualityPreviewExample(t)
	in.After.Audits[0].Args = `{"amount_money":300}`
	// Keep raw record at200 and coherentalltablecapture, then supply contradictoryall-auditdata.
	if _, err := qualityPreviewCandidate(in); err == nil {
		t.Fatal("audit contradiction accepted")
	}
}

func TestQualityPreviewV2RejectsContradictorySnapshotAudit(t *testing.T) {
	in := qualityPreviewExample(t)
	in.After.Audits = append([]store.AgentAuditLog{}, in.After.Audits...)
	in.Record.Result.Observations[0].State.Own.Audits[0].Args = `{"amount_money":300}`
	qualityRebind(t, &in)
	if _, err := qualityPreviewCandidate(in); err == nil {
		t.Fatal("raw snapshot contradicts complete audit rows")
	}
}
func TestQualityPreviewV2CoherentTraceFaults(t *testing.T) {
	for _, name := range []string{"failed", "unknown", "multiple", "actual-effect", "source-correct", "foreign", "raw-duplicate"} {
		t.Run(name, func(t *testing.T) {
			in := qualityPreviewExample(t)
			o := &in.Record.Result.Observations[0]
			switch name {
			case "failed":
				o.Turn.Response.Trace[0].Outcome = "failure"
			case "unknown":
				o.Turn.Response.Trace[0].Phase = "other"
			case "multiple":
				o.Turn.Response.Trace = append(o.Turn.Response.Trace, o.Turn.Response.Trace[0])
			case "actual-effect":
				o.State.Own.BalanceCent++
				o.State.Own.LedgerBalance++
			case "source-correct":
				in.Record.Result.Case.Scenario.Operation.Arguments["amount_money"] = float64(200)
			case "foreign":
				o.Turn.Response.Trace[0].Args = `{"amount_money":200,"tenant_id":20}`
			}
			qualityRebind(t, &in)
			if name == "raw-duplicate" {
				in.Record.Result.Observations[0].Turn.RawBody = `{"session_id":"session-19","session_id":"session-19"}`
				raw, _ := json.Marshal(in.Record.Result)
				in.Record.ResultSHA256 = conversationHash(raw)
			}
			if _, err := qualityPreviewCandidate(in); err == nil {
				t.Fatal("coherent unsafe trace accepted")
			}
		})
	}
}

func TestQualityPreviewV2RejectsDuplicateRawTraceKey(t *testing.T) {
	in := qualityPreviewExample(t)
	in.Record.Result.Observations[0].Turn.TraceRaw = json.RawMessage(`[{"tool":"recharge","tool":"recharge","args":"{\"amount_money\":200}","resolved_args":"{\"amount_money\":200}","confirmed":false,"preview":"充值200元","result":"等待确认","phase":"preview","outcome":"pending","duration_ms":0}]`)
	raw, _ := json.Marshal(in.Record.Result)
	in.Record.ResultSHA256 = conversationHash(raw)
	if _, err := qualityPreviewCandidate(in); err == nil {
		t.Fatal("duplicate trace JSON key accepted")
	}
}

func TestQualityPreviewV2RejectsAuthorityAndTimeUncertainty(t *testing.T) {
	for _, fault := range []string{"catalogue", "bills", "consumption", "nextday", "zero-time", "missing-authority", "authority-time"} {
		t.Run(fault, func(t *testing.T) {
			in := qualityPreviewExample(t)
			o := &in.Record.Result.Observations[0]
			switch fault {
			case "catalogue":
				o.State.AnswerAuthority.Plans = append(o.State.AnswerAuthority.Plans, store.Plan{ID: 999})
			case "bills":
				o.State.AnswerAuthority.BillsTotal++
			case "consumption":
				o.State.AnswerAuthority.ConsumptionDays++
			case "nextday":
				o.State.CapturedAt = o.State.CapturedAt.Add(24 * time.Hour)
				o.State.AnswerAuthority.CapturedAt = o.State.CapturedAt
			case "zero-time":
				o.State.CapturedAt = time.Time{}
				o.State.AnswerAuthority.CapturedAt = o.State.CapturedAt
			case "missing-authority":
				o.State.AnswerAuthority = nil
			case "authority-time":
				o.State.AnswerAuthority.CapturedAt = o.State.CapturedAt.Add(time.Second)
			}
			qualityRebind(t, &in)
			if _, err := qualityPreviewCandidate(in); err == nil {
				t.Fatal("changed/unknown authority or time accepted")
			}
		})
	}
}

func TestQualityPreviewV2SessionProjectionMatchesSQLCells(t *testing.T) {
	at := time.Date(2026, 10, 27, 12, 0, 0, 123000000, time.UTC)
	columns := []string{"id", "session_id", "tenant_id", "api_key_id", "created_at", "last_active_at"}
	rows := []store.AgentSession{{ID: 7, SessionID: "session-19", TenantID: 19, APIKeyID: 0, CreatedAt: at, LastActiveAt: at}}
	got, err := qualitySessionTableCapture(rows, columns)
	if err != nil {
		t.Fatalf("current SQL session row must project exactly: %v", err)
	}
	// Literal SQL NullString cell encoding, including numeric decimal text and
	// parseTime driver's RFC3339Nano representation, is independent of projector.
	raw := `[{"null":false,"bytes":"Nw=="},{"null":false,"bytes":"c2Vzc2lvbi0xOQ=="},{"null":false,"bytes":"MTk="},{"null":false,"bytes":"MA=="},{"null":false,"bytes":"MjAyNi0xMC0yN1QxMjowMDowMC4xMjNa"},{"null":false,"bytes":"MjAyNi0xMC0yN1QxMjowMDowMC4xMjNa"}]`
	if len(got.Rows) != 1 || got.Rows[0] != conversationHash([]byte(raw)) {
		t.Fatalf("session projection differs from raw SQL cells: %+v", got)
	}
}

func qualityRehashSessions(t *testing.T, in *qualityPreviewInput) {
	t.Helper()
	for _, capture := range []*qualityEffectCapture{&in.Before, &in.After} {
		projected, err := qualitySessionTableCapture(capture.Sessions, []string{"id", "session_id", "tenant_id", "api_key_id", "created_at", "last_active_at"})
		if err != nil {
			t.Fatal(err)
		}
		capture.Tables["agent_sessions"] = projected
	}
}
func TestQualityPreviewV2CurrentSessionLifecycle(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		in := qualityPreviewExample(t)
		if _, err := qualityPreviewCandidate(in); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("touch", func(t *testing.T) {
		in := qualityPreviewExample(t)
		row := in.After.Sessions[0]
		row.CreatedAt = row.CreatedAt.Add(-time.Hour)
		row.LastActiveAt = row.LastActiveAt.Add(-time.Minute)
		in.Before.Sessions = []store.AgentSession{row}
		in.After.Sessions[0].CreatedAt = row.CreatedAt
		in.BeforeSession = json.RawMessage(`{"history":[]}`)
		qualityRehashSessions(t, &in)
		if _, err := qualityPreviewCandidate(in); err != nil {
			t.Fatal(err)
		}
	})
	for _, fault := range []string{"tenant", "apikey", "other-row", "extra-row", "deleted-row", "created-change", "created-zone", "backwards-touch", "future", "early", "submillisecond", "missing-before", "unexpected-before", "hash-drift", "nil-rows", "columns"} {
		t.Run(fault, func(t *testing.T) {
			in := qualityPreviewExample(t)
			row := in.After.Sessions[0]
			row.CreatedAt = row.CreatedAt.Add(-time.Hour)
			row.LastActiveAt = row.LastActiveAt.Add(-time.Minute)
			in.Before.Sessions = []store.AgentSession{row}
			in.After.Sessions[0].CreatedAt = row.CreatedAt
			in.BeforeSession = json.RawMessage(`{"history":[]}`)
			switch fault {
			case "tenant":
				in.After.Sessions[0].TenantID = 20
			case "apikey":
				in.After.Sessions[0].APIKeyID = 88
			case "other-row":
				other := store.AgentSession{ID: 8, SessionID: "other", TenantID: 20, CreatedAt: row.CreatedAt, LastActiveAt: row.LastActiveAt}
				in.Before.Sessions = append(in.Before.Sessions, other)
				other.APIKeyID = 4
				in.After.Sessions = append(in.After.Sessions, other)
			case "extra-row":
				other := row
				other.ID = 8
				other.SessionID = "extra"
				in.After.Sessions = append(in.After.Sessions, other)
			case "deleted-row":
				other := row
				other.ID = 8
				other.SessionID = "other"
				in.Before.Sessions = append(in.Before.Sessions, other)
			case "created-change":
				in.After.Sessions[0].CreatedAt = in.After.Sessions[0].CreatedAt.Add(time.Second)
			case "created-zone":
				in.After.Sessions[0].CreatedAt = in.After.Sessions[0].CreatedAt.In(time.FixedZone("changed", 8*60*60))
			case "backwards-touch":
				in.After.Sessions[0].LastActiveAt = row.LastActiveAt.Add(-time.Second)
			case "future":
				in.After.Sessions[0].LastActiveAt = in.After.State.CapturedAt.Add(time.Millisecond)
			case "early":
				in.After.Sessions[0].LastActiveAt = in.Before.State.CapturedAt.Add(-time.Millisecond)
			case "submillisecond":
				in.After.Sessions[0].LastActiveAt = in.After.State.CapturedAt.Add(time.Nanosecond)
			case "missing-before":
				in.Before.Sessions = []store.AgentSession{}
			case "unexpected-before":
				in.BeforeSession = json.RawMessage(`{}`)
			case "nil-rows":
				in.Before.Sessions = nil
			}
			if fault != "nil-rows" {
				qualityRehashSessions(t, &in)
			}
			if fault == "hash-drift" {
				v := in.After.Tables["agent_sessions"]
				v.Rows = []string{}
				in.After.Tables["agent_sessions"] = v
			}
			if fault == "columns" {
				v := in.After.Tables["agent_sessions"]
				v.Columns = []string{"id"}
				in.After.Tables["agent_sessions"] = v
			}
			if _, err := qualityPreviewCandidate(in); err == nil {
				t.Fatal("session lifecycle violation accepted")
			}
		})
	}
}

func TestQualityPreviewV2KeepsStrictPrivateMoneyNumber(t *testing.T) {
	in := qualityPreviewExample(t)
	text := `{"amount_money":9007199254740992}`
	o := &in.Record.Result.Observations[0]
	o.Turn.Response.Trace[0].Args = text
	o.Turn.Response.Trace[0].ResolvedArgs = text
	o.Pending.Args = map[string]any{"amount_money": float64(9007199254740992)}
	in.Pending.Pending.Args = map[string]any{"amount_money": json.Number("9007199254740992")}
	in.Pending.Raw = json.RawMessage(`{"history":[],"pending_action":{"tool":"recharge","args":{"amount_money":9007199254740992},"preview":"充值200元","idem_key":"recharge-id","tenant_id":19}}`)
	o.State.Own.Audits[0].Args = text
	qualityRebind(t, &in)
	got, err := qualityPreviewCandidate(in)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := got.Evidence.Pending.Pending.Args["amount_money"].(json.Number)
	if !ok || n.String() != "9007199254740992" {
		t.Fatalf("strict private integer lost to float: %T %v", got.Evidence.Pending.Pending.Args["amount_money"], n)
	}
	if err := verifyQualityPreviewSidecar(got); err != nil {
		t.Fatal(err)
	}
}

func TestQualityPreviewV2AbsentSessionOnlyAtFirstTurn(t *testing.T) {
	in := qualityPreviewExample(t)
	in.Turn = 2
	if qualitySessionEffects(in) == nil {
		t.Fatal("absent Redis/SQL session accepted after prior conversation turn")
	}
}
