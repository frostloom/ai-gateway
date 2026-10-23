package jev

import (
	"errors"
	"fmt"
)

// Failure retains only bounded diagnostic fields. Never retain upstream body,
// request URL, headers or raw transport error, which may contain credentials.
// Nil UpstreamAttempted means an untyped evaluator supplied no such evidence.
type Failure struct {
	Kind              string  `json:"kind"`
	TransportCause    string  `json:"transport_cause,omitempty"`
	HTTPStatus        int     `json:"http_status,omitempty"`
	UpstreamAttempted *bool   `json:"upstream_attempted,omitempty"`
	LatencyMS         float64 `json:"latency_ms,omitempty"`
}

func (f *Failure) Error() string {
	return fmt.Sprintf("jev failure: %s (HTTP %d)", f.Kind, f.HTTPStatus)
}

func NewFailure(kind string, status int, attempted bool) *Failure {
	return &Failure{Kind: kind, HTTPStatus: status, UpstreamAttempted: &attempted}
}

func FailureDetails(err error) Failure {
	var f *Failure
	if errors.As(err, &f) && f != nil {
		return *f
	}
	return Failure{Kind: "unknown"}
}
