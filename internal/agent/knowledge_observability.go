package agent

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/observability"
	"strings"
)

type knowledgeCorpusKey struct{}

func observeKnowledgeResult(ctx context.Context, r knowledge.Result) {
	stage := "retrieved"
	if r.Status != "ok" {
		stage = r.Status
	}
	ctx, root := observability.Start(ctx, "knowledge.retrieval", "span")
	root.Knowledge(observability.KnowledgeMetadata{CorpusVersion: r.CorpusVersion, Stage: stage, HitCount: len(r.Hits)})
	defer root.End(nil)
	for _, h := range r.Hits {
		_, op := observability.Start(ctx, "knowledge.chunk", "span")
		op.Knowledge(knowledgeMetadata(r, h, stage, 0))
		op.Input(map[string]any{"title": h.Title, "sources": h.Sources, "version": h.Version, "scope": h.Scope})
		op.KnowledgeContent(h.Text)
		op.End(nil)
	}
}
func knowledgeMetadata(r knowledge.Result, h knowledge.Hit, stage string, n int) observability.KnowledgeMetadata {
	return observability.KnowledgeMetadata{CorpusVersion: r.CorpusVersion, DocumentID: h.DocumentID, ChunkID: h.ChunkID, SourceSHA256: h.SHA256, Stage: stage, HitCount: len(r.Hits), RequestOrdinal: n}
}

// Only server-created paired assistant/tool messages can be knowledge context.
func requestedKnowledge(messages []Message) []knowledge.Result {
	calls := map[string]bool{}
	var out []knowledge.Result
	for _, m := range messages {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				calls[tc.ID] = tc.Function.Name == "search_platform_knowledge"
			}
		}
		if m.Role != "tool" || !calls[m.ToolCallID] {
			continue
		}
		delete(calls, m.ToolCallID)
		var r knowledge.Result
		if len(m.Content) > knowledge.MaxResultBytes || json.Unmarshal([]byte(m.Content), &r) != nil || r.Status != "ok" || len(r.CorpusVersion) != 64 || len(r.Hits) > 3 {
			continue
		}
		valid := true
		for _, h := range r.Hits {
			if len(h.Text) > knowledge.MaxChunkBytes || contentSHA([]byte(h.Text)) != h.SHA256 || !strings.HasPrefix(h.ChunkID, h.DocumentID+":") {
				valid = false
			}
		}
		if valid {
			out = append(out, r)
		}
	}
	return out
}
func observeKnowledgeRequest(ctx context.Context, messages []Message, ordinal int) {
	observeResidentRequest(ctx, messages, ordinal)
	current, _ := ctx.Value(knowledgeCorpusKey{}).(*knowledge.Corpus)
	for _, r := range requestedKnowledge(messages) {
		stage := "included_in_request"
		if current.Version() != r.CorpusVersion {
			stage = "stale_in_request"
		}
		for _, h := range r.Hits {
			_, op := observability.Start(ctx, "knowledge.request_context", "span")
			op.Knowledge(knowledgeMetadata(r, h, stage, ordinal))
			op.KnowledgeContent(h.Text)
			op.End(nil)
		}
	}
}
func observeKnowledgeResponse(ctx context.Context, messages []Message, err error) {
	stage := "provider_response"
	if err != nil {
		stage = "provider_error"
	}
	n := 0
	if s := turnTelemetry(ctx); s != nil {
		n = s.LLMCalls
	}
	for _, r := range requestedKnowledge(messages) {
		_, op := observability.Start(ctx, "knowledge.request_outcome", "span")
		op.Knowledge(observability.KnowledgeMetadata{CorpusVersion: r.CorpusVersion, Stage: stage, HitCount: len(r.Hits), RequestOrdinal: n})
		op.End(err)
	}
}
func knowledgeRequestOrdinal(ctx context.Context) int {
	if s := turnTelemetry(ctx); s != nil {
		return s.LLMCalls
	}
	return 0
}
