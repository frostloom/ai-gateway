// Package knowledge loads immutable, reviewed public platform documentation.
package knowledge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxDocuments = 100
const MaxFileBytes = 64 * 1024
const MaxCorpusBytes = 2 * 1024 * 1024
const MaxChunkBytes = 768
const MaxResultBytes = 4096

type Source struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
}
type Document struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Version    string   `json:"version"`
	Status     string   `json:"status"`
	ReviewedAt string   `json:"reviewed_at"`
	Scope      string   `json:"scope"`
	Aliases    []string `json:"aliases"`
	Sources    []Source `json:"sources"`
	Paragraphs []string `json:"paragraphs"`
}
type Hit struct {
	DocumentID string   `json:"document_id"`
	ChunkID    string   `json:"chunk_id"`
	Title      string   `json:"title"`
	Version    string   `json:"version"`
	Scope      string   `json:"scope"`
	Sources    []Source `json:"sources"`
	Text       string   `json:"text"`
	SHA256     string   `json:"sha256"`
	Paragraph  int      `json:"paragraph"`
	Score      float64  `json:"score"`
}
type Result struct {
	Status        string `json:"status"`
	CorpusVersion string `json:"corpus_version"`
	Hits          []Hit  `json:"hits"`
}
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Schema string `json:"schema"`
	Files  []File `json:"files"`
}
type Corpus struct {
	docs    []Document
	version string
}

func (c *Corpus) Version() string {
	if c == nil {
		return ""
	}
	return c.version
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func decode(raw []byte, v any) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

var filename = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.json$`)
var documentID = regexp.MustCompile(`^KB-[0-9]{3}$`)

func bounded(s string, n int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= n && utf8.ValidString(s)
}
func validate(d Document) error {
	if !documentID.MatchString(d.ID) || !bounded(d.Title, 128) || !bounded(d.Version, 32) || d.Status != "approved" || !bounded(d.Scope, 256) {
		return fmt.Errorf("invalid document metadata")
	}
	if _, e := time.Parse("2006-01-02", d.ReviewedAt); e != nil {
		return fmt.Errorf("invalid review date")
	}
	if len(d.Sources) == 0 || len(d.Sources) > 4 || len(d.Paragraphs) == 0 || len(d.Aliases) > 24 {
		return fmt.Errorf("invalid document fields")
	}
	for _, s := range d.Sources {
		if !fs.ValidPath(s.Path) || path.Clean(s.Path) != s.Path || !bounded(s.Path, 160) || !bounded(s.Symbol, 80) {
			return fmt.Errorf("invalid source")
		}
	}
	for _, s := range d.Aliases {
		if !bounded(s, 128) {
			return fmt.Errorf("invalid alias")
		}
	}
	for _, s := range d.Paragraphs {
		if !bounded(s, MaxChunkBytes) {
			return fmt.Errorf("paragraph must be nonempty and <=768 bytes")
		}
	}
	return nil
}
func read(fsys fs.FS, name string, limit int) ([]byte, error) {
	f, e := fsys.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > int64(limit) {
		return nil, fmt.Errorf("invalid file size/type: %s", name)
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if e != nil || len(b) > limit {
		return nil, fmt.Errorf("read failed/oversize: %s", name)
	}
	return b, nil
}
func scan(fsys fs.FS) (Manifest, []Document, error) {
	m := Manifest{Schema: "platform-knowledge.v1", Files: []File{}}
	var docs []Document
	entries, e := fs.ReadDir(fsys, ".")
	if e != nil {
		return m, nil, e
	}
	seen := map[string]bool{}
	total := 0
	for _, entry := range entries {
		if entry.Name() == "manifest.json" {
			if !entry.Type().IsRegular() {
				return m, nil, fmt.Errorf("invalid manifest type")
			}
			continue
		}
		if !filename.MatchString(entry.Name()) || !entry.Type().IsRegular() {
			return m, nil, fmt.Errorf("unrecognized file: %s", entry.Name())
		}
		if len(docs) >= MaxDocuments {
			return m, nil, fmt.Errorf("too many documents")
		}
		b, e := read(fsys, entry.Name(), MaxFileBytes)
		if e != nil {
			return m, nil, e
		}
		total += len(b)
		if total > MaxCorpusBytes {
			return m, nil, fmt.Errorf("corpus too large")
		}
		var d Document
		if e = decode(b, &d); e != nil {
			return m, nil, fmt.Errorf("document %s: %w", entry.Name(), e)
		}
		if e = validate(d); e != nil {
			return m, nil, fmt.Errorf("document %s: %w", entry.Name(), e)
		}
		if seen[d.ID] {
			return m, nil, fmt.Errorf("duplicate document ID")
		}
		seen[d.ID] = true
		docs = append(docs, d)
		m.Files = append(m.Files, File{entry.Name(), digest(b)})
	}
	if len(docs) == 0 {
		return m, nil, fmt.Errorf("empty corpus")
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	return m, docs, nil
}

// GenerateManifest validates all source documents. It never approves or edits them.
func GenerateManifest(fsys fs.FS) ([]byte, error) {
	m, _, e := scan(fsys)
	if e != nil {
		return nil, e
	}
	return json.MarshalIndent(m, "", "  ")
}
func Load(fsys fs.FS) (*Corpus, error) {
	raw, e := read(fsys, "manifest.json", 32*1024)
	if e != nil {
		return nil, e
	}
	var declared Manifest
	if e = decode(raw, &declared); e != nil {
		return nil, e
	}
	actual, docs, e := scan(fsys)
	if e != nil {
		return nil, e
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(declared)
	if !bytes.Equal(a, b) {
		return nil, fmt.Errorf("manifest mismatch")
	}
	return &Corpus{docs: docs, version: digest(a)}, nil
}
