package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClosed struct {
	mu     sync.Mutex
	status taskbench.FullWindowClosedStatus
	runs   int
	closes int
}

func newFakeClosed() *fakeClosed {
	return &fakeClosed{status: taskbench.FullWindowClosedStatus{EvidenceKind: "local_stub", NextWindowIndex: 1}}
}
func (s *fakeClosed) Status() taskbench.FullWindowClosedStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
func (s *fakeClosed) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	s.status.Closed = true
	return nil
}
func (s *fakeClosed) RunWindow(ctx context.Context, n int, prior string) (taskbench.FullWindowClosedReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Closed || n != s.status.NextWindowIndex || prior != s.status.LastReceiptSHA256 {
		return taskbench.FullWindowClosedReceipt{}, errors.New("rejected")
	}
	s.runs++
	s.status.NextWindowIndex++
	s.status.LastReceiptSHA256 = strings.Repeat("a", 64)
	return taskbench.FullWindowClosedReceipt{FullWindowClosedStatus: s.status, WindowIndex: n, ReceiptSHA256: s.status.LastReceiptSHA256}, nil
}
func cliArgs(t *testing.T) []string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fixture-dsn")
	if e := os.WriteFile(p, []byte("root:root@tcp(127.0.0.1:3307)/\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return []string{"--serve-closed", "--private-root", filepath.Join(t.TempDir(), "new"), "--width", "1", "--source-revision", strings.Repeat("b", 40), "--allow-fixture-creation", "--fixture-dsn-file", p}
}
func TestClosedCLIExplicitOperations(t *testing.T) {
	s := newFakeClosed()
	opens := 0
	var out bytes.Buffer
	input := `{"operation":"status"}` + "\n" + `{"operation":"run_window","expected_index":1,"prior_receipt_sha256":""}` + "\n" + `{"operation":"close"}` + "\n"
	code := runClosedCLI(context.Background(), cliArgs(t), io.NopCloser(strings.NewReader(input)), &out, func(_ context.Context, o taskbench.FullWindowClosedOptions) (closedSession, error) {
		opens++
		if o.LoopbackAdminDSN != "root:root@tcp(127.0.0.1:3307)/" {
			t.Fatal("DSN changed")
		}
		return s, nil
	})
	if code != 0 || opens != 1 || s.runs != 1 || s.closes < 1 {
		t.Fatalf("explicit closed operation not executed: code=%d opens=%d runs=%d closes=%d", code, opens, s.runs, s.closes)
	}
	var lines []map[string]any
	d := json.NewDecoder(&out)
	for d.More() {
		var x map[string]any
		if e := d.Decode(&x); e != nil {
			t.Fatal(e)
		}
		lines = append(lines, x)
	}
	found := false
	for _, x := range lines {
		if x["type"] == "window" {
			found = x["receipt_sha256"] == strings.Repeat("a", 64)
		}
	}
	if !found {
		t.Fatal("receipt chain not exposed")
	}
	if strings.Contains(out.String(), "root:root") {
		t.Fatal("DSN leaked")
	}
}
func TestClosedCLIDefaultDoesNotOpenOrReadSecrets(t *testing.T) {
	var out bytes.Buffer
	opened := 0
	code := runClosedCLI(context.Background(), []string{"--fixture-dsn-file", "/does/not/exist"}, io.NopCloser(strings.NewReader("")), &out, func(context.Context, taskbench.FullWindowClosedOptions) (closedSession, error) {
		opened++
		return newFakeClosed(), nil
	})
	if code != 0 || opened != 0 || !strings.Contains(out.String(), "read_only") {
		t.Fatal("default performed work", code, opened, out.String())
	}
}
func TestClosedCLIRejectsMalformedOperationBeforeRun(t *testing.T) {
	for _, body := range []string{`{"operation":"run_window","operation":"status"}`, `{"operation":"run_window","expected_index":1}`, `{"operation":"run_window","expected_index":1.5,"prior_receipt_sha256":""}`, `{"operation":"run_window","expected_index":1,"prior_receipt_sha256":null}`, `{"operation":"run_window","expected_index":1,"prior_receipt_sha256":"","allow_paid":true}`, `{"operation":"status","expected_index":1}`, `{"operation":"execute_paid"}`, strings.Repeat("x", 5000)} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			s := newFakeClosed()
			var out bytes.Buffer
			code := runClosedCLI(context.Background(), cliArgs(t), io.NopCloser(strings.NewReader(body+"\n")), &out, func(context.Context, taskbench.FullWindowClosedOptions) (closedSession, error) { return s, nil })
			if code == 0 || s.runs != 0 || !s.Status().Closed {
				t.Fatal("invalid input executed", code, s.runs)
			}
		})
	}
}
func TestClosedCLIRejectsUnprotectedDSN(t *testing.T) {
	args := cliArgs(t)
	p := args[len(args)-1]
	if e := os.Chmod(p, 0644); e != nil {
		t.Fatal(e)
	}
	opened := false
	var out bytes.Buffer
	if code := runClosedCLI(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, func(context.Context, taskbench.FullWindowClosedOptions) (closedSession, error) {
		opened = true
		return newFakeClosed(), nil
	}); code == 0 || opened {
		t.Fatal("unprotected credentials read")
	}
}
func TestClosedCLICancelInterruptsIdleInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer writer.Close()
	s := newFakeClosed()
	var out bytes.Buffer
	done := make(chan int, 1)
	opened := make(chan struct{})
	args := cliArgs(t)
	go func() {
		done <- runClosedCLI(ctx, args, reader, &out, func(context.Context, taskbench.FullWindowClosedOptions) (closedSession, error) {
			close(opened)
			return s, nil
		})
	}()
	<-opened
	cancel()
	select {
	case code := <-done:
		if code == 0 || !s.Status().Closed {
			t.Fatal("cancel did not stop owner")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle cancellation blocked")
	}
}
