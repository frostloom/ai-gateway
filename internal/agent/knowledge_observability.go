package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/observability"
)

var knowledgeDocumentID = regexp.MustCompile(`^KB-[0-9]{3}$`)
var knowledgeChunkID = regexp.MustCompile(`^KB-[0-9]{3}:[1-9][0-9]{0,3}$`)

func knowledgeObservationState(ctx context.Context) *knowledgeTurn {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(knowledgeContextKey{}).(*knowledgeTurn)
	return state
}

func observeKnowledgeRetrieval(ctx context.Context, r knowledge.Result, cacheHit bool, source string, err error) {
	if knowledgeObservationState(ctx) == nil {
		return
	}
	if source != "prefetch" && source != "tool" {
		return
	}
	if stats := turnTelemetry(ctx); stats != nil {
		stats.KnowledgeSearches++
		if cacheHit {
			stats.KnowledgeCacheHits++
		}
		stats.KnowledgeHits += len(r.Hits)
		if err != nil || r.Status == "unavailable" {
			stats.KnowledgeErrors++
		}
	}
	stage := "retrieved"
	if r.Status != "ok" {
		stage = r.Status
	}
	if err != nil {
		stage = "unavailable"
	}
	ctx, root := observability.Start(ctx, "knowledge.retrieval", "span")
	root.Knowledge(observability.KnowledgeMetadata{CorpusVersion: r.CorpusVersion, Stage: stage, HitCount: len(r.Hits), CacheHit: cacheHit, Source: source})
	defer root.End(err)
	for _, h := range r.Hits {
		_, op := observability.Start(ctx, "knowledge.chunk", "span")
		meta := knowledgeObservationMetadata(r, h, stage, 0, source)
		meta.CacheHit = cacheHit
		op.Knowledge(meta)
		op.KnowledgeContent(h.Text)
		op.End(nil)
	}
}

func knowledgeObservationMetadata(r knowledge.Result, h knowledge.Hit, stage string, n int, source string) observability.KnowledgeMetadata {
	return observability.KnowledgeMetadata{CorpusVersion: r.CorpusVersion, DocumentID: h.DocumentID, ChunkID: h.ChunkID, SourceSHA256: h.SHA256, Stage: stage, HitCount: len(r.Hits), RequestOrdinal: n, Source: source}
}

type requestedKnowledgeSource struct {
	result knowledge.Result
	source string
}

func validRequestedKnowledge(r knowledge.Result, version string) bool {
	if r.Status != "ok" || r.CorpusVersion != version || len(version) != 64 || len(r.Hits) == 0 || len(r.Hits) > 3 {
		return false
	}
	if _, err := hex.DecodeString(version); err != nil {
		return false
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > knowledge.MaxResultBytes {
		return false
	}
	for _, h := range r.Hits {
		sum := sha256.Sum256([]byte(h.Text))
		if len(h.Text) > knowledge.MaxChunkBytes || hex.EncodeToString(sum[:]) != h.SHA256 || !knowledgeDocumentID.MatchString(h.DocumentID) || !knowledgeChunkID.MatchString(h.ChunkID) || !strings.HasPrefix(h.ChunkID, h.DocumentID+":") {
			return false
		}
	}
	return true
}

// Only the exact server prefetch and paired server tool history can be sources.
// User content is never inspected for knowledge markers or citations.
func requestedKnowledge(ctx context.Context, messages []Message) []requestedKnowledgeSource {
	state := knowledgeObservationState(ctx)
	if state == nil {
		return nil
	}
	var out []requestedKnowledgeSource
	if validRequestedKnowledge(state.prefetch, state.version) && len(messages) > 0 && messages[0].Role == "system" {
		raw, _ := json.Marshal(state.prefetch)
		marker := "\n<platform_knowledge>\n" + string(raw) + "\n</platform_knowledge>"
		if strings.Contains(messages[0].Content, marker) {
			out = append(out, requestedKnowledgeSource{state.prefetch, "prefetch"})
		}
	}
	calls := map[string]bool{}
	for _, m := range messages {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					calls[tc.ID] = tc.Function.Name == "search_platform_knowledge"
				}
			}
		}
		if m.Role != "tool" || !calls[m.ToolCallID] {
			continue
		}
		delete(calls, m.ToolCallID)
		var r knowledge.Result
		if len(m.Content) > knowledge.MaxResultBytes || json.Unmarshal([]byte(m.Content), &r) != nil || !validRequestedKnowledge(r, state.version) {
			continue
		}
		out = append(out, requestedKnowledgeSource{r, "tool"})
	}
	return out
}

func observeKnowledgeRequest(ctx context.Context, messages []Message) {
	sources := requestedKnowledge(ctx, messages)
	if len(sources) == 0 {
		return
	}
	ordinal := 0
	if stats := turnTelemetry(ctx); stats != nil {
		stats.KnowledgeRequests++
		ordinal = stats.LLMCalls
		for _, s := range sources {
			for _, h := range s.result.Hits {
				id := h.ChunkID
				found := false
				for _, prior := range stats.KnowledgeSources {
					if prior == id {
						found = true
						break
					}
				}
				if !found {
					stats.KnowledgeSources = append(stats.KnowledgeSources, id)
				}
			}
		}
	}
	for _, s := range sources {
		for _, h := range s.result.Hits {
			_, op := observability.Start(ctx, "knowledge.request_context", "span")
			op.Knowledge(knowledgeObservationMetadata(s.result, h, "included_in_request", ordinal, s.source))
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
	ordinal := 0
	if stats := turnTelemetry(ctx); stats != nil {
		ordinal = stats.LLMCalls
	}
	for _, s := range requestedKnowledge(ctx, messages) {
		_, op := observability.Start(ctx, "knowledge.request_outcome", "span")
		op.Knowledge(observability.KnowledgeMetadata{CorpusVersion: s.result.CorpusVersion, Stage: stage, HitCount: len(s.result.Hits), RequestOrdinal: ordinal, Source: s.source})
		op.End(err)
	}
}
