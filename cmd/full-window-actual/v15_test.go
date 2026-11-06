package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActualCLIV15PreparationRemainsReadOnly(t *testing.T) {
	var out bytes.Buffer
	if code := runActualCLI(context.Background(), []string{"--prepare-v15", "--manifest", "/not-frozen"}, nil, &out, nil, nil); code != 1 || !strings.Contains(out.String(), "actual_v15_preparation_rejected") {
		t.Fatalf("%d %s", code, out.String())
	}
}
func TestActualCLIV15FlagIsolation(t *testing.T) {
	for _, args := range [][]string{{"--prepare-v15", "--prepare-v14"}, {"--prepare-v15", "--private-settings", "/not-read"}, {"--epoch-v15"}, {"--epoch-v15", "--epoch-v14", "--serve-actual", "--authorization", "/not-read"}} {
		var out bytes.Buffer
		if code := runActualCLI(context.Background(), args, nil, &out, nil, nil); code != 1 || !strings.Contains(out.String(), "invalid_start") {
			t.Fatalf("%v %d %s", args, code, out.String())
		}
	}
}
