package knowledge

import (
	"fmt"
	"strings"
)

const ResidentMaxBytes = 4096
const ResidentMaxChunks = 16

// ResidentContext compiles the complete small corpus, never a silent subset.
// The byte bound includes source labels and separators. Facts are not rewritten.
func (c *Corpus) ResidentContext() (string, []Hit, error) {
	if c == nil || c.version == "" {
		return "", nil, fmt.Errorf("knowledge unavailable")
	}
	var b strings.Builder
	var hits []Hit
	for _, d := range c.docs {
		for i, text := range d.Paragraphs {
			hits = append(hits, Hit{DocumentID: d.ID, ChunkID: fmt.Sprintf("%s:%d", d.ID, i+1), Title: d.Title, Version: d.Version, Scope: d.Scope, Sources: append([]Source(nil), d.Sources...), Text: text, SHA256: digest([]byte(text)), Paragraph: i + 1})
			fmt.Fprintf(&b, "[%s] %s\n", d.ID, text)
			if len(hits) > ResidentMaxChunks || b.Len() > ResidentMaxBytes {
				return "", nil, fmt.Errorf("corpus exceeds resident context budget")
			}
		}
	}
	if len(hits) == 0 {
		return "", nil, fmt.Errorf("empty resident knowledge")
	}
	return b.String(), hits, nil
}
