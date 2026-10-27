package main

import (
	"bytes"
	"testing"
)

func TestDefaultNeverReadsSecretsOrExecutes(t *testing.T) {
	var b bytes.Buffer
	if run([]string{"-root", "../.."}, &b) != 0 {
		t.Fatal(b.String())
	}
	if run([]string{"-execute", "-contract", "/does-not-exist"}, &b) == 0 {
		t.Fatal("missing contract accepted")
	}
}
