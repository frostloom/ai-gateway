package knowledge

import (
	"os"
	"strings"
	"testing"
)

func TestResidentContextKeepsAllFactsAndBound(t *testing.T) {
	c, e := Load(os.DirFS("../../knowledge/platform"))
	if e != nil {
		t.Fatal(e)
	}
	text, hits, e := c.ResidentContext()
	if e != nil {
		t.Fatal(e)
	}
	if len(hits) != 12 || len(text) > 4096 || !strings.Contains(text, "[KB-012]") {
		t.Fatal("incomplete or oversized resident context")
	}
	for _, h := range hits {
		if !strings.Contains(text, h.Text) || digest([]byte(h.Text)) != h.SHA256 {
			t.Fatal("facts rewritten")
		}
	}
	hits[0].Sources[0].Path = "MUTATED"
	_, again, e := c.ResidentContext()
	if e != nil || again[0].Sources[0].Path == "MUTATED" {
		t.Fatal("mutable corpus escaped")
	}
}
func TestResidentContextRejectsOversizedCorpus(t *testing.T) {
	c := &Corpus{version: strings.Repeat("a", 64), docs: []Document{{ID: "KB-001", Paragraphs: []string{strings.Repeat("长", 1400)}}}}
	if _, _, e := c.ResidentContext(); e == nil {
		t.Fatal("over budget context accepted")
	}
}
