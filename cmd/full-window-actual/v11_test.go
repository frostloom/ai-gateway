package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActualCLIV11PreparationRemainsReadOnly(t *testing.T) {
	var out bytes.Buffer
	if code := runActualCLI(context.Background(), []string{"--prepare-v11", "--manifest", "/not-frozen"}, nil, &out, nil, nil); code != 1 || !strings.Contains(out.String(), "actual_v11_preparation_rejected") {
		t.Fatalf("%d %s", code, out.String())
	}
}
func TestActualCLIV11FlagIsolation(t *testing.T) {
	for _, args := range [][]string{{"--prepare-v11", "--prepare-v10"}, {"--prepare-v11", "--private-settings", "/not-read"}, {"--epoch-v11"}, {"--epoch-v11", "--epoch-v10", "--serve-actual", "--authorization", "/not-read"}} {
		var out bytes.Buffer
		if code := runActualCLI(context.Background(), args, nil, &out, nil, nil); code != 1 || !strings.Contains(out.String(), "invalid_start") {
			t.Fatalf("%v %d %s", args, code, out.String())
		}
	}
}
