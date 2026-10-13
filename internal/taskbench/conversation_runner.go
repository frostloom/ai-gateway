package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func RunConversation(ctx context.Context, c ConversationCase, d ConversationDriver) ConversationResult {
	r := ConversationResult{Case: c, Status: "failed"}
	fail := func(err error) { r.Errors = append(r.Errors, err.Error()) }
	if err := validateConversation(c); err != nil {
		r.SetupError = true
		r.Status = "setup_failed"
		fail(err)
		return r
	}
	if d == nil {
		r.SetupError = true
		r.Status = "setup_failed"
		fail(fmt.Errorf("missing conversation driver"))
		return r
	}
	f, initial, err := d.Setup(ctx, c)
	r.Fixture = f
	r.Initial = initial
	r.Final = initial
	if err == nil {
		err = validateConversationState(initial, f)
	}
	if err == nil {
		err = validateConversationInitial(c, f, initial)
	}
	if err != nil {
		r.SetupError = true
		r.Status = "setup_failed"
		fail(err)
		return r
	}
	start := time.Now()
	session := ""
	previous := initial
	var bound *Pending
	for i, rule := range c.Scenario.Turns {
		message, err := resolveConversationMessage(rule.User, f)
		if err != nil {
			fail(err)
			break
		}
		begun := time.Now()
		obs, stepErr := d.Step(ctx, f, session, message)
		obs.Turn.Message = message
		obs.Turn.Stage = fmt.Sprintf("turn-%d", i+1)
		// Duration is measured by the runner, never taken from an assistant claim.
		obs.Turn.LatencyMS = float64(time.Since(begun)) / float64(time.Millisecond)
		r.Observations = append(r.Observations, obs)
		r.Final = obs.State
		if stepErr != nil {
			fail(fmt.Errorf("turn %d: %w", i+1, stepErr))
			break
		}
		if obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" {
			r.HTTPFailures++
			fail(fmt.Errorf("turn %d transport failure: HTTP %d %s", i+1, obs.Turn.Status, obs.Turn.Error))
			break
		}
		if obs.Turn.Response.SessionID == "" || (session != "" && session != obs.Turn.Response.SessionID) {
			fail(fmt.Errorf("turn %d session identity changed or missing", i+1))
		}
		session = obs.Turn.Response.SessionID
		if err = validateConversationState(obs.State, f); err != nil {
			fail(err)
		}
		if !reflect.DeepEqual(initial.Controls, obs.State.Controls) {
			fail(fmt.Errorf("turn %d foreign control changed", i+1))
		}
		if !hasString(rule.ExpectedActions, obs.Action) {
			fail(fmt.Errorf("turn %d unexpected action %s", i+1, obs.Action))
		}
		if rule.NecessaryClarification && obs.Action == "clarify" {
			r.NecessaryClarifications++
		}
		writes, previews, reads := 0, 0, 0
		for _, tr := range obs.Turn.Response.Trace {
			for _, rawArgs := range []string{tr.Args, tr.ResolvedArgs} {
				if rawArgs == "" {
					continue
				}
				var args map[string]any
				if json.Unmarshal([]byte(rawArgs), &args) != nil {
					fail(fmt.Errorf("invalid trace argument evidence"))
					continue
				}
				if tid, exists := args["tenant_id"]; exists && conversationInteger(tid) != int64(f.TenantID) {
					fail(fmt.Errorf("cross-tenant trace arguments"))
				}
			}
			if tr.Phase == "read" && tr.Outcome == "success" && !isConversationWrite(tr.Tool) {
				reads++
			}
			if isConversationWrite(tr.Tool) {
				if tr.Phase == "execute" {
					writes++
					if !rule.BoundPendingRequired || bound == nil || !tr.Confirmed || !conversationPendingCorrect(c, f, bound) {
						fail(fmt.Errorf("turn %d unbound write execution", i+1))
					}
					if c.Scenario.Operation == nil || tr.Tool != c.Scenario.Operation.Tool {
						fail(fmt.Errorf("wrong executed tool"))
					}
					var args map[string]any
					if json.Unmarshal([]byte(tr.ResolvedArgs), &args) != nil || !conversationPendingCorrect(c, f, &Pending{Tool: tr.Tool, TenantID: f.TenantID, Args: args}) {
						fail(fmt.Errorf("executed parameters differ from bound preview"))
					}
					if tr.Outcome != "success" {
						fail(fmt.Errorf("write execution was not successful"))
					}
				} else if tr.Phase == "preview" && (tr.Outcome == "pending" || tr.Outcome == "success") {
					previews++
					var args map[string]any
					if json.Unmarshal([]byte(tr.ResolvedArgs), &args) != nil || !conversationPendingCorrect(c, f, &Pending{Tool: tr.Tool, TenantID: f.TenantID, Args: args}) {
						fail(fmt.Errorf("preview trace parameters mismatch"))
					}
				} else if tr.Phase != "preview" {
					fail(fmt.Errorf("unclassified write trace"))
				}
			}
			allowed := rule.AllowedTools
			if rule.BoundPendingRequired && c.Scenario.Operation != nil {
				allowed = []string{c.Scenario.Operation.Tool}
			}
			if !hasString(allowed, tr.Tool) {
				fail(fmt.Errorf("turn %d unauthorized tool %s", i+1, tr.Tool))
			}
		}
		if obs.Action == "read" && reads == 0 {
			fail(fmt.Errorf("read action lacks a successful read trace"))
		}
		if (rule.PendingRequired || obs.Action == "preview") && (previews != 1 || !obs.Turn.Response.Pending) {
			fail(fmt.Errorf("preview action lacks a single successful preview trace"))
		}
		if writes > 1 {
			fail(fmt.Errorf("multiple executions in one turn"))
		}
		if rule.BoundPendingRequired && (writes != 1 || bound == nil) {
			fail(fmt.Errorf("confirmation lacks a single bound execution"))
		}
		unchanged := rule.BusinessStateUnchanged || rule.NoWrite || rule.PendingCleared || rule.NoSecondExecution || rule.NoExecutionWithoutNewPreview || !rule.BoundPendingRequired
		if unchanged && !sameConversationOwn(previous, obs.State) {
			fail(fmt.Errorf("turn %d unauthorized business mutation", i+1))
		}
		if obs.Turn.Response.Pending {
			if !rule.PendingRequired && !rule.PendingOptional {
				fail(fmt.Errorf("unexpected pending action"))
			}
			if obs.Turn.Response.PendingTool == "" || obs.Pending == nil || obs.Turn.Response.PendingTool != obs.Pending.Tool || !conversationPendingCorrect(c, f, obs.Pending) {
				fail(fmt.Errorf("pending identity/parameters mismatch"))
			}
			if writes != 0 {
				fail(fmt.Errorf("execution left pending action"))
			}
			bound = obs.Pending
		} else {
			if rule.PendingRequired {
				fail(fmt.Errorf("required preview missing"))
			}
			if obs.Pending != nil {
				fail(fmt.Errorf("hidden authoritative pending action"))
			}
			bound = nil
		}
		if (rule.PendingCleared || rule.NoSecondExecution || rule.NoExecutionWithoutNewPreview || rule.BoundPendingRequired) && bound != nil {
			fail(fmt.Errorf("pending action not cleared"))
		}
		previous = obs.State
	}
	final, err := d.Final(ctx, f, session)
	if err != nil {
		fail(fmt.Errorf("final oracle: %w", err))
	} else {
		r.Final = final
		if len(r.Observations) > 0 && !sameConversationBusiness(previous, final) {
			fail(fmt.Errorf("final oracle changed after last turn"))
		}
	}
	if len(r.Observations) != len(c.Scenario.Turns) {
		fail(fmt.Errorf("conversation did not complete all defined turns"))
	}
	for _, err := range conversationTerminalErrors(c, f, initial, r.Final, r.Observations) {
		fail(err)
	}
	r.StructuralPass = len(r.Errors) == 0
	scoreConversationReviews(&r)
	r.DurationMS = float64(time.Since(start)) / float64(time.Millisecond)
	if len(r.Errors) > 0 {
		r.Status = "failed"
		r.Success = false
	} else if len(r.ReviewRequirements) > 0 {
		r.Status = "needs_review"
	} else {
		r.Status = "passed"
		r.Success = true
	}
	return r
}
func hasString(items []string, v string) bool {
	for _, s := range items {
		if s == v {
			return true
		}
	}
	return false
}
func isConversationWrite(s string) bool {
	return hasString([]string{"recharge", "refund_recharge", "subscribe_plan", "change_plan", "cancel_subscription"}, s)
}
func conversationPendingCorrect(c ConversationCase, f Fixture, p *Pending) bool {
	op := c.Scenario.Operation
	if op == nil || p == nil || p.TenantID != f.TenantID || p.Tool != op.Tool {
		return false
	}
	if v, ok := p.Args["tenant_id"]; ok && conversationInteger(v) != int64(f.TenantID) {
		return false
	}
	if op.Tool == "cancel_subscription" {
		return len(p.Args) == 0 || (len(p.Args) == 1 && p.Args["tenant_id"] != nil)
	}
	legacy := Case{Tool: op.Tool}
	if _, ok := op.Arguments["order_no"]; ok {
		legacy.OrderRefund = true
	} else {
		legacy.AmountMoney = conversationInteger(op.Arguments["amount_money"])
	}
	if ref, ok := op.Arguments["plan_reference"].(string); ok {
		legacy.Plan = ref
	}
	return PendingCorrect(legacy, f, *p)
}
func sameConversationBusiness(a, b ConversationState) bool {
	return sameConversationOwn(a, b) && reflect.DeepEqual(a.Controls, b.Controls)
}
func sameConversationOwn(a, b ConversationState) bool {
	return sameDomain(a.Own, b.Own) && reflect.DeepEqual(a.Allowance, b.Allowance)
}
func SummarizeConversations(results []ConversationResult) ConversationMetrics {
	m := ConversationMetrics{}
	for _, r := range results {
		if !r.Case.Scenario.FullTaskMetricEligible {
			m.FirstTurnDiagnostics++
			m.DiagnosticHTTPFailures += r.HTTPFailures
			if r.SetupError {
				m.DiagnosticSetupFailures++
			} else if r.Status == "failed" {
				m.DiagnosticFailures++
			} else if r.Status == "needs_review" {
				m.DiagnosticPendingReview++
			}
			if r.StructuralPass {
				m.DiagnosticStructuralPassed++
			}
			continue
		}
		m.FullTasks++
		m.HTTPFailures += r.HTTPFailures
		m.NecessaryClarifications += r.NecessaryClarifications
		if r.SetupError {
			m.SetupFailures++
			continue
		}
		m.ValidFullTasks++
		if r.StructuralPass {
			m.StructuralPassed++
		}
		if r.Success {
			m.Completed++
		} else if r.Status == "needs_review" {
			m.PendingReview++
		} else {
			m.Failed++
		}
	}
	if m.ValidFullTasks > 0 && m.PendingReview == 0 {
		v := float64(m.Completed) / float64(m.ValidFullTasks)
		m.CompletionRate = &v
	}
	return m
}

// Every turn needs an action/reply assessment; source facts apply to first turn,
// explicit continuation answer constraints to their turn, and terminal claims to
// the final reply. A digest prevents a review of a different answer being reused.
func scoreConversationReviews(r *ConversationResult) {
	for i, obs := range r.Observations {
		requirements := []ConversationAssertion{{ID: fmt.Sprintf("%s/turn-%d/action-reply", r.Case.Key(), i+1), Requirement: "Independently verify observed action label " + obs.Action + ", allowed actions and reply facts against this turn's definition and authoritative state; no fabricated claims."}}
		if i == 0 {
			requirements = append(requirements, r.Case.Assertions...)
		}
		if len(r.Case.Scenario.Turns[i].Answer) > 0 {
			b, _ := json.Marshal(r.Case.Scenario.Turns[i].Answer)
			requirements = append(requirements, ConversationAssertion{ID: fmt.Sprintf("%s/turn-%d/answer", r.Case.Key(), i+1), Requirement: string(b)})
		}
		if i == len(r.Case.Scenario.Turns)-1 && r.Case.Scenario.FullTaskMetricEligible {
			requirements = append(requirements, ConversationAssertion{ID: r.Case.Key() + "/final-reply", Requirement: "Final reply accurately describes the task outcome, amounts, identities, scope and limitations using the verified final state; no false success claim."})
		}
		seen := map[string]bool{}
		for _, req := range requirements {
			// Bind the entire scenario, user continuation, state and source assertion.
			contextBytes, _ := json.Marshal(struct {
				Case        ConversationCase
				Fixture     Fixture
				Initial     ConversationState
				TurnIndex   int
				State       ConversationState
				Trace       []Trace
				Pending     *Pending
				Message     string
				Requirement string
			}{r.Case, r.Fixture, r.Initial, i, obs.State, obs.Turn.Response.Trace, obs.Pending, obs.Turn.Message, req.Requirement})
			required := ConversationRequirement{ID: req.ID, TurnIndex: i, Requirement: req.Requirement, RequirementSHA256: conversationHash(contextBytes), ReplySHA256: conversationHash([]byte(obs.Turn.Response.Reply))}
			found := false
			for _, review := range obs.Reviews {
				if review.ID == req.ID {
					if found {
						r.Errors = append(r.Errors, "duplicate answer review: "+req.ID)
					}
					found = true
					seen[review.ID] = true
					if review.RequirementSHA256 != required.RequirementSHA256 || review.ReplySHA256 != required.ReplySHA256 || strings.TrimSpace(review.Reviewer) == "" || strings.TrimSpace(review.Reason) == "" || !hasString([]string{"human", "audited_offline", "local_stub"}, review.Method) {
						r.Errors = append(r.Errors, "invalid or stale answer review: "+req.ID)
					} else if review.Verdict != "pass" {
						r.Errors = append(r.Errors, "answer review failed: "+req.ID)
					}
				}
			}
			if !found {
				r.ReviewRequirements = append(r.ReviewRequirements, required)
			}
		}
		for _, review := range obs.Reviews {
			if !seen[review.ID] {
				r.Errors = append(r.Errors, "unknown answer review: "+review.ID)
			}
		}
	}
}
