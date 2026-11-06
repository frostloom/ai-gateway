package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActualCLIV14PreparationRemainsReadOnly(t *testing.T) {
	var out bytes.Buffer
	if code := runActualCLI(context.Background(), []string{"--prepare-v14", "--manifest", "/not-frozen"}, nil, &out, nil, nil); code != 1 || !strings.Contains(out.String(), "actual_v14_preparation_rejected") {
		t.Fatalf("%d %s", code, out.String())
	}
}
func TestActualCLIV14FlagIsolation(t *testing.T) {
	for _, args := range [][]string{{"--prepare-v14", "--prepare-v12"}, {"--prepare-v14", "--private-settings", "/not-read"}, {"--epoch-v14"}, {"--epoch-v14", "--epoch-v12", "--serve-actual", "--authorization", "/not-read"}} {
		var out bytes.Buffer
		if code := runActualCLI(context.Background(), args, nil, &out, nil, nil); code != 1 || !strings.Contains(out.String(), "invalid_start") {
			t.Fatalf("%v %d %s", args, code, out.String())
		}
	}
}
