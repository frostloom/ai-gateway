package knowledge

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func syntheticCorpus(t *testing.T) *Corpus {
	t.Helper()
	files := fixture(t, func(document map[string]any) {
		document["title"] = "余额与账户"
		document["aliases"] = []string{"钱包", "余额查询", "API Key"}
		document["paragraphs"] = []string{"当前现金余额需要实时查询，知识库不提供账户金额。", "API Key 仅在创建时展示，可在 API 接入页删除密钥。", "账户查询的价格和用量必须从业务工具读取。", "现金余额与月卡金额权益分开统计。"}
	})
	corpus, err := Load(files)
	if err != nil {
		t.Fatal(err)
	}
	return corpus
}

func TestTokenizerBusinessAndIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"请问我的退款怎么办", []string{"退款"}},
		{"取消套餐会退钱吗", []string{"退订", "退款"}},
		{"充值码与月卡", []string{"兑换码", "月卡"}},
		{"ＡＰＩ Key /v1/chat/completions", []string{"api", "key", "v1/chat/completions", "chat", "completions"}},
		{"tokens 和 API_Key", []string{"token", "api", "key"}},
		{"全新概念", []string{"全新", "新概", "概念"}},
	} {
		got := terms(tc.query)
		for _, term := range tc.want {
			if !got[term] {
				t.Errorf("%q missing %q in %v", tc.query, term, got)
			}
		}
	}
	if got := terms("请问 怎么 如何 这个 你好 123"); len(got) != 0 {
		t.Fatalf("generic terms became retrieval evidence: %v", got)
	}
}

func TestShortBusinessQueryAndTopThree(t *testing.T) {
	corpus := syntheticCorpus(t)
	result, err := corpus.Search("余额")
	if err != nil || result.Status != "ok" || len(result.Hits) != 3 {
		t.Fatalf("short business query: %+v %v", result, err)
	}
	if result.Hits[0].DocumentID != "KB-001" || result.Hits[0].Score <= 0 {
		t.Fatalf("invalid ranked result %+v", result)
	}
	encoded, _ := json.Marshal(result)
	if len(encoded) > MaxResultBytes {
		t.Fatal("result exceeds prompt budget")
	}
	for _, query := range []string{"请问如何", "外星天气", "发票怎么开", "最近有什么促销活动"} {
		result, err := corpus.Search(query)
		if err != nil || result.Status != "no_match" || len(result.Hits) != 0 {
			t.Fatalf("unsupported query %q returned evidence: %+v %v", query, result, err)
		}
	}
}

func TestSearchValidation(t *testing.T) {
	corpus := syntheticCorpus(t)
	for _, query := range []string{"", "  ", strings.Repeat("中", 257), string([]byte{0xff})} {
		if _, _, err := corpus.SearchDetailed(query); err == nil {
			t.Fatalf("accepted invalid query %q", query)
		}
	}
	if _, err := corpus.Search(strings.Repeat("中", 256)); err != nil {
		t.Fatal("rejected supported query bound", err)
	}
	for _, unavailable := range []*Corpus{nil, {}} {
		result, _, err := unavailable.SearchDetailed("余额")
		if err == nil || result.Status != "unavailable" {
			t.Fatal("uninitialized corpus did not report unavailable")
		}
	}
}

func TestSearchCacheNormalizationAndCopies(t *testing.T) {
	corpus := syntheticCorpus(t)
	first, info, err := corpus.SearchDetailed("  API   Key  ")
	if err != nil || info.CacheHit || len(first.Hits) == 0 {
		t.Fatalf("first lookup: %+v %+v %v", first, info, err)
	}
	first.Hits[0].Text = "caller changed text"
	first.Hits[0].Sources[0].Path = "caller-changed"
	second, info, err := corpus.SearchDetailed("api key")
	if err != nil || !info.CacheHit || second.Hits[0].Text == first.Hits[0].Text || second.Hits[0].Sources[0].Path == "caller-changed" {
		t.Fatalf("cache result mutated: %+v %+v %v", second, info, err)
	}
	second.Hits[0].Sources[0].Path = "changed-again"
	third, _, _ := corpus.SearchDetailed("api key")
	if third.Hits[0].Sources[0].Path == "changed-again" {
		t.Fatal("cache hit exposed mutable storage")
	}
	for key := range corpus.cache.entries {
		if len(key) != 64 || strings.Contains(key, "api") {
			t.Fatal("cache stores query text")
		}
	}
}

func TestSearchCacheBoundExpiryAndVersion(t *testing.T) {
	corpus := syntheticCorpus(t)
	corpus.cache.capacity = 2
	now := time.Unix(1000, 0)
	corpus.cache.now = func() time.Time { return now }
	corpus.Search("余额")
	corpus.Search("钱包")
	corpus.Search("余额") // Keep the most recently used entry.
	corpus.Search("密钥")
	if len(corpus.cache.entries) != 2 {
		t.Fatal("cache capacity exceeded")
	}
	if _, info, _ := corpus.SearchDetailed("钱包"); info.CacheHit {
		t.Fatal("least recently used entry survived eviction")
	}
	corpus.Search("余额")
	now = now.Add(cacheLifetime)
	if _, info, _ := corpus.SearchDetailed("余额"); info.CacheHit {
		t.Fatal("expired result reused")
	}
	second := syntheticCorpus(t)
	second.version = strings.Repeat("f", 64)
	second.cache = corpus.cache
	if _, info, _ := second.SearchDetailed("余额"); info.CacheHit {
		t.Fatal("cache reused a different corpus version")
	}
	if corpus.RetrievalVersion() != RetrievalVersion {
		t.Fatal("retrieval version missing")
	}
}

func TestSearchConcurrentResultsImmutable(t *testing.T) {
	corpus := syntheticCorpus(t)
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 20; n++ {
				result, _, err := corpus.SearchDetailed("余额")
				if err != nil || len(result.Hits) == 0 {
					t.Errorf("concurrent lookup: %+v %v", result, err)
					return
				}
				result.Hits[0].Sources[0].Path = "mutated"
			}
		}()
	}
	workers.Wait()
	result, _ := corpus.Search("余额")
	if result.Hits[0].Sources[0].Path == "mutated" {
		t.Fatal("concurrent caller changed public evidence")
	}
}

func TestCurrentPublicCorpusShortQueries(t *testing.T) {
	corpus, err := Load(os.DirFS("../../knowledge/platform"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ query, id string }{{"退款", "KB-009"}, {"充值", "KB-008"}, {"兑换码", "KB-008"}, {"月卡", "KB-004"}, {"余额", "KB-002"}, {"chat/completions", "KB-012"}, {"月卡怎么买", "KB-004"}, {"套餐价格", "KB-004"}, {"额度啥时候回来", "KB-005"}, {"五小时", "KB-005"}, {"超额自动扣款", "KB-005"}, {"token和钱包一样吗", "KB-002"}, {"取消套餐会退钱吗", "KB-006"}, {"API Key", "KB-012"}, {"删除密钥", "KB-012"}} {
		result, err := corpus.Search(tc.query)
		if err != nil || len(result.Hits) == 0 || result.Hits[0].DocumentID != tc.id {
			t.Errorf("%q expected %s: %+v %v", tc.query, tc.id, result, err)
		}
	}
}

func TestRetrievalScoresDeterministic(t *testing.T) {
	corpus, err := Load(os.DirFS("../../knowledge/platform"))
	if err != nil {
		t.Fatal(err)
	}
	query := "月卡余额额度恢复窗口价格模型现金用量token"
	expected := corpus.retrieve(terms(query), normalizeQuery(query))
	for i := 0; i < 200; i++ {
		if got := corpus.retrieve(terms(query), normalizeQuery(query)); !reflect.DeepEqual(got, expected) {
			t.Fatal("identical query produced different ranking or scores")
		}
	}
}

func TestCurrentPublicCorpusDeletionParagraph(t *testing.T) {
	corpus, err := Load(os.DirFS("../../knowledge/platform"))
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"删除密钥", "如何删除我的API密钥"} {
		result, err := corpus.Search(query)
		if err != nil || len(result.Hits) == 0 {
			t.Fatalf("%q unavailable: %+v %v", query, result, err)
		}
		if first := result.Hits[0]; first.ChunkID != "KB-012:4" || !strings.Contains(first.Text, "密钥支持启用、停用与删除") {
			t.Errorf("%q did not prioritize actual deletion instructions: %+v", query, result.Hits)
		}
	}
}
