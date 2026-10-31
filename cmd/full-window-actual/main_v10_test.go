package main

import (
	"bytes"
	"context"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"strings"
	"testing"
)

func TestActualV10DedicatedModeDenials(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"--prepare-v10", "--manifest", "/synthetic/missing"}, "actual_v10_preparation_rejected"},
		{[]string{"--prepare-v10", "--prepare-v9"}, "invalid_start"},
		{[]string{"--prepare-v10", "--serve-actual"}, "invalid_start"},
		{[]string{"--prepare-v10", "--private-settings", "/do-not-read"}, "invalid_start"},
		{[]string{"--epoch-v10", "--epoch-v9", "--serve-actual", "--authorization", "/do-not-read"}, "invalid_start"},
		{[]string{"--epoch-v10", "--serve-actual", "--manifest", "/synthetic/missing", "--authorization", "/do-not-read", "--private-settings", "/do-not-read"}, "actual_v10_authorization_rejected"},
	} {
		var out bytes.Buffer
		code := runActualCLI(context.Background(), tc.args, io.NopCloser(strings.NewReader("")), &out, nil, func(context.Context, *taskbench.FullWindowActualReady, string) (actualSession, error) {
			t.Fatal("default denial reached session/credentials")
			return nil, nil
		})
		if code != 1 || !strings.Contains(out.String(), tc.code) {
			t.Fatalf("%v got%d %s", tc.args, code, out.String())
		}
	}
}
