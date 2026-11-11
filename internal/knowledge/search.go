package knowledge

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const RetrievalVersion = "zh-domain-bigram-additive-v2"

type SearchInfo struct {
	CacheHit bool
}

type indexedChunk struct {
	hit     Hit
	weights map[string]float64
	anchors map[string]bool
	aliases map[string]bool
}

func (c *Corpus) RetrievalVersion() string { return RetrievalVersion }

func (c *Corpus) buildIndex() {
	frequencies := map[string]int{}
	for _, document := range c.docs {
		for i, text := range document.Paragraphs {
			chunk := indexedChunk{
				hit:     Hit{DocumentID: document.ID, ChunkID: fmt.Sprintf("%s:%d", document.ID, i+1), Title: document.Title, Version: document.Version, Scope: document.Scope, Sources: append([]Source(nil), document.Sources...), Text: text, SHA256: digest([]byte(text)), Paragraph: i + 1},
				weights: map[string]float64{}, anchors: map[string]bool{}, aliases: map[string]bool{},
			}
			for _, alias := range document.Aliases {
				chunk.aliases[normalizeQuery(alias)] = true
			}
			for term := range terms(text) {
				chunk.weights[term] = 1
			}
			// Add metadata evidence instead of overwriting body matches: each
			// paragraph must retain its own relevance within the document.
			for term := range terms(strings.Join(document.Aliases, " ")) {
				chunk.weights[term] += 2
				chunk.anchors[term] = true
			}
			for term := range terms(document.Title) {
				chunk.weights[term] += 3
				chunk.anchors[term] = true
			}
			for term := range chunk.weights {
				frequencies[term]++
			}
			c.index = append(c.index, chunk)
		}
	}
	c.idf = make(map[string]float64, len(frequencies))
	for term, frequency := range frequencies {
		c.idf[term] = math.Log(1 + (float64(len(c.index)-frequency)+.5)/(float64(frequency)+.5))
	}
}

func (c *Corpus) Search(query string) (Result, error) {
	result, _, err := c.SearchDetailed(query)
	return result, err
}

func (c *Corpus) SearchDetailed(query string) (Result, SearchInfo, error) {
	result := Result{Status: "unavailable", Hits: []Hit{}}
	if c == nil || c.cache == nil {
		return result, SearchInfo{}, fmt.Errorf("knowledge unavailable")
	}
	result.CorpusVersion = c.version
	if !utf8.ValidString(query) || strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 256 {
		return result, SearchInfo{}, fmt.Errorf("query must have 1–256 valid characters")
	}
	key := digest([]byte(c.version + "\x00" + RetrievalVersion + "\x00" + normalizeQuery(query)))
	if hit, ok := c.cache.get(key); ok {
		return hit, SearchInfo{CacheHit: true}, nil
	}
	result = c.retrieve(terms(query), normalizeQuery(query))
	c.cache.put(key, result)
	return cloneResult(result), SearchInfo{}, nil
}

func (c *Corpus) retrieve(query map[string]bool, normalized string) Result {
	result := Result{Status: "no_match", CorpusVersion: c.version, Hits: []Hit{}}
	if len(query) == 0 {
		return result
	}
	queryTerms := make([]string, 0, len(query))
	for term := range query {
		queryTerms = append(queryTerms, term)
	}
	sort.Strings(queryTerms) // Stable floating-point accumulation and ranking.
	for _, chunk := range c.index {
		matched := 0
		anchored := false
		weighted := 0.0
		possible := 0.0
		for _, term := range queryTerms {
			idf := c.idf[term]
			if idf == 0 {
				idf = math.Log(1 + float64(len(c.index)))
			}
			possible += 6 * idf
			if weight := chunk.weights[term]; weight > 0 {
				matched++
				weighted += weight * idf
				anchored = anchored || chunk.anchors[term] || isDomainTerm(term) || isLatinTerm(term)
			}
		}
		if matched == 0 || (matched < 2 && !anchored) || float64(matched)/float64(len(query)) < .20 {
			continue
		}
		hit := chunk.hit
		hit.Score = .8 * weighted / possible
		// Reviewed aliases identify exact user intents; prefer them over a title
		// that only mentions the same term in passing. Keep scores in [0, 1].
		if chunk.aliases[normalized] {
			hit.Score += .2
		}
		result.Hits = append(result.Hits, hit)
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
		encoded, _ := json.Marshal(result)
		if len(encoded) <= MaxResultBytes {
			break
		}
		result.Hits = result.Hits[:len(result.Hits)-1]
	}
	if len(result.Hits) == 0 {
		result.Status = "no_match"
	}
	return result
}

func isDomainTerm(term string) bool {
	_, ok := domainTerms[term]
	return ok
}

func isLatinTerm(term string) bool {
	for _, r := range term {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
