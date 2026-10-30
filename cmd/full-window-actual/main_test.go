package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeActualSession struct {
	runs, closed int
	fail         bool
	started      chan struct{}
}

func (f *fakeActualSession) Status() taskbench.FullWindowActualStatus {
	return taskbench.FullWindowActualStatus{EvidenceKind: "local_stub", PlannedPairs: 3660, PlannedArms: 7320}
}
func (f *fakeActualSession) RunWindow(_ context.Context, index int, prior string) (taskbench.FullWindowActualReceipt, error) {
	f.runs++
	if index != 1 || prior != "" {
		return taskbench.FullWindowActualReceipt{}, errors.New("wrong input")
	}
	r := taskbench.FullWindowActualReceipt{WindowIndex: 1, ReceiptSHA256: strings.Repeat("a", 64)}
	if f.fail {
		return r, errors.New("synthetic failure")
	}
	return r, nil
}
func (f *fakeActualSession) Close() error { f.closed++; return nil }
func actualCLIFactories(f *fakeActualSession, checks, opens *int, reject bool) (checkActual, openActual) {
	check := func(taskbench.FullWindowActualReadyOptions) (taskbench.FullWindowActualReadySummary, *taskbench.FullWindowActualReady, error) {
		*checks++
		if reject {
			return taskbench.FullWindowActualReadySummary{}, nil, errors.New("not ready")
		}
		return taskbench.FullWindowActualReadySummary{}, &taskbench.FullWindowActualReady{}, nil
	}
	open := func(_ context.Context, _ *taskbench.FullWindowActualReady, path string) (actualSession, error) {
		*opens++
		if path != "/private/settings.json" {
			return nil, errors.New("wrong settings path")
		}
		if f.started != nil {
			close(f.started)
		}
		return f, nil
	}
	return check, open
}

var actualServeArgs = []string{"--serve-actual", "--manifest", "/private/ready.json", "--private-settings", "/private/settings.json"}

func TestActualCLIDefaultAndCheckNeverOpen(t *testing.T) {
	for _, args := range [][]string{nil, {"--check", "--manifest", "/private/ready.json"}} {
		f := &fakeActualSession{}
		checks, opens := 0, 0
		check, open := actualCLIFactories(f, &checks, &opens, false)
		var out bytes.Buffer
		code := runActualCLI(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, check, open)
		if code != 0 || opens != 0 || f.runs != 0 || f.closed != 0 {
			t.Fatalf("read-only side effect code=%d opens=%d runs=%d closed=%d", code, opens, f.runs, f.closed)
		}
		want := 0
		if len(args) > 0 {
			want = 1
		}
		if checks != want {
			t.Fatalf("checks=%d want %d", checks, want)
		}
	}
}
func TestActualCLIReadinessRejectsBeforeOpen(t *testing.T) {
	f := &fakeActualSession{}
	checks, opens := 0, 0
	check, open := actualCLIFactories(f, &checks, &opens, true)
	var out bytes.Buffer
	if code := runActualCLI(context.Background(), actualServeArgs, io.NopCloser(strings.NewReader("")), &out, check, open); code == 0 || checks != 1 || opens != 0 || f.runs != 0 {
		t.Fatalf("unready accepted code=%d checks=%d opens=%d", code, checks, opens)
	}
}
func TestActualCLIExplicitSingleOperation(t *testing.T) {
	f := &fakeActualSession{}
	checks, opens := 0, 0
	check, open := actualCLIFactories(f, &checks, &opens, false)
	var out bytes.Buffer
	input := "{\"operation\":\"status\"}\n{\"operation\":\"run_window\",\"expected_index\":1,\"prior_receipt_sha256\":\"\"}\n{\"operation\":\"close\"}\n"
	if code := runActualCLI(context.Background(), actualServeArgs, io.NopCloser(strings.NewReader(input)), &out, check, open); code != 0 || checks != 1 || opens != 1 || f.runs != 1 || f.closed != 1 {
		t.Fatalf("explicit code=%d checks=%d opens=%d runs=%d closed=%d", code, checks, opens, f.runs, f.closed)
	}
	if !strings.Contains(out.String(), strings.Repeat("a", 64)) {
		t.Fatal("missing raw receipt chain digest")
	}
}
func TestActualCLIInvalidCommandsCloseWithoutRun(t *testing.T) {
	for _, input := range []string{
		`{"operation":"run_window","expected_index":1,"prior_receipt_sha256":null}`,
		`{"operation":"run_window","expected_index":null,"prior_receipt_sha256":""}`,
		`{"operation":"run_window","expected_index":1,"expected_index":2,"prior_receipt_sha256":""}`,
		`{"operation":"run_window","expected_index":1,"prior_receipt_sha256":"","cap":99}`,
		`{"operation":"status","expected_index":1}`, `{"operation":"run_window","expected_index":3661,"prior_receipt_sha256":""}`,
		`{"operation":"run_window","expected_index":1,"prior_receipt_sha256":"x"}`, `{"operation":"status"}{}`,
		`{"operation":null}`, `{"operation":"status"}` + strings.Repeat(" ", 4096),
	} {
		t.Run(input[:min(50, len(input))], func(t *testing.T) {
			f := &fakeActualSession{}
			checks, opens := 0, 0
			check, open := actualCLIFactories(f, &checks, &opens, false)
			var out bytes.Buffer
			if code := runActualCLI(context.Background(), actualServeArgs, io.NopCloser(strings.NewReader(input+"\n")), &out, check, open); code == 0 || f.runs != 0 || f.closed != 1 {
				t.Fatalf("invalid command code=%d runs=%d closed=%d", code, f.runs, f.closed)
			}
		})
	}
}
func TestActualCLIFailedWindowStopsFollowing(t *testing.T) {
	f := &fakeActualSession{fail: true}
	checks, opens := 0, 0
	check, open := actualCLIFactories(f, &checks, &opens, false)
	var out bytes.Buffer
	input := strings.Repeat("{\"operation\":\"run_window\",\"expected_index\":1,\"prior_receipt_sha256\":\"\"}\n", 2)
	if code := runActualCLI(context.Background(), actualServeArgs, io.NopCloser(strings.NewReader(input)), &out, check, open); code == 0 || f.runs != 1 || f.closed != 1 {
		t.Fatalf("failure not terminal code=%d runs=%d closed=%d", code, f.runs, f.closed)
	}
}
func TestActualCLIIdleCancellationClosesOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer writer.Close()
	f := &fakeActualSession{started: make(chan struct{})}
	checks, opens := 0, 0
	check, open := actualCLIFactories(f, &checks, &opens, false)
	done := make(chan int, 1)
	go func() { var out bytes.Buffer; done <- runActualCLI(ctx, actualServeArgs, reader, &out, check, open) }()
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("owner not opened")
	}
	cancel()
	select {
	case code := <-done:
		if code == 0 || f.closed != 1 || f.runs != 0 {
			t.Fatalf("cancel code=%d closed=%d runs=%d", code, f.closed, f.runs)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not end owner")
	}
}
func TestActualCLIRejectsMutableExecutionFlags(t *testing.T) {
	for _, args := range [][]string{{"--serve-closed"}, {"--provider-url", "https://example.invalid"}, {"--check", "--serve-actual"}, {"--private-settings", "/missing"}, {"--serve-actual", "--manifest", "/missing"}} {
		var out bytes.Buffer
		if code := runActualCLI(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, nil, nil); code == 0 {
			t.Fatalf("invalid flags accepted: %v", args)
		}
	}
}

func TestActualCLIV8DedicatedModesRejectBeforeOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code string
	}{
		{"prepare_missing", []string{"--prepare-v8", "--manifest", "/synthetic/missing"}, "actual_v8_preparation_rejected"},
		{"prepare_settings", []string{"--prepare-v8", "--manifest", "/synthetic/missing", "--private-settings", "/do-not-read"}, "invalid_start"},
		{"prepare_serve", []string{"--prepare-v8", "--serve-actual", "--manifest", "/synthetic/missing"}, "invalid_start"},
		{"auth_check", []string{"--check", "--authorization", "/synthetic/missing", "--manifest", "/synthetic/missing"}, "invalid_start"},
		{"auth_missing", []string{"--serve-actual", "--authorization", "/synthetic/missing", "--manifest", "/synthetic/missing", "--private-settings", "/do-not-read"}, "actual_v8_authorization_rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, opens := 0, 0
			f := &fakeActualSession{}
			check, open := actualCLIFactories(f, &checks, &opens, false)
			var out bytes.Buffer
			code := runActualCLI(context.Background(), tc.args, io.NopCloser(strings.NewReader("")), &out, check, open)
			if code != 1 || !strings.Contains(out.String(), tc.code) || opens != 0 || checks != 0 {
				t.Fatalf("v8 mode escaped: %d %s checks=%d opens=%d", code, out.String(), checks, opens)
			}
		})
	}
}
