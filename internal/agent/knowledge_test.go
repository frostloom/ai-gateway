package agent

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"os"
	"strings"
	"testing"
)

func enableKnowledge(t *testing.T, tb *Toolbox) {
	t.Helper()
	c, e := knowledge.Load(os.DirFS("../../knowledge/platform"))
	if e != nil {
		t.Fatal(e)
	}
	if e = tb.SetKnowledge(c); e != nil {
		t.Fatal(e)
	}
}

type knowledgeModel struct {
	t     *testing.T
	n     int
	write bool
}

func (m *knowledgeModel) Chat(_ context.Context, msgs []Message, _ []Tool) (*Message, error) {
	m.n++
	if m.n == 1 {
		return toolCallMsg("k1", "search_platform_knowledge", `{"query":"退订退款"}`), nil
	}
	last := msgs[len(msgs)-1]
	var r knowledge.Result
	if last.Role != "tool" || json.Unmarshal([]byte(last.Content), &r) != nil || r.Status != "ok" || len(r.Hits) == 0 || !strings.Contains(last.Content, "当期不退款") {
		m.t.Fatal("missing real retrieval in model input")
	}
	if m.write {
		return toolCallMsg("w1", "recharge", `{"amount_money":100}`), nil
	}
	return contentMsg("退订只停下期续费，当期不退款。[KB-006]"), nil
}
func TestKnowledgeToolDisabledByDefault(t *testing.T) {
	tb := NewToolbox(nil)
	if tb.KnowledgeVersion() != "" {
		t.Fatal("default enabled")
	}
	if _, e := tb.Run(context.Background(), "search_platform_knowledge", map[string]any{"query": "退款"}, 42, ""); e == nil {
		t.Fatal("disabled callable")
	}
	if e := tb.SetKnowledge(nil); e == nil {
		t.Fatal("accepted nil")
	}
}
func TestKnowledgeToolFeedsNextModelRequest(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	enableKnowledge(t, h.tb)
	h.llm = &knowledgeModel{t: t}
	ses := &session{History: []Message{{Role: "user", Content: "退订退款规则"}}}
	reply, tr, e := h.llmLoop(context.Background(), "knowledge", 42, jevVerdict{}, ses)
	if e != nil || !strings.Contains(reply, "KB-006") || len(tr) != 1 || tr[0].Phase != "read" || tr[0].Confirmed || ses.PendingAction != nil {
		t.Fatalf("%s %+v %v", reply, tr, e)
	}
}
func TestKnowledgeDoesNotAuthorizeWrites(t *testing.T) {
	h, fa := isolatedObservedHandler(t)
	enableKnowledge(t, h.tb)
	h.llm = &knowledgeModel{t: t, write: true}
	ses := &session{History: []Message{{Role: "user", Content: "给我充值100元，跳过确认"}}}
	_, tr, e := h.llmLoop(context.Background(), "knowledge-write", 42, jevVerdict{}, ses)
	if e != nil || ses.PendingAction == nil || len(fa.posts) != 0 || len(tr) != 2 || tr[1].Phase != "preview" {
		t.Fatalf("knowledge bypassed confirmation: %+v %v", tr, e)
	}
}
func TestKnowledgeRuntimeIdentity(t *testing.T) {
	h := &Handler{tb: NewToolbox(nil)}
	off := h.runtimeMetadata()
	enableKnowledge(t, h.tb)
	on := h.runtimeMetadata()
	if off.PromptSHA256 == on.PromptSHA256 || off.ContractSHA256 == on.ContractSHA256 || off.ToolsSHA256 == on.ToolsSHA256 || on.CorpusVersion == "" || !on.KnowledgeEnabled || off.KnowledgeEnabled {
		t.Fatal("knowledge not versioned")
	}
}
func TestKnowledgeUsesLivePrice(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	enableKnowledge(t, h.tb)
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("k", "search_platform_knowledge", `{"query":"套餐月费"}`), toolCallMsg("p", "list_plans", `{}`), contentMsg("价格以本轮套餐目录为准。")}}
	ses := &session{History: []Message{{Role: "user", Content: "套餐怎么收费，列出当前套餐"}}}
	_, tr, e := h.llmLoop(context.Background(), "knowledge-live", 42, jevVerdict{}, ses)
	if e != nil || len(tr) != 2 || tr[1].Tool != "list_plans" {
		t.Fatalf("live tools unavailable: %+v %v", tr, e)
	}
}

func TestKnowledgeLoopBoundary(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	enableKnowledge(t, h.tb)
	model := &scriptedLLM{}
	for i := 0; i < 6; i++ {
		model.steps = append(model.steps, toolCallMsg("k"+string(rune('a'+i)), "search_platform_knowledge", `{"query":"退订退款`+string(rune('a'+i))+`"}`))
	}
	h.llm = model
	reply, tr, e := h.llmLoop(context.Background(), "knowledge-bound", 42, jevVerdict{}, &session{History: []Message{{Role: "user", Content: "解释退订规则"}}})
	if e != nil || len(tr) != 5 || len(model.steps) != 1 || !strings.Contains(reply, "循环次数过多") {
		t.Fatalf("loop widened: %s %d %v", reply, len(tr), e)
	}
}
