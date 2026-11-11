package observability

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestKnowledgeContentCaptureAndMetadata(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[on], func(t *testing.T) {
			server, capture := receiver(t)
			defer server.Close()
			cfg := enabled(server.URL)
			cfg.CaptureContent = on
			o, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, turn := o.StartTurn(context.Background(), "model")
			_, op := Start(ctx, "knowledge.chunk", "span")
			op.Knowledge(KnowledgeMetadata{CorpusVersion: strings.Repeat("a", 64), DocumentID: "KB-001", ChunkID: "KB-001:1", SourceSHA256: strings.Repeat("b", 64), Stage: "retrieved", HitCount: 1, CacheHit: true, Source: "prefetch"})
			op.KnowledgeContent("知识原文 user@example.com")
			op.End(nil)
			turn.End(200, Summary{})
			if err := o.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			capture.mu.Lock()
			defer capture.mu.Unlock()
			found := false
			for _, span := range capture.spans {
				if span.Name != "knowledge.chunk" {
					continue
				}
				found = true
				a := attrs(span)
				if a["langfuse.observation.metadata.knowledge_cache_hit"] != "true" || a["langfuse.observation.metadata.knowledge_source"] != "prefetch" {
					t.Fatalf("metadata=%v", a)
				}
				if on && !strings.Contains(a["langfuse.observation.output"], "[REDACTED]") {
					t.Fatal("capture not sanitized")
				}
				if !on && a["langfuse.observation.output"] != "" {
					t.Fatal("capture off leaked content")
				}
			}
			if !found {
				t.Fatal("missing span")
			}
			if bytes.Contains(capture.raw, []byte("user@example.com")) {
				t.Fatal("email leaked")
			}
		})
	}
}

func TestKnowledgeMetadataRejectsInvalidSourceIdentity(t *testing.T) {
	server, capture := receiver(t)
	defer server.Close()
	o, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, turn := o.StartTurn(context.Background(), "model")
	for _, k := range []KnowledgeMetadata{
		{CorpusVersion: strings.Repeat("a", 64), DocumentID: "KB-001:1", ChunkID: "KB-001:1", SourceSHA256: strings.Repeat("b", 64), Stage: "retrieved", HitCount: 1},
		{CorpusVersion: strings.Repeat("a", 64), DocumentID: "KB-001", ChunkID: "KB-002:1", SourceSHA256: strings.Repeat("b", 64), Stage: "retrieved", HitCount: 1},
		{CorpusVersion: strings.Repeat("a", 64), ChunkID: "untrusted text", Stage: "retrieved", HitCount: 1},
	} {
		_, op := Start(ctx, "invalid.knowledge", "span")
		op.Knowledge(k)
		op.End(nil)
	}
	turn.End(200, Summary{})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, span := range capture.spans {
		if span.Name == "invalid.knowledge" && attrs(span)["langfuse.observation.metadata.knowledge_stage"] != "" {
			t.Fatal("accepted invalid knowledge source identity")
		}
	}
}
