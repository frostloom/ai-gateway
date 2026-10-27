package taskbench

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"strings"
	"testing"
)

func collectionUnmetDriver() *collectionTestDriver {
	d := newCollectionTestDriver()
	for i := range d.steps {
		d.steps[i] = ConversationObservation{Action: "unreviewed", State: d.initial, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "尚未执行，没有生成预览。"}}}
	}
	return d
}

func TestCollectionPolicyRetainsSafeUnmetGoal(t *testing.T) {
	d := collectionUnmetDriver()
	got := recordCollectionConversation(context.Background(), rechargeConversation(), d)
	if got.Disposition != "continue_recorded" || got.Result.Status != "failed" || got.Result.StructuralPass || got.Result.Success || len(d.sent) != 3 {
		t.Fatalf("safe task negative not retained: %+v", got)
	}
	if len(got.Issues) != len(got.Result.Errors) || len(got.Issues) == 0 {
		t.Fatal("errors lack origin issues")
	}
	for _, issue := range got.Issues {
		if issue.Disposition != "continue_recorded" || issue.Code == "" {
			t.Fatal(issue)
		}
	}
	if got.PolicySHA256 == "" || got.ResultSHA256 == "" {
		t.Fatal("policy/result not bound")
	}
}

func TestCollectionPolicyHaltsBeforeWrongPendingConfirmation(t *testing.T) {
	d := newCollectionTestDriver()
	d.steps[1].Pending.Args["amount_money"] = float64(200)
	got := recordCollectionConversation(context.Background(), rechargeConversation(), d)
	if got.Disposition != "halt" || len(d.sent) != 2 || got.Result.Success {
		t.Fatalf("unsafe preview reached confirmation: %+v sent%v", got, d.sent)
	}
	if !sameConversationBusiness(got.Result.Initial, got.Result.Final) {
		t.Fatal("test driver supplied unexecuted future state")
	}
}

func TestCollectionPolicyDetectsUntracedMutation(t *testing.T) {
	d := collectionUnmetDriver()
	bad := cloneConversationState(d.initial)
	bad.Own.BalanceCent += 10000
	bad.Own.LedgerBalance += 10000
	d.steps[2].State = bad
	got := recordCollectionConversation(context.Background(), rechargeConversation(), d)
	if got.Disposition != "halt" {
		t.Fatal("hidden write classified as missing goal", got)
	}
}

func TestCollectionPolicyPositiveAndLegacyJSON(t *testing.T) {
	got := recordCollectionConversation(context.Background(), rechargeConversation(), newCollectionTestDriver())
	if got.Disposition != "continue_recorded" || got.Result.Status != "needs_review" || !got.Result.StructuralPass || got.Result.Success || len(got.Issues) != 0 {
		t.Fatalf("positive recording changed: %+v", got)
	}
	old := RunConversation(context.Background(), rechargeConversation(), newCollectionTestDriver())
	raw, err := json.Marshal(old)
	if err != nil || strings.Contains(string(raw), "policy_sha256") || strings.Contains(string(raw), "disposition") || strings.Contains(string(raw), "issues") {
		t.Fatal("legacy JSON changed")
	}
}

func TestCollectionPolicyUnknownEvidenceStopsImmediately(t *testing.T) {
	for _, fault := range []string{"action", "phase", "outcome"} {
		t.Run(fault, func(t *testing.T) {
			c := rechargeConversation()
			d := newCollectionTestDriver()
			if fault == "action" {
				d.steps[0].Action = "teleport"
			} else {
				c.Scenario.Turns[0].ExpectedActions = []string{"read"}
				c.Scenario.Turns[0].AllowedTools = []string{"get_balance"}
				tr := Trace{Tool: "get_balance", Phase: "read", Outcome: "success", ResolvedArgs: `{"tenant_id":19}`}
				if fault == "phase" {
					tr.Phase = "unknown-phase"
				} else {
					tr.Outcome = "unknown-outcome"
				}
				d.steps[0].Action = "read"
				d.steps[0].Turn.Response.Trace = []Trace{tr}
			}
			got := recordCollectionConversation(context.Background(), c, d)
			if got.Disposition != "halt" || len(d.sent) != 1 {
				t.Fatalf("unknown evidence continued: %s %+v", fault, got)
			}
		})
	}
}

func TestCollectionPolicyWrongActualAmountStopsBeforeLaterTurn(t *testing.T) {
	c := rechargeConversation()
	d := newCollectionTestDriver()
	bad := cloneConversationState(d.steps[2].State)
	bad.Own.BalanceCent += 10000
	bad.Own.LedgerBalance += 10000
	bad.Own.Orders[0].AmountCent = 20000
	bad.Own.Orders[0].AmountMoney = 200
	bad.Own.Topups[0].Amount = 20000
	d.steps[2].State = bad
	c.Scenario.Turns = append(c.Scenario.Turns, ConversationRule{User: "继续", ExpectedActions: []string{"respond"}, BusinessStateUnchanged: true})
	d.steps = append(d.steps, ConversationObservation{Action: "unreviewed", State: bad, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "继续回复"}}})
	got := recordCollectionConversation(context.Background(), c, d)
	if got.Disposition != "halt" || len(d.sent) != 3 {
		t.Fatalf("wrong actual effects reached later turn: sent%d %+v", len(d.sent), got)
	}
}

func TestCollectionPolicyReviewNegativeIsDistinctFromInvalidEvidence(t *testing.T) {
	for _, kind := range []string{"fail", "stale", "duplicate", "unknown", "verdict"} {
		t.Run(kind, func(t *testing.T) {
			c, d := rechargeConversation(), newCollectionTestDriver()
			base := RunConversation(context.Background(), c, d)
			for _, req := range base.ReviewRequirements {
				d.steps[req.TurnIndex].Reviews = append(d.steps[req.TurnIndex].Reviews, ConversationReview{ID: req.ID, RequirementSHA256: req.RequirementSHA256, ReplySHA256: req.ReplySHA256, Verdict: "pass", Reviewer: "local-regression", Method: "local_stub", Reason: "synthetic review fixture"})
			}
			switch kind {
			case "fail":
				d.steps[0].Reviews[0].Verdict = "fail"
			case "stale":
				d.steps[0].Reviews[0].ReplySHA256 = strings.Repeat("f", 64)
			case "duplicate":
				d.steps[0].Reviews = append(d.steps[0].Reviews, d.steps[0].Reviews[0])
			case "unknown":
				d.steps[0].Reviews = append(d.steps[0].Reviews, ConversationReview{ID: "unknown"})
			case "verdict":
				d.steps[0].Reviews[0].Verdict = "unknown-verdict"
			}
			got := recordCollectionConversation(context.Background(), c, d)
			want := "halt"
			if kind == "fail" {
				want = "continue_recorded"
			}
			if got.Disposition != want || got.Result.Success || got.Result.Status != "failed" {
				t.Fatalf("review class %s wrong disposition %s", kind, got.Disposition)
			}
			if len(got.Result.Errors) != len(got.Issues) {
				t.Fatal("untyped review error")
			}
			for _, issue := range got.Issues {
				if issue.Turn != 1 {
					t.Fatal("review issue attributed to wrong turn", issue.Turn)
				}
			}
		})
	}
}

func TestCollectionPolicyReplayRejectsAlteredAssessment(t *testing.T) {
	for _, negative := range []bool{false, true} {
		d := newCollectionTestDriver()
		if negative {
			d = collectionUnmetDriver()
		}
		original := recordCollectionConversation(context.Background(), rechargeConversation(), d)
		if err := verifyCollectionRecording(original); err != nil {
			t.Fatal("valid recording failed replay", err)
		}
		for _, mutation := range []string{"policy", "disposition", "result", "review", "issue"} {
			t.Run(mutation, func(t *testing.T) {
				raw, _ := json.Marshal(original)
				var bad collectionRecording
				json.Unmarshal(raw, &bad)
				switch mutation {
				case "policy":
					bad.PolicySHA256 = strings.Repeat("0", 64)
				case "disposition":
					bad.Disposition = "passed"
				case "result":
					bad.Result.Observations[0].Turn.Response.Reply = "changed answer"
					b, _ := json.Marshal(bad.Result)
					bad.ResultSHA256 = conversationHash(b)
				case "review":
					bad.Result.ReviewRequirements[0].ReplySHA256 = strings.Repeat("f", 64)
					b, _ := json.Marshal(bad.Result)
					bad.ResultSHA256 = conversationHash(b)
				case "issue":
					bad.Issues = append(bad.Issues, collectionIssue{Code: "unknown", Disposition: "continue_recorded"})
				}
				if verifyCollectionRecording(bad) == nil {
					t.Fatal("altered recording accepted", mutation)
				}
			})
		}
	}
}

type collectionTestDriver struct{ *scriptedConversation }

func newCollectionTestDriver() *collectionTestDriver { return &collectionTestDriver{rechargeDriver()} }
func (d *collectionTestDriver) Final(context.Context, Fixture, string) (ConversationState, error) {
	if len(d.sent) == 0 {
		return d.initial, nil
	}
	return d.steps[len(d.sent)-1].State, nil
}

func TestCollectionPolicySafetyFaultsStopAtObservedTurn(t *testing.T) {
	for _, fault := range []string{"early-write", "foreign-control", "wrong-pending", "duplicate-write", "http", "missing-control", "ledger", "unknown-terminal", "wrong-tool", "session", "preview-failure", "foreign-args", "hidden-pending", "missing-authoritative-pending", "allowance"} {
		t.Run(fault, func(t *testing.T) {
			c, d := rechargeConversation(), newCollectionTestDriver()
			want := 3
			switch fault {
			case "early-write":
				d.steps[0] = d.steps[2]
				want = 1
			case "foreign-control":
				s := cloneConversationState(d.steps[0].State)
				v := s.Controls[20]
				v.BalanceCent++
				v.LedgerBalance++
				s.Controls[20] = v
				d.steps[0].State = s
				want = 1
			case "wrong-pending":
				d.steps[1].Pending.TenantID = 99
				want = 2
			case "duplicate-write":
				d.steps[2].Turn.Response.Trace = append(d.steps[2].Turn.Response.Trace, d.steps[2].Turn.Response.Trace[0])
			case "http":
				d.steps[0].Turn.Status = 503
				want = 1
			case "missing-control":
				d.initial.Controls = nil
				want = 0
			case "ledger":
				d.steps[0].State.Own.LedgerBalance = 12
				want = 1
			case "unknown-terminal":
				c.Scenario.Terminal["unknown-terminal-rule"] = true
			case "wrong-tool":
				d.steps[2].Turn.Response.Trace[0].Tool = "refund_recharge"
			case "session":
				d.steps[0].Turn.Response.SessionID = ""
				want = 1
			case "preview-failure":
				d.steps[1].Turn.Response.Trace[0].Outcome = "failure"
				want = 2
			case "foreign-args":
				d.steps[0].Turn.Response.Trace = []Trace{{Tool: "get_balance", Phase: "read", Outcome: "success", ResolvedArgs: `{"tenant_id":99}`}}
				want = 1
			case "hidden-pending":
				d.steps[0].Pending = d.steps[1].Pending
				want = 1
			case "missing-authoritative-pending":
				d.steps[1].Pending = nil
				want = 2
			case "allowance":
				d.steps[2].State.Allowance = []store.TierRemain{{Tier: "文本", Quota: 100, Remaining: 100}}
			}
			c.Scenario.Turns = append(c.Scenario.Turns, ConversationRule{User: "后续轮", ExpectedActions: []string{"respond"}, BusinessStateUnchanged: true})
			d.steps = append(d.steps, ConversationObservation{Action: "unreviewed", State: d.steps[2].State, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "后续轮不应执行"}}})
			got := recordCollectionConversation(context.Background(), c, d)
			if got.Disposition != "halt" || len(d.sent) != want || got.Result.Success {
				t.Fatalf("%s sent%d want%d disposition%s", fault, len(d.sent), want, got.Disposition)
			}
			if len(got.Issues) != len(got.Result.Errors) {
				t.Fatal("untyped failure")
			}
		})
	}
}

func TestCollectionPolicyFactMismatchRemainsNegative(t *testing.T) {
	d := newCollectionTestDriver()
	d.steps[0].Turn.Response.Reply = "现金余额：500000元。"
	got := recordCollectionConversation(context.Background(), rechargeConversation(), d)
	if got.Disposition != "continue_recorded" || got.Result.Status != "failed" || got.Result.Success {
		t.Fatalf("fact negative lost: %s/%s", got.Disposition, got.Result.Status)
	}
	found := false
	for _, issue := range got.Issues {
		if issue.Code == "fact_mismatch" && issue.Turn == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("fact mismatch lacks origin evidence")
	}
}

type collectionCancelDriver struct {
	*collectionTestDriver
	cancel context.CancelFunc
}

func (d *collectionCancelDriver) Step(ctx context.Context, f Fixture, s, m string) (ConversationObservation, error) {
	obs, err := d.collectionTestDriver.Step(ctx, f, s, m)
	d.cancel()
	return obs, err
}
func TestCollectionPolicyCancellationStopsBeforeAnotherTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &collectionCancelDriver{newCollectionTestDriver(), cancel}
	got := recordCollectionConversation(ctx, rechargeConversation(), d)
	if got.Disposition != "halt" || len(d.sent) != 1 {
		t.Fatal("cancelled recording continued", got.Disposition, len(d.sent))
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	fresh := newCollectionTestDriver()
	got = recordCollectionConversation(ctx, rechargeConversation(), fresh)
	if got.Disposition != "halt" || len(fresh.sent) != 0 {
		t.Fatal("cancelled context reached driver")
	}
}
