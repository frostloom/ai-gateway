package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// Catches accepting untrusted release data or silently rewriting its manifest.
func fixture(t *testing.T, changes func(map[string]any)) fstest.MapFS {
	t.Helper()
	d := map[string]any{"id": "KB-001", "title": "测试规则", "version": "1", "status": "approved", "reviewed_at": "2026-10-24", "scope": "647ba9f demo", "aliases": []string{"测试"}, "sources": []any{map[string]any{"path": "internal/agent/prompt.go", "symbol": "systemPrompt"}}, "paragraphs": []string{"测试规则：模拟支付不会进行真实扣款。"}}
	if changes != nil {
		changes(d)
	}
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	m, _ := json.Marshal(map[string]any{"schema": "platform-knowledge.v1", "files": []any{map[string]any{"path": "001.json", "sha256": hex.EncodeToString(sum[:])}}})
	return fstest.MapFS{"001.json": &fstest.MapFile{Data: raw}, "manifest.json": &fstest.MapFile{Data: m}}
}
func TestLoadValidCorpus(t *testing.T) {
	c, e := Load(os.DirFS("../../knowledge/platform"))
	if e != nil {
		t.Fatal(e)
	}
	if len(c.docs) != 12 || len(c.Version()) != 64 {
		t.Fatalf("invalid release: %d %s", len(c.docs), c.Version())
	}
}
func TestLoadRejectsTamper(t *testing.T) {
	f := fixture(t, nil)
	f["001.json"].Data = append(f["001.json"].Data, ' ')
	if _, e := Load(f); e == nil {
		t.Fatal("accepted tampered release")
	}
}
func TestLoadValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(map[string]any)
	}{
		{"draft", func(d map[string]any) { d["status"] = "draft" }},
		{"unknown", func(d map[string]any) { d["surprise"] = true }},
		{"empty", func(d map[string]any) { d["paragraphs"] = []string{" "} }},
		{"date", func(d map[string]any) { d["reviewed_at"] = "yesterday" }},
		{"long paragraph", func(d map[string]any) { d["paragraphs"] = []string{strings.Repeat("中", 257)} }},
		{"too large", func(d map[string]any) { d["title"] = strings.Repeat("x", 65537) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := Load(fixture(t, tc.mut)); e == nil {
				t.Fatal("accepted invalid document")
			}
		})
	}
	for _, name := range []string{"missing manifest", "bad json", "unlisted", "symlink", "duplicate id", "traversal", "too many", "total size"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, nil)
			switch name {
			case "missing manifest":
				delete(f, "manifest.json")
			case "bad json":
				f["manifest.json"].Data = []byte("{")
			case "unlisted":
				f["extra.json"] = &fstest.MapFile{Data: []byte("{}")}
			case "symlink":
				f["001.json"].Mode = os.ModeSymlink
			case "duplicate id":
				f["002.json"] = &fstest.MapFile{Data: append([]byte(nil), f["001.json"].Data...)}
				m, _ := GenerateManifest(f)
				f["manifest.json"].Data = m
			case "traversal":
				f["manifest.json"].Data = []byte(`{"schema":"platform-knowledge.v1","files":[{"path":"../001.json","sha256":"abc"}]}`)
			case "too many":
				for i := 0; i < 101; i++ {
					f[string(rune('A'+i))+".json"] = &fstest.MapFile{Data: f["001.json"].Data}
				}
				m, e := GenerateManifest(f)
				if e != nil {
					return
				}
				f["manifest.json"].Data = m
			case "total size":
				f["huge.json"] = &fstest.MapFile{Data: []byte(strings.Repeat("x", 2*1024*1024+1))}
			}
			if _, e := Load(f); e == nil {
				t.Fatal("accepted invalid corpus")
			}
		})
	}
}
func TestManifestIsReadOnlyCheck(t *testing.T) {
	f := fixture(t, nil)
	before := string(f["001.json"].Data)
	a, e := GenerateManifest(f)
	if e != nil {
		t.Fatal(e)
	}
	b, e := GenerateManifest(f)
	if e != nil || string(a) != string(b) || before != string(f["001.json"].Data) {
		t.Fatal("unstable or mutated generation")
	}
}

func TestParagraphLimits(t *testing.T) {
	f := fixture(t, func(d map[string]any) { d["paragraphs"] = []string{strings.Repeat("中", 256)} })
	if _, e := Load(f); e != nil {
		t.Fatal(e)
	}
	f = fixture(t, func(d map[string]any) { d["paragraphs"] = []string{strings.Repeat("中", 257)} })
	if _, e := Load(f); e == nil {
		t.Fatal("accepted oversized intact paragraph")
	}
}
