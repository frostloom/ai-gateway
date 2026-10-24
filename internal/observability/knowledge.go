package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go.opentelemetry.io/otel/attribute"
	"regexp"
	"strconv"
	"strings"
)

type KnowledgeMetadata struct {
	CorpusVersion, DocumentID, ChunkID, SourceSHA256, Stage string
	HitCount, RequestOrdinal                                int
}

var knowledgeID = regexp.MustCompile(`^KB-[0-9]{3}(?::[1-9][0-9]{0,3})?$`)

func hexDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func (o *Operation) Knowledge(k KnowledgeMetadata) {
	if o == nil {
		return
	}
	switch k.Stage {
	case "retrieved", "no_match", "unavailable", "included_in_request", "stale_in_request", "provider_response", "provider_error":
	default:
		return
	}
	if !hexDigest(k.CorpusVersion) || k.HitCount < 0 || k.HitCount > 3 || k.RequestOrdinal < 0 {
		return
	}
	if k.DocumentID != "" && (!knowledgeID.MatchString(k.DocumentID) || !knowledgeID.MatchString(k.ChunkID) || !hexDigest(k.SourceSHA256)) {
		return
	}
	o.span.SetAttributes(meta("knowledge_corpus_version", k.CorpusVersion), meta("knowledge_document_id", k.DocumentID), meta("knowledge_chunk_id", k.ChunkID), meta("knowledge_source_sha256", k.SourceSHA256), meta("knowledge_stage", k.Stage), meta("knowledge_hit_count", strconv.Itoa(k.HitCount)), meta("knowledge_request_ordinal", strconv.Itoa(k.RequestOrdinal)))
}

// KnowledgeContent preserves the existing global content switch and sanitizer.
func (o *Operation) KnowledgeContent(text string) {
	if o == nil || o.observer == nil {
		return
	}
	on := o.observer.cfg.CaptureContent
	o.span.SetAttributes(meta("knowledge_content_capture", strconv.FormatBool(on)))
	if !on {
		return
	}
	sanitized := redact(text)
	encoded, _ := json.Marshal(sanitized)
	clipped := len(text) > 2048 || len(encoded) > contentLimit
	if len(encoded) > contentLimit {
		preview := clip(string(encoded), 1500)
		encoded, _ = json.Marshal(map[string]any{"truncated": true, "preview": preview})
	}
	sum := sha256.Sum256(encoded)
	o.span.SetAttributes(meta("knowledge_redacted", strconv.FormatBool(sanitized != text && strings.Contains(sanitized, "[REDACTED]"))), meta("knowledge_clipped", strconv.FormatBool(clipped)), meta("knowledge_display_sha256", hex.EncodeToString(sum[:])), attribute.String("langfuse.observation.output", string(encoded)))
}
