package knowledge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func terms(s string) map[string]bool {
	out := map[string]bool{}
	var han []rune
	var word strings.Builder
	flushHan := func() {
		for i := 1; i < len(han); i++ {
			out[string(han[i-1:i+1])] = true
		}
		han = nil
	}
	flushWord := func() {
		if word.Len() > 1 {
			out[word.String()] = true
		}
		word.Reset()
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			flushHan()
			word.WriteRune(r)
		default:
			flushHan()
			flushWord()
		}
	}
	flushHan()
	flushWord()
	return out
}
func (c *Corpus) Search(query string) (Result, error) {
	result := Result{Status: "unavailable", Hits: []Hit{}}
	if c == nil {
		return result, fmt.Errorf("knowledge unavailable")
	}
	result.CorpusVersion = c.version
	if !utf8.ValidString(query) || strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 256 {
		return result, fmt.Errorf("query must have 1–256 valid characters")
	}
	q := terms(query)
	result.Status = "no_match"
	if len(q) == 0 {
		return result, nil
	}
	for _, d := range c.docs {
		title := terms(d.Title)
		aliases := terms(strings.Join(d.Aliases, " "))
		for i, text := range d.Paragraphs {
			body := terms(text)
			matched, weight := 0, 0
			english := false
			for term := range q {
				w := 0
				if body[term] {
					w = 1
				}
				if aliases[term] {
					w = 2
				}
				if title[term] {
					w = 3
				}
				if w > 0 {
					matched++
					weight += w
					if len([]rune(term)) > 1 && !unicode.Is(unicode.Han, []rune(term)[0]) {
						english = true
					}
				}
			}
			if (matched < 2 && !english) || float64(matched)/float64(len(q)) < .20 {
				continue
			}
			result.Hits = append(result.Hits, Hit{DocumentID: d.ID, ChunkID: fmt.Sprintf("%s:%d", d.ID, i+1), Title: d.Title, Version: d.Version, Scope: d.Scope, Sources: append([]Source(nil), d.Sources...), Text: text, SHA256: digest([]byte(text)), Paragraph: i + 1, Score: float64(weight) / float64(3*len(q))})
		}
	}
	sort.Slice(result.Hits, func(i, j int) bool {
		a, b := result.Hits[i], result.Hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.DocumentID != b.DocumentID {
			return a.DocumentID < b.DocumentID
		}
		return a.Paragraph < b.Paragraph
	})
	if len(result.Hits) > 3 {
		result.Hits = result.Hits[:3]
	}
	for len(result.Hits) > 0 {
		result.Status = "ok"
		b, _ := json.Marshal(result)
		if len(b) <= MaxResultBytes {
			break
		}
		result.Hits = result.Hits[:len(result.Hits)-1]
	}
	if len(result.Hits) == 0 {
		result.Status = "no_match"
	}
	return result, nil
}
