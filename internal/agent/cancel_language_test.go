package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A prose promise to cancel must never leave a persisted write executable.
func TestAgentNaturalCancellationAcrossRequests(t *testing.T) {
	h, fa, db, done := newAgentHandler(t)
	defer done()
	ctx := context.Background()
	for _, tool := range []string{"recharge", "refund_recharge", "subscribe_plan", "change_plan", "cancel_subscription"} {
		for i, phrase := range []string{"取消，不执行这次操作。", "取消这次操作", "不执行了", " 取消， 不执行本次操作！ ", "算了，不充值了"} {
			t.Run(fmt.Sprintf("%s/%d", tool, i), func(t *testing.T) {
				sid := fmt.Sprintf("sid-natural-cancel-%s-%d", tool, i)
				if err := h.saveSession(ctx, sid, &session{PendingAction: &pendingAction{
					Tool: tool, Args: map[string]any{"amount_money": 100},
					Preview: "本次待确认操作", IdemKey: "cancelled-" + sid, TenantID: 42,
				}}); err != nil {
					t.Fatal(err)
				}
				// Cancellation must bypass model inference even if the model is unavailable.
				h.llm = &scriptedLLM{}
				rec := cancellationRequest(h, sid, phrase)
				if rec.Code != http.StatusOK {
					t.Fatalf("cancel HTTP %d: %s", rec.Code, rec.Body.String())
				}
				var body struct {
					Pending bool   `json:"pending_confirm"`
					Reply   string `json:"reply"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Pending || !strings.Contains(body.Reply, "已取消") {
					t.Fatalf("cancellation not reflected in response: %s", rec.Body.String())
				}
				saved, err := h.loadSession(ctx, sid)
				if err != nil || saved.PendingAction != nil {
					t.Fatalf("cancelled write still persisted: session=%+v err=%v", saved, err)
				}
				if got := countAudit(t, db, "session_id = ? AND tool = ? AND confirmed = ? AND result = ?", sid, "cancel_confirmation", false, "cancelled"); got != 1 {
					t.Fatalf("durable cancellation audit rows=%d, want 1", got)
				}
				// A later confirmation loads the cleared session and cannot execute the old write.
				h.llm = &scriptedLLM{steps: []*Message{contentMsg("没有待确认操作。")}}
				followup := cancellationRequest(h, sid, "确认")
				if followup.Code != http.StatusOK {
					t.Fatalf("follow-up HTTP %d: %s", followup.Code, followup.Body.String())
				}
				if got := countAudit(t, db, "session_id = ? AND confirmed = ?", sid, true); got != 0 {
					t.Fatalf("cancelled write executed on follow-up: audits=%d", got)
				}
				fa.mu.Lock()
				posts := len(fa.posts)
				fa.mu.Unlock()
				if posts != 0 {
					t.Fatalf("cancelled operation made %d billing writes", posts)
				}
			})
		}
	}
}

func cancellationRequest(h *Handler, sid, message string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"session_id": sid, "message": message})
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer sk-test-key-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestConfirmationNaturalCancellationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"取消，不执行这次操作。", -1},
		{"取消这次操作", -1},
		{"算了，不充值了", -1},
		{"不执行了", -1},
		{"取消;不执行本次操作!", -1},
		{"取消", -1}, {"STOP!", -1}, {"确认。", 1}, {"yes.", 1},
		{"不要取消，确认", 0},
		{"取消自动续费", 0}, {"取消订阅", 0},
		{"取消订阅，确认", 0},
		{"取消这次操作，改充200元", 0},
		{"我想知道取消这次操作会怎样", 0},
		{"取消吗？", 0}, {"取消这次操作？", 0}, {"不执行了?", 0}, {"不充值了", 0},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if got := classifyConfirmation(tc.text); got != tc.want {
				t.Fatalf("classifyConfirmation(%q)=%d, want %d", tc.text, got, tc.want)
			}
		})
	}
}
