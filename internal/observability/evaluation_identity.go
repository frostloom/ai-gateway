package observability

import (
	"crypto/sha256"
	"encoding/hex"
)

// EvaluationIdentity is a read-only, credential-free identity for guarded runs.
// It is configuration evidence; it does not certify delivery or authorization.
type EvaluationIdentity struct {
	ObservationReceiptBoundary string  `json:"observation_receipt_boundary,omitempty"`
	SpoolRootSHA256            string  `json:"spool_root_sha256,omitempty"`
	SpoolRunID                 string  `json:"spool_run_id,omitempty"`
	SpoolMaxBytes              int64   `json:"spool_max_bytes,omitempty"`
	Enabled                    bool    `json:"enabled"`
	CaptureContent             bool    `json:"capture_content"`
	SampleRate                 float64 `json:"sample_rate"`
	Environment                string  `json:"environment"`
	Release                    string  `json:"release"`
	ExportTimeoutMS            int64   `json:"export_timeout_ms"`
	QueueSize                  int     `json:"queue_size"`
	EndpointSHA256             string  `json:"endpoint_sha256"`
	ProjectSHA256              string  `json:"project_sha256"`
}

func (o *Observer) EvaluationIdentity() EvaluationIdentity {
	if o == nil || !o.cfg.Enabled || o.provider == nil || o.tracer == nil || o.processor == nil || o.registry == nil {
		return EvaluationIdentity{}
	}
	digest := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	identity := EvaluationIdentity{Enabled: true, CaptureContent: o.cfg.CaptureContent, SampleRate: o.cfg.SampleRate, Environment: o.cfg.Environment, Release: o.cfg.Release, ExportTimeoutMS: o.cfg.ExportTimeout.Milliseconds(), QueueSize: o.cfg.QueueSize, EndpointSHA256: digest(o.cfg.Endpoint), ProjectSHA256: digest(o.cfg.PublicKey)}
	if o.durable != nil && o.cfg.DurableSpool != nil {
		identity.ObservationReceiptBoundary = "durable_local"
		identity.SpoolRootSHA256 = digest(o.cfg.DurableSpool.Root)
		identity.SpoolRunID = o.cfg.DurableSpool.RunID
		identity.SpoolMaxBytes = o.cfg.DurableSpool.MaxBytes
	}
	return identity
}
