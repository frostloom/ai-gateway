package knowledgepilot

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"path/filepath"
	"sort"
)

// WriteReviewPackage shuffles opaque IDs and withholds the arm mapping. Tool
// traces may reveal knowledge use, so this is best-effort masking, not blinding.
func WriteReviewPackage(dir string, r Recording) error {
	type item struct {
		ID                          string             `json:"review_id"`
		Case                        knowledgeeval.Case `json:"case"`
		Reply                       string             `json:"reply"`
		Trace                       json.RawMessage    `json:"trace"`
		Before, After               json.RawMessage
		PendingBefore, PendingAfter bool
	}
	type binding struct{ TaskID, ReplySHA string }
	cases := map[string]knowledgeeval.Case{}
	for _, c := range r.Suite {
		cases[c.ID] = c
	}
	var items []item
	mapping := map[string]binding{}
	for _, t := range r.Tasks {
		if t.HTTPStatus != 200 {
			continue
		}
		var raw [16]byte
		if _, e := rand.Read(raw[:]); e != nil {
			return e
		}
		id := hex.EncodeToString(raw[:])
		if _, ok := mapping[id]; ok {
			return fmt.Errorf("review ID collision")
		}
		mapping[id] = binding{t.Identity, SHA([]byte(t.Reply))}
		items = append(items, item{ID: id, Case: cases[t.CaseID], Reply: t.Reply, Trace: t.Trace, Before: t.Before, After: t.After, PendingBefore: t.PendingBefore, PendingAfter: t.PendingAfter})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, v := range []struct {
		name  string
		value any
	}{{"review-map.json", mapping}, {"review-package.json", items}} {
		b, e := json.MarshalIndent(v.value, "", "  ")
		if e != nil {
			return e
		}
		if e = writeExclusive(filepath.Join(dir, v.name), append(b, '\n')); e != nil {
			return e
		}
	}
	return nil
}
