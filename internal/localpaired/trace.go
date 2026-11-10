package localpaired

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

const localLangfuseTraceEndpoint = "http://127.0.0.1:13000/api/public/otel/v1/traces"
const localLangfuseScoreEndpoint = "http://127.0.0.1:13000/api/public/scores"

func SaveAndExport(ctx context.Context, dir string, endpoint string, body []byte, post func(context.Context, string, []byte) error) (string, error) {
	path := filepath.Join(dir, "trace.bin")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	if endpoint == "" {
		endpoint = localLangfuseTraceEndpoint
	}
	if err := post(ctx, endpoint, body); err != nil {
		return path, err
	}
	return path, nil
}

func AttachScore(ctx context.Context, endpoint string, traceID string, score []byte, post func(context.Context, string, []byte) error) error {
	if endpoint == "" {
		endpoint = localLangfuseScoreEndpoint
	}
	body, err := json.Marshal(struct {
		TraceID string `json:"traceId"`
		Score   string `json:"score"`
	}{
		TraceID: traceID,
		Score:   string(score),
	})
	if err != nil {
		return err
	}
	return post(ctx, endpoint, body)
}
