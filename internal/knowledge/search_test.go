package knowledge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func testCorpus(t *testing.T) *Corpus {
	t.Helper()
	c, e := Load(os.DirFS("../../knowledge/platform"))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestSearchChineseParaphrases(t *testing.T) {
	c := testCorpus(t)
	for _, tc := range []struct{ q, id string }{{"额度啥时候回来", "KB-005"}, {"取消套餐会退钱吗", "KB-006"}, {"token和钱包一样吗", "KB-002"}} {
		r, e := c.Search(tc.q)
		if e != nil || len(r.Hits) == 0 || r.Hits[0].DocumentID != tc.id {
			t.Fatalf("%s: %+v %v", tc.q, r, e)
		}
	}
}
func TestSearchUnknown(t *testing.T) {
	c := testCorpus(t)
	for _, q := range []string{"发票怎么开", "最近有什么促销活动", "外星天气"} {
		r, e := c.Search(q)
		if e != nil || r.Status != "no_match" || len(r.Hits) != 0 {
			t.Fatalf("unrelated: %s %+v %v", q, r, e)
		}
	}
}
func TestSearchAPITerm(t *testing.T) {
	r, e := testCorpus(t).Search("chat/completions")
	if e != nil || len(r.Hits) == 0 || r.Hits[0].DocumentID != "KB-012" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestSearchBounds(t *testing.T) {
	c := testCorpus(t)
	for _, q := range []string{" ", strings.Repeat("中", 257), string([]byte{255})} {
		if _, e := c.Search(q); e == nil {
			t.Fatal("invalid query accepted")
		}
	}
	if _, e := c.Search(strings.Repeat("中", 256)); e != nil {
		t.Fatal(e)
	}
	r, e := c.Search("套餐额度退款输入输出")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r)
	if len(b) > 4096 || len(r.Hits) > 3 {
		t.Fatal("oversized result")
	}
	for _, h := range r.Hits {
		if len(h.Text) > 768 || !utf8.ValidString(h.Text) {
			t.Fatal("broken text")
		}
	}
}
func TestSearchDeterministic(t *testing.T) {
	c := testCorpus(t)
	r, _ := c.Search("套餐额度")
	a, _ := json.Marshal(r)
	for i := 0; i < 20; i++ {
		r, _ = c.Search("套餐额度")
		b, _ := json.Marshal(r)
		if string(a) != string(b) {
			t.Fatal("unstable search")
		}
	}
	if len(r.Hits) > 0 {
		r.Hits[0].Sources[0].Path = "mutated"
		again, _ := c.Search("套餐额度")
		if again.Hits[0].Sources[0].Path == "mutated" {
			t.Fatal("caller mutated corpus")
		}
	}
}
func TestSearchNegation(t *testing.T) {
	r, e := testCorpus(t).Search("超额自动扣款")
	if e != nil || len(r.Hits) == 0 || r.Hits[0].DocumentID != "KB-005" || !strings.Contains(r.Hits[0].Text, "不会自动改扣现金") {
		t.Fatalf("lost exception: %+v %v", r, e)
	}
}
