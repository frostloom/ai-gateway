package observability

import (
	"context"
	"strings"
	"testing"
)

func TestKnowledgeCapture(t *testing.T) {
	for _, captureOn := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[captureOn], func(t *testing.T) {
			s, c := receiver(t)
			defer s.Close()
			cfg := enabled(s.URL)
			cfg.CaptureContent = captureOn
			o, e := New(context.Background(), cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer o.Shutdown(context.Background())
			ctx, tr := o.StartTurn(context.Background(), "model")
			_, op := Start(ctx, "knowledge.chunk", "span")
			op.Knowledge(KnowledgeMetadata{CorpusVersion: strings.Repeat("a", 64), DocumentID: "KB-001", ChunkID: "KB-001:1", SourceSHA256: strings.Repeat("b", 64), Stage: "retrieved", HitCount: 1})
			op.KnowledgeContent(strings.Repeat("中", 256))
			op.End(nil)
			tr.End(200, Summary{})
			if e = o.ForceFlush(context.Background()); e != nil {
				t.Fatal(e)
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			found := false
			for _, s := range c.spans {
				if s.Name != "knowledge.chunk" {
					continue
				}
				found = true
				a := attrs(s)
				output := a["langfuse.observation.output"]
				if captureOn && !strings.Contains(output, strings.Repeat("中", 256)) {
					t.Fatal("bounded chunk truncated")
				}
				if !captureOn && output != "" {
					t.Fatal("capture disabled leaked body")
				}
				if a["langfuse.observation.metadata.knowledge_stage"] != "retrieved" {
					t.Fatal("missing metadata")
				}
			}
			if !found {
				t.Fatal("no observation")
			}
		})
	}
}
func TestKnowledgeContentRedaction(t *testing.T) {
	s, c := receiver(t)
	defer s.Close()
	cfg := enabled(s.URL)
	cfg.CaptureContent = true
	o, e := New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Shutdown(context.Background())
	ctx, tr := o.StartTurn(context.Background(), "model")
	_, op := Start(ctx, "knowledge.chunk", "span")
	op.KnowledgeContent("password=secretvalue user@example.com")
	op.End(nil)
	tr.End(200, Summary{})
	o.ForceFlush(context.Background())
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.spans {
		if s.Name == "knowledge.chunk" {
			a := attrs(s)
			if strings.Contains(a["langfuse.observation.output"], "secretvalue") || a["langfuse.observation.metadata.knowledge_redacted"] != "true" {
				t.Fatal("missing redaction marker")
			}
		}
	}
}
