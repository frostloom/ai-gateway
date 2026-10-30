package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestActualCLIV9DedicatedModesRejectBeforeOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code string
	}{
		{"prepare_missing", []string{"--prepare-v9", "--manifest", "/synthetic/missing"}, "actual_v9_preparation_rejected"},
		{"prepare_settings", []string{"--prepare-v9", "--manifest", "/synthetic/missing", "--private-settings", "/do-not-read"}, "invalid_start"},
		{"prepare_both", []string{"--prepare-v8", "--prepare-v9", "--manifest", "/synthetic/missing"}, "invalid_start"},
		{"prepare_serve", []string{"--prepare-v9", "--serve-actual", "--manifest", "/synthetic/missing"}, "invalid_start"},
		{"epoch_without_auth", []string{"--epoch-v9", "--serve-actual", "--manifest", "/synthetic/missing", "--private-settings", "/do-not-read"}, "invalid_start"},
		{"epoch_check", []string{"--epoch-v9", "--check", "--manifest", "/synthetic/missing"}, "invalid_start"},
		{"auth_missing", []string{"--epoch-v9", "--serve-actual", "--authorization", "/synthetic/missing", "--manifest", "/synthetic/missing", "--private-settings", "/do-not-read"}, "actual_v9_authorization_rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, opens := 0, 0
			check, open := actualCLIFactories(&fakeActualSession{}, &checks, &opens, false)
			var out bytes.Buffer
			code := runActualCLI(context.Background(), tc.args, io.NopCloser(strings.NewReader("")), &out, check, open)
			if code != 1 || !strings.Contains(out.String(), tc.code) || checks != 0 || opens != 0 {
				t.Fatalf("v9 mode escaped: %d %s checks=%d opens=%d", code, out.String(), checks, opens)
			}
		})
	}
}
