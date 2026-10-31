package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// These private captures are data, never an owned proof or acquisition authority.
type qualityPendingCapture struct {
	Pending   Pending         `json:"pending"`
	Preview   string          `json:"preview"`
	IdemKey   string          `json:"idem_key"`
	SessionID string          `json:"session_id"`
	Raw       json.RawMessage `json:"raw"`
}
type qualityTableCapture struct {
	Columns []string `json:"columns"`
	Rows    []string `json:"rows"`
}
type qualityEffectCapture struct {
	State    ConversationState              `json:"state"`
	Tables   map[string]qualityTableCapture `json:"tables"`
	Audits   []store.AgentAuditLog          `json:"audits"`
	Sessions []store.AgentSession           `json:"sessions"`
}
type qualityPreviewInput struct {
	Plans         []store.Plan           `json:"plans"`
	Record        collectionRecording    `json:"record"`
	Turn          int                    `json:"turn"`
	BeforeSession json.RawMessage        `json:"before_session"`
	Pending       *qualityPendingCapture `json:"pending"`
	Before        qualityEffectCapture   `json:"before"`
	After         qualityEffectCapture   `json:"after"`
}
type qualityUnrunTurn struct {
	Turn       int              `json:"turn"`
	Rule       ConversationRule `json:"rule"`
	RuleSHA256 string           `json:"rule_sha256"`
	Reason     string           `json:"reason"`
}
type qualityPreviewSidecar struct {
	PolicyID        string              `json:"policy_id"`
	PolicySHA256    string              `json:"policy_sha256"`
	SourceSHA256    string              `json:"source_sha256"`
	RuleSHA256      string              `json:"rule_sha256"`
	Disposition     string              `json:"disposition"`
	CompletionProof string              `json:"completion_proof"`
	AuditOnly       bool                `json:"audit_only"`
	Evidence        qualityPreviewInput `json:"evidence"`
	Unrun           []qualityUnrunTurn  `json:"unrun"`
}

const qualityPreviewPolicyID = "full-customer-owned-preview-candidate.v2"

func qualityPreviewTableNames() []string {
	return []string{"tenants", "api_keys", "providers", "models", "bills", "plans", "subscriptions", "recharge_orders", "topups", "agent_sessions", "outbox_events", "audit_events", "admin_users", "admin_sessions", "portal_users", "portal_sessions"}
}

// This identity describes ONLY the pure evidence schema/rules, not live cleanup
// or acquisition. A future owned policy must bind its own complete contract.
func qualityPreviewPolicySHA() string {
	raw, _ := json.Marshal(struct {
		ID, V1, Rules string
		Tables        []string
	}{qualityPreviewPolicyID, collectionPolicySHA(), "full-v1-replay;current-single-pending-preview;strict-args;raw-session-response-trace;known-own-five-tools;whole-sql-rows-except-bound-current-session-lifecycle-ms;authority-same-day;all-audit-exact-delta;source-mismatch;exact-source-suffix;precleanup-audit-only", qualityPreviewTableNames()})
	return conversationHash(raw)
}

func qualityPreviewCandidate(in qualityPreviewInput) (qualityPreviewSidecar, error) {
	fail := func(reason string) (qualityPreviewSidecar, error) {
		return qualityPreviewSidecar{}, fmt.Errorf("preview candidate rejected: %s", reason)
	}
	r := in.Record.Result
	if err := verifyCollectionRecording(in.Record); err != nil {
		return fail("v1 assessment")
	}
	if in.Record.Disposition != "halt" || r.Status != "failed" || r.Success || r.StructuralPass || in.Turn < 1 || in.Turn != len(r.Observations) || in.Turn > len(r.Case.Scenario.Turns) {
		return fail("not an original v1 stopped turn")
	}
	origin := false
	for _, issue := range in.Record.Issues {
		if issue.Disposition != "halt" {
			continue
		}
		if issue.Turn != in.Turn {
			return fail("earlier halt")
		}
		switch issue.Code {
		case "preview_arguments", "pending_identity", "unexpected_pending", "unauthorized_tool", "pending_not_cleared":
			origin = true
		case "incomplete":
		default:
			return fail("unrelated v1 halt")
		}
	}
	if !origin {
		return fail("missing preview-derived v1 stop")
	}
	obs := r.Observations[in.Turn-1]
	before := r.Initial
	if in.Turn > 1 {
		before = r.Observations[in.Turn-2].State
	}
	if !canonicalEqual(before, in.Before.State) || !canonicalEqual(obs.State, in.After.State) || !qualitySameEffectState(in.Before.State, in.After.State, r.Fixture) || !canonicalEqual(in.Before.State.Controls, in.After.State.Controls) || !qualitySameEffectState(obs.State, r.Final, r.Fixture) {
		return fail("state linkage or effects")
	}
	if err := validateConversationState(in.Before.State, r.Fixture); err != nil {
		return fail("before state")
	}
	if err := validateConversationState(in.After.State, r.Fixture); err != nil {
		return fail("after state")
	}
	if err := qualityNoTableEffects(in.Before.Tables, in.After.Tables); err != nil {
		return fail("complete SQL footprint")
	}
	if obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" || obs.Action != "preview" {
		return fail("transport/action")
	}
	tm := obs.Turn.Response.Telemetry
	if tm == nil || tm.Runtime == nil || tm.LangfuseTraceID == "" || tm.LLMCalls < 0 || tm.JevCalls < 0 || tm.LLMUsageMissing != 0 || tm.JevUsageMissing != 0 || tm.JevErrors != 0 || tm.LLMUsageResponses != tm.LLMCalls || tm.JevUsageResponses != tm.JevCalls {
		return fail("runtime/usage unavailable")
	}
	var response Response
	if _, err := windowJSONObject([]byte(obs.Turn.RawBody)); err != nil {
		return fail("raw response")
	}
	if json.Unmarshal([]byte(obs.Turn.RawBody), &response) != nil || !reflect.DeepEqual(response, obs.Turn.Response) {
		return fail("raw response differs")
	}
	if _, err := windowJSONObject(append(append([]byte(`{"trace":`), obs.Turn.TraceRaw...), '}')); err != nil {
		return fail("strict raw trace")
	}
	var traces []Trace
	if json.Unmarshal(obs.Turn.TraceRaw, &traces) != nil || !reflect.DeepEqual(traces, response.Trace) {
		return fail("raw trace differs")
	}
	previous, err := windowJSONObject(in.BeforeSession)
	if err != nil || windowOnlyKeys(previous, "history", "pending_action") != nil || previous["pending_action"] != nil {
		return fail("previous pending unavailable or present")
	}
	if err := qualitySessionEffects(in); err != nil {
		return fail("session bookkeeping")
	}
	p := in.Pending
	if p == nil || p.SessionID == "" || p.SessionID != response.SessionID || p.IdemKey == "" || p.Preview == "" || p.Pending.TenantID != r.Fixture.TenantID || !response.Pending || response.PendingTool != p.Pending.Tool || response.Preview != p.Preview || obs.Pending == nil || !qualityJSONEqual(obs.Pending, p.Pending) {
		return fail("pending identity")
	}
	session, err := windowJSONObject(p.Raw)
	if err != nil || windowOnlyKeys(session, "history", "pending_action") != nil {
		return fail("raw session")
	}
	rawPending, ok := session["pending_action"].(map[string]any)
	if !ok || windowOnlyKeys(rawPending, "tool", "args", "preview", "idem_key", "tenant_id") != nil {
		return fail("raw pending schema")
	}
	wantPending := map[string]any{"tool": p.Pending.Tool, "args": p.Pending.Args, "preview": p.Preview, "idem_key": p.IdemKey, "tenant_id": p.Pending.TenantID}
	if !qualityJSONEqual(rawPending, wantPending) {
		return fail("raw pending differs")
	}
	previews := 0
	for _, tr := range response.Trace {
		if tr.Confirmed {
			return fail("confirmed trace")
		}
		args, err := windowJSONObject([]byte(tr.Args))
		if err != nil {
			return fail("raw trace arguments")
		}
		resolved, err := windowJSONObject([]byte(tr.ResolvedArgs))
		if err != nil {
			return fail("resolved trace arguments")
		}
		for _, a := range []map[string]any{args, resolved} {
			if tid, exists := a["tenant_id"]; exists {
				n, e := windowNonnegativeInteger(tid)
				if e != nil || uint64(n) != r.Fixture.TenantID {
					return fail("foreign trace")
				}
			}
		}
		if tr.Phase == "read" && tr.Outcome == "success" && !isConversationWrite(tr.Tool) && collectionWrongToolCode(tr) == "wrong_read_tool" {
			continue
		}
		if !isConversationWrite(tr.Tool) || tr.Phase != "preview" || tr.Outcome != "pending" || tr.Result != "等待确认" || tr.Preview != p.Preview || tr.Tool != p.Pending.Tool || !qualityJSONEqual(resolved, p.Pending.Args) {
			return fail("not a current successful preview")
		}
		previews++
		if err := qualityPreviewTarget(tr.Tool, args, resolved, r.Fixture, in.Before.State, in.Plans); err != nil {
			return fail("unknown target or arguments")
		}
	}
	if previews != 1 {
		return fail("not exactly one preview")
	}
	rule := r.Case.Scenario.Turns[in.Turn-1]
	mismatch := !conversationPendingCorrect(r.Case, r.Fixture, &p.Pending) || (!rule.PendingRequired && !rule.PendingOptional) || !hasString(rule.AllowedTools, p.Pending.Tool) || rule.BoundPendingRequired || rule.PendingCleared || rule.NoSecondExecution || rule.NoExecutionWithoutNewPreview
	if !mismatch {
		return fail("source-correct preview")
	}
	if !qualityAuditProjection(in.Before.State, in.Before.Audits, r.Fixture.TenantID) || !qualityAuditProjection(in.After.State, in.After.Audits, r.Fixture.TenantID) || !qualityAuditProjection(r.Final, in.After.Audits, r.Fixture.TenantID) {
		return fail("audit snapshot linkage")
	}
	if err := qualityAuditDelta(in.Before.Audits, in.After.Audits, response.Trace, r.Fixture.TenantID, response.SessionID); err != nil {
		return fail("audit completeness")
	}
	out := qualityPreviewSidecar{PolicyID: qualityPreviewPolicyID, PolicySHA256: qualityPreviewPolicySHA(), SourceSHA256: qualityJSONSHA(r.Case), RuleSHA256: qualityJSONSHA(rule), Disposition: "candidate_precleanup", CompletionProof: "unavailable_from_recording", AuditOnly: true, Evidence: in, Unrun: []qualityUnrunTurn{}}
	for i := in.Turn; i < len(r.Case.Scenario.Turns); i++ {
		rule := r.Case.Scenario.Turns[i]
		out.Unrun = append(out.Unrun, qualityUnrunTurn{Turn: i + 1, Rule: rule, RuleSHA256: qualityJSONSHA(rule), Reason: "quality_end_candidate_unrun"})
	}
	// Detach all caller-owned maps/slices. No pointer seal or runtime authority is created.
	raw, err := json.Marshal(out)
	if err != nil {
		return fail("serialization")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&out) != nil {
		return fail("serialization")
	}
	// v1 is kept in its original decoder contract; strict private argument maps
	// retain json.Number. Refuse any legacy precision loss instead of rebinding it.
	recordRaw, e := json.Marshal(in.Record)
	if e != nil || json.Unmarshal(recordRaw, &out.Evidence.Record) != nil || qualityJSONSHA(out.Evidence.Record.Result) != in.Record.ResultSHA256 {
		return fail("v1 clone precision")
	}

	return out, nil
}

func verifyQualityPreviewSidecar(sidecar qualityPreviewSidecar) error {
	expected, err := qualityPreviewCandidate(sidecar.Evidence)
	if err != nil {
		return err
	}
	if !qualityJSONEqual(expected, sidecar) {
		return fmt.Errorf("preview sidecar binding differs")
	}
	return nil
}
func qualityJSONSHA(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return conversationHash(raw)
}
func qualityJSONEqual(a, b any) bool {
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	return ex == nil && ey == nil && string(x) == string(y)
}
func qualityNoTableEffects(before, after map[string]qualityTableCapture) error {
	names := qualityPreviewTableNames()
	if len(before) != len(names) || len(after) != len(names) {
		return fmt.Errorf("table set incomplete")
	}
	for _, name := range names {
		a, ok := before[name]
		b, exists := after[name]
		if !ok || !exists || len(a.Columns) == 0 || a.Rows == nil || len(b.Columns) == 0 || b.Rows == nil || (name != "agent_sessions" && !reflect.DeepEqual(a, b)) || (name == "agent_sessions" && !reflect.DeepEqual(a.Columns, b.Columns)) {
			return fmt.Errorf("table unavailable or changed")
		}
		seen := map[string]bool{}
		for _, col := range a.Columns {
			if col == "" || seen[col] {
				return fmt.Errorf("columns unavailable")
			}
			seen[col] = true
		}
		if !sort.StringsAreSorted(a.Rows) {
			return fmt.Errorf("row hashes not sorted")
		}
		for _, row := range a.Rows {
			if !comparisonDigest(row) {
				return fmt.Errorf("row hash invalid")
			}
		}
	}
	return nil
}

func qualityPreviewTarget(tool string, raw, resolved map[string]any, f Fixture, before ConversationState, plans []store.Plan) error {
	// Input objects were decoded with strict duplicate-key rejection and UseNumber.
	stripTenant := func(m map[string]any) map[string]any {
		v := map[string]any{}
		for k, x := range m {
			if k != "tenant_id" {
				v[k] = x
			}
		}
		return v
	}
	raw, resolved = stripTenant(raw), stripTenant(resolved)
	money := func(m map[string]any) bool {
		n, e := windowNonnegativeInteger(m["amount_money"])
		return e == nil && n > 0 && n <= math.MaxInt64/100 && len(m) == 1
	}
	switch tool {
	case "recharge":
		if !money(raw) || !money(resolved) || !qualityJSONEqual(raw, resolved) {
			return fmt.Errorf("money")
		}
	case "refund_recharge":
		if before.Own.BalanceCent <= 0 || !qualityJSONEqual(raw, resolved) {
			return fmt.Errorf("refund")
		}
		if _, ok := raw["amount_money"]; ok {
			if !money(raw) {
				return fmt.Errorf("refund money")
			}
			return nil
		}
		order, ok := raw["order_no"].(string)
		if !ok || order == "" || len(raw) != 1 {
			return fmt.Errorf("order")
		}
		matches := 0
		for _, o := range before.Own.Orders {
			if o.OrderNo == order {
				if o.TenantID != f.TenantID || o.Status != store.RechargePaid || o.AmountMoney <= 0 || o.AmountMoney > math.MaxInt64/100 || o.AmountCent != o.AmountMoney*100 {
					return fmt.Errorf("order authority")
				}
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("order unknown")
		}
	case "cancel_subscription":
		active := 0
		for _, sub := range before.Own.Subscriptions {
			if sub.TenantID == f.TenantID && sub.Status == store.SubActive && sub.PlanType == store.PlanTypeRecurring {
				active++
			}
		}
		if active != 1 {
			return fmt.Errorf("cancel target unavailable")
		}
		if len(raw) != 0 || len(resolved) != 0 {
			return fmt.Errorf("cancel arguments")
		}
	case "subscribe_plan", "change_plan":
		if plans == nil || windowOnlyKeys(raw, "plan_id", "plan_name") != nil || windowOnlyKeys(resolved, "plan_id", "plan_name") != nil || len(resolved) != 2 {
			return fmt.Errorf("plan authority")
		}
		id, e := windowNonnegativeInteger(resolved["plan_id"])
		name, ok := resolved["plan_name"].(string)
		if e != nil || id <= 0 || !ok || name == "" {
			return fmt.Errorf("resolved plan")
		}
		selected := 0
		for _, p := range plans {
			if p.ID == uint64(id) {
				if p.Status != 0 || p.PlanType != store.PlanTypeRecurring || p.Name != name {
					return fmt.Errorf("plan unavailable")
				}
				selected++
			}
		}
		if selected != 1 {
			return fmt.Errorf("plan ambiguous")
		}
		if supplied, exists := raw["plan_id"]; exists {
			n, e := windowNonnegativeInteger(supplied)
			if e != nil || n != id {
				return fmt.Errorf("plan id")
			}
		}
		if supplied, exists := raw["plan_name"]; exists {
			n, ok := supplied.(string)
			if !ok || qualityResolvePlanName(n, plans) != uint64(id) {
				return fmt.Errorf("plan name")
			}
		} else if raw["plan_id"] == nil {
			return fmt.Errorf("plan target absent")
		}
	default:
		return fmt.Errorf("unsupported tool")
	}
	return nil
}
func qualityResolvePlanName(name string, plans []store.Plan) uint64 {
	normalize := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '·' {
				return -1
			}
			return r
		}, s)
	}
	name = normalize(name)
	if name == "" {
		return 0
	}
	match := func(needle string, exact bool) []uint64 {
		ids := []uint64{}
		for _, p := range plans {
			n := normalize(p.Name)
			if p.Status == 0 && p.PlanType == store.PlanTypeRecurring && ((exact && n == needle) || (!exact && strings.Contains(n, needle))) {
				ids = append(ids, p.ID)
			}
		}
		return ids
	}
	ids := match(name, true)
	if len(ids) == 0 {
		ids = match(name, false)
	}
	if len(ids) == 0 {
		alias := name
		for _, suffix := range []string{"套餐", "档位", "档"} {
			alias = strings.TrimSuffix(alias, suffix)
		}
		if alias != "" && alias != name {
			ids = match(alias, false)
		}
	}
	if len(ids) != 1 {
		return 0
	}
	return ids[0]
}
func qualityAuditDelta(before, after []store.AgentAuditLog, traces []Trace, tenant uint64, session string) error {
	if before == nil || after == nil {
		return fmt.Errorf("audit query unavailable")
	}
	old := map[uint64]store.AgentAuditLog{}
	for _, row := range before {
		if row.ID == 0 {
			return fmt.Errorf("audit id")
		}
		if _, ok := old[row.ID]; ok {
			return fmt.Errorf("duplicate audit")
		}
		old[row.ID] = row
	}
	seen := map[uint64]bool{}
	added := []store.AgentAuditLog{}
	for _, row := range after {
		if row.ID == 0 || seen[row.ID] {
			return fmt.Errorf("audit id")
		}
		seen[row.ID] = true
		if prior, ok := old[row.ID]; ok {
			if !reflect.DeepEqual(prior, row) {
				return fmt.Errorf("old audit changed")
			}
		} else {
			added = append(added, row)
		}
	}
	for id := range old {
		if !seen[id] {
			return fmt.Errorf("old audit missing")
		}
	}
	if len(added) != len(traces) {
		return fmt.Errorf("audit delta size")
	}
	for _, tr := range traces {
		found := -1
		for i, row := range added {
			args, err := windowJSONObject([]byte(row.Args))
			if err != nil {
				continue
			}
			resolved, err := windowJSONObject([]byte(tr.ResolvedArgs))
			if err != nil {
				continue
			}
			result := tr.Result
			if tr.Phase == "preview" {
				result = "pending_confirmation"
			}
			if row.TenantID == tenant && row.SessionID == session && row.Tool == tr.Tool && !row.Confirmed && row.Preview == tr.Preview && row.Result == result && row.Guard == "" && qualityJSONEqual(args, resolved) {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("trace audit absent")
		}
		added = append(added[:found], added[found+1:]...)
	}
	return nil
}

// Public snapshots contain tenant-specific audit projections; the complete
// private query must agree with every retained own/control row as well.
func qualityAuditProjection(state ConversationState, rows []store.AgentAuditLog, own uint64) bool {
	check := func(tenant uint64, snapshot Snapshot) bool {
		actual := map[uint64]store.AgentAuditLog{}
		for _, row := range rows {
			if row.TenantID == tenant {
				if _, exists := actual[row.ID]; exists {
					return false
				}
				actual[row.ID] = row
			}
		}
		if len(actual) != len(snapshot.Audits) {
			return false
		}
		seen := map[uint64]bool{}
		for _, row := range snapshot.Audits {
			if seen[row.ID] || !canonicalEqual(actual[row.ID], row) {
				return false
			}
			seen[row.ID] = true
		}
		return true
	}
	if !check(own, state.Own) {
		return false
	}
	for tid, snapshot := range state.Controls {
		if !check(tid, snapshot) {
			return false
		}
	}
	return true
}

// CapturedAt annotates observations rather than business state. Every other
// authority value (including catalogue, bills, consumption and quota) is exact;
// no cross-day comparison is certified by this initial policy.
func qualitySameEffectState(a, b ConversationState, f Fixture) bool {
	if a.CapturedAt.IsZero() || b.CapturedAt.IsZero() || a.AnswerAuthority == nil || b.AnswerAuthority == nil || !a.AnswerAuthority.CapturedAt.Equal(a.CapturedAt) || !b.AnswerAuthority.CapturedAt.Equal(b.CapturedAt) || a.CapturedAt.Format("2006-01-02") != b.CapturedAt.Format("2006-01-02") {
		return false
	}
	_, za := a.CapturedAt.Zone()
	_, zb := b.CapturedAt.Zone()
	if za != zb {
		return false
	}
	if validateAnswerAuthority(a, f) != nil || validateAnswerAuthority(b, f) != nil || !a.AnswerAuthority.CatalogComplete || !b.AnswerAuthority.CatalogComplete || !a.AnswerAuthority.ConsumptionComplete || !b.AnswerAuthority.ConsumptionComplete {
		return false
	}
	aa, bb := *a.AnswerAuthority, *b.AnswerAuthority
	aa.CapturedAt = time.Time{}
	bb.CapturedAt = time.Time{}
	a.AnswerAuthority = &aa
	b.AnswerAuthority = &bb
	a.CapturedAt = time.Time{}
	b.CapturedAt = time.Time{}
	a.Own.Audits = nil
	b.Own.Audits = nil
	return canonicalEqual(a, b)
}

func qualitySessionTableCapture(rows []store.AgentSession, columns []string) (qualityTableCapture, error) {
	expected := []string{"id", "session_id", "tenant_id", "api_key_id", "created_at", "last_active_at"}
	if rows == nil || !reflect.DeepEqual(columns, expected) {
		return qualityTableCapture{}, fmt.Errorf("session table projection unavailable")
	}
	out := qualityTableCapture{Columns: append([]string(nil), columns...), Rows: []string{}}
	ids := map[uint64]bool{}
	sids := map[string]bool{}
	for _, row := range rows {
		if row.ID == 0 || row.SessionID == "" || row.TenantID == 0 || ids[row.ID] || sids[row.SessionID] || row.CreatedAt.IsZero() || row.LastActiveAt.IsZero() || row.LastActiveAt.Before(row.CreatedAt) {
			return qualityTableCapture{}, fmt.Errorf("invalid session row")
		}
		ids[row.ID] = true
		sids[row.SessionID] = true
		type cell struct {
			Null  bool   `json:"null"`
			Bytes []byte `json:"bytes"`
		}
		values := []string{strconv.FormatUint(row.ID, 10), row.SessionID, strconv.FormatUint(row.TenantID, 10), strconv.FormatUint(row.APIKeyID, 10), row.CreatedAt.Format(time.RFC3339Nano), row.LastActiveAt.Format(time.RFC3339Nano)}
		cells := make([]cell, len(values))
		for i, value := range values {
			cells[i] = cell{Bytes: []byte(value)}
		}
		raw, err := json.Marshal(cells)
		if err != nil {
			return qualityTableCapture{}, err
		}
		out.Rows = append(out.Rows, conversationHash(raw))
	}
	sort.Strings(out.Rows)
	return out, nil
}

// RecordSession bookkeeping is the only permitted SQL change. Its actual typed
// rows must project to the full raw SQL hashes before lifecycle validation.
func qualitySessionEffects(in qualityPreviewInput) error {
	fail := func() error { return fmt.Errorf("current session bookkeeping differs") }
	for _, capture := range []qualityEffectCapture{in.Before, in.After} {
		table, ok := capture.Tables["agent_sessions"]
		if !ok {
			return fail()
		}
		projected, err := qualitySessionTableCapture(capture.Sessions, table.Columns)
		if err != nil || !reflect.DeepEqual(projected, table) {
			return fail()
		}
	}
	if in.Pending == nil || in.Pending.SessionID == "" {
		return fail()
	}
	sid := in.Pending.SessionID
	tenant := in.Record.Result.Fixture.TenantID
	before := map[uint64]store.AgentSession{}
	after := map[uint64]store.AgentSession{}
	var old, current *store.AgentSession
	for i := range in.Before.Sessions {
		row := in.Before.Sessions[i]
		before[row.ID] = row
		if row.SessionID == sid {
			copy := row
			old = &copy
		}
	}
	for i := range in.After.Sessions {
		row := in.After.Sessions[i]
		after[row.ID] = row
		if row.SessionID == sid {
			copy := row
			current = &copy
		}
	}
	if current == nil || current.TenantID != tenant || current.APIKeyID != 0 {
		return fail()
	}
	object, err := windowJSONObject(in.BeforeSession)
	if err != nil {
		return fail()
	}
	absent := len(object) == 0
	if absent != (old == nil) || (absent && in.Turn != 1) {
		return fail()
	}
	// SQL datetime(3) quantization is bounded to one storage quantum, never a
	// request-time allowance. Bounds themselves still belong to this turn.
	lower, upper := in.Before.State.CapturedAt, in.After.State.CapturedAt
	if lower.IsZero() || upper.IsZero() || upper.Before(lower) {
		return fail()
	}
	lower = lower.Truncate(time.Millisecond)
	if upper.Nanosecond()%int(time.Millisecond) != 0 {
		upper = upper.Truncate(time.Millisecond).Add(time.Millisecond)
	}
	within := func(at time.Time) bool {
		return at.Nanosecond()%int(time.Millisecond) == 0 && !at.Before(lower) && !at.After(upper)
	}
	if !within(current.LastActiveAt) {
		return fail()
	}
	if old == nil {
		if len(after) != len(before)+1 || current.CreatedAt.Format(time.RFC3339Nano) != current.LastActiveAt.Format(time.RFC3339Nano) || !within(current.CreatedAt) {
			return fail()
		}
	} else {
		if len(after) != len(before) || old.TenantID != tenant || old.APIKeyID != 0 || current.ID != old.ID || current.SessionID != old.SessionID || current.CreatedAt.Format(time.RFC3339Nano) != old.CreatedAt.Format(time.RFC3339Nano) || current.LastActiveAt.Before(old.LastActiveAt) {
			return fail()
		}
	}
	for id, row := range before {
		if old != nil && id == old.ID {
			continue
		}
		actual, exists := after[id]
		if !exists || !reflect.DeepEqual(row, actual) {
			return fail()
		}
	}
	return nil
}
