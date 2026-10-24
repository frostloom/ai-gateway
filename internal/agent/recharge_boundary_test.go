package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestEmptyPendingConfirmationCannotCreatePreview(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective"} {
		t.Run(mode, func(t *testing.T) {
			h, fa, _, done := newAgentHandler(t)
			defer done()
			if err := h.SetRoutingMode(mode); err != nil {
				t.Fatal(err)
			}
			h.SetJev(jev.NewFake())
			for _, text := range []string{"确认", "执行", "确定！", " OK "} {
				model := &scriptedLLM{steps: []*Message{toolCallMsg("unexpected", "recharge", `{"amount_money":100}`)}}
				h.llm = model
				stats := &TurnTelemetry{}
				ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
				ses := &session{}
				reply, trace, err := h.process(ctx, "empty-confirm", 42, text, ses)
				if err != nil || ses.PendingAction != nil || len(trace) != 0 || len(fa.posts) != 0 || len(model.steps) != 1 || stats.JevCalls != 0 || !strings.Contains(reply, "没有待确认") {
					t.Fatalf("empty confirmation inferred operation: %q pending=%+v trace=%+v stats=%+v err=%v", reply, ses.PendingAction, trace, stats, err)
				}
			}
		})
	}
}

func TestRechargeCompletionNeedsActualExecution(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective"} {
		for _, user := range []string{"充值 100 元", "明确申请给我本人账户充值100元人民币，请先预览。"} {
			for _, variant := range []string{"repair", "exhausted", "tool-explanation"} {
				t.Run(mode+"/"+variant+"/"+user, func(t *testing.T) {
					h, fa, _, done := newAgentHandler(t)
					defer done()
					if err := h.SetRoutingMode(mode); err != nil {
						t.Fatal(err)
					}
					h.SetJev(jev.NewFake(fastVerdict("recharge", .99)))
					proposal := toolCallMsg("preview", "recharge", `{"amount_money":100}`)
					steps := []*Message{contentMsg("充值成功：到账 ¥100。"), proposal}
					if variant == "exhausted" {
						steps = []*Message{contentMsg("充值成功：到账 ¥100。"), contentMsg("充值成功：到账 ¥100。")}
					}
					if variant == "tool-explanation" {
						proposal.Content = "充值成功：到账 ¥100。"
						steps = []*Message{proposal}
					}
					model := &scriptedLLM{steps: steps}
					h.llm = model
					ses := &session{}
					stats := &TurnTelemetry{}
					ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
					reply, trace, err := h.process(ctx, "success-before-preview", 42, user, ses)
					if err != nil || strings.Contains(reply, "充值成功") || len(fa.posts) != 0 || len(model.steps) != 0 {
						t.Fatalf("false completion leaked: reply=%q trace=%+v pending=%+v err=%v", reply, trace, ses.PendingAction, err)
					}
					if variant == "exhausted" {
						if ses.PendingAction != nil || len(trace) != 0 || stats.ReplyRepairs != 1 || !strings.Contains(reply, "没有执行") {
							t.Fatal("unbounded or false exhausted repair")
						}
						return
					}
					wantRepairs := 1
					if variant == "tool-explanation" {
						wantRepairs = 0
					}
					if ses.PendingAction == nil || len(trace) != 1 || trace[0].Phase != "preview" || stats.ReplyRepairs != wantRepairs {
						t.Fatal("real preview lost or extra calls")
					}
					before := stats.JevCalls
					reply, trace, err = h.process(ctx, "success-before-preview", 42, "确认", ses)
					if err != nil || ses.PendingAction != nil || len(trace) != 1 || trace[0].Phase != "execute" || trace[0].Outcome != "success" || len(fa.posts) != 2 || stats.JevCalls != before || !strings.Contains(reply, "充值成功") {
						t.Fatalf("bound confirmation damaged: %q trace=%+v err=%v", reply, trace, err)
					}
				})
			}
		}
	}
}

func TestRechargeCompletionRepairFiniteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		user, reply string
		repair      bool
	}{
		{"充值100元", "充值成功：到账 ¥100。", true},
		{"帮我充值100元", "已充值成功。", true},
		{"我要充值", "充值成功！", true},
		{"昨天充值100元成功了吗", "充值成功：到账 ¥100。", false},
		{"不要充值100元", "充值成功：到账 ¥100。", false},
		{"充值100元", "如果充值成功：余额会增加。", false},
		{"充值100元", "“充值成功：到账 ¥100。”是示例。", false},
		{"充值100元", "充值成功后可以查询账单。", false},
	} {
		if got := replyRepairReason(tc.reply, tc.user, nil) != ""; got != tc.repair {
			t.Fatalf("user=%q reply=%q repair=%v want=%v", tc.user, tc.reply, got, tc.repair)
		}
	}
}

func TestCompoundConfirmationStillRoutesNewApplication(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.SetRoutingMode("model")
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("new", "recharge", `{"amount_money":100}`)}}
	ses := &session{}
	_, trace, err := h.process(context.Background(), "compound", 42, "确认充值100元", ses)
	if err != nil || ses.PendingAction == nil || len(trace) != 1 || trace[0].Phase != "preview" {
		t.Fatal("complete phrase matcher swallowed a new application")
	}
}

func TestRechargeBoundaryChangesRuntimeContract(t *testing.T) {
	h := &Handler{}
	if got := h.EvaluationRuntimeMetadata().ContractSHA256; got == "b92458d1bafd3187a2daa6387b0575e9517da84f66df6602b1945a8d47ba9d8e" {
		t.Fatal("recharge execution changes reused cash-only runtime identity")
	}
}

func TestSupplementRechargeCompletionFiniteForms(t *testing.T) {
	for _, tc := range []struct {
		user   string
		repair bool
	}{
		{"明确申请给我本人账户充值100元人民币，请先预览。", true},
		{"请给我本人账户充值100元人民币。", true},
		{"给我本人账户充值 100 元，请先预览。", true},
		{"明确申请给我本人账户充值100元人民币", true},
		{"明确申请给他本人账户充值100元人民币，请先预览。", false},
		{"不要给我本人账户充值100元人民币，请先预览。", false},
		{"昨天给我本人账户充值100元人民币，请先预览。", false},
		{"如果给我本人账户充值100元人民币，请先预览。", false},
		{"用户说：明确申请给我本人账户充值100元人民币，请先预览。", false},
		{"明确申请给我本人账户充值100元人民币，请先预览，然后退款50元。", false},
		{"明确申请给我本人账户充值100token，请先预览。", false},
		{"明确申请给我本人账户充值100元人民币，如何办理？", false},
		{"100元", false},
	} {
		if got := replyRepairReason("充值成功：到账 ¥100。", tc.user, nil) != ""; got != tc.repair {
			t.Errorf("%q repair=%v want=%v", tc.user, got, tc.repair)
		}
	}
}

func TestSupplementRechargeChangesRuntimeIdentity(t *testing.T) {
	if got := (&Handler{}).EvaluationRuntimeMetadata().ContractSHA256; got == "35c117c3dcee33a783e074712d6836236dd1f6a9d4a798fdfd01f0c92e95bce1" {
		t.Fatal("new boundary reused historical recharge contract")
	}
}
