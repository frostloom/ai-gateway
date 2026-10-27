// full-window-closed exposes explicit bounded operations over concrete local
// substitutes. It has no paid mode, provider key, endpoint or transport option.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

type closedSession interface {
	Status() taskbench.FullWindowClosedStatus
	RunWindow(context.Context, int, string) (taskbench.FullWindowClosedReceipt, error)
	Close() error
}
type openClosed func(context.Context, taskbench.FullWindowClosedOptions) (closedSession, error)
type closedOperation struct {
	Operation string
	Index     *int
	Prior     *string
}

func emit(out io.Writer, value any) error { return json.NewEncoder(out).Encode(value) }
func cliError(out io.Writer, code string) int {
	_ = emit(out, map[string]string{"type": "error", "code": code})
	return 1
}
func runClosedCLI(ctx context.Context, args []string, in io.ReadCloser, out io.Writer, open openClosed) int {
	fs := flag.NewFlagSet("full-window-closed", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serve := fs.Bool("serve-closed", false, "start a concrete closed session; no actual provider mode exists")
	definitions := fs.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "original definitions")
	profiles := fs.String("profiles", "eval/runtime-profiles/2026-10-13-six-profiles-v1", "original profiles")
	root := fs.String("private-root", "", "new private synthetic root")
	width := fs.Int("width", 0, "explicit closed width1..100")
	revision := fs.String("source-revision", "", "declared source revision; not build attestation")
	allow := fs.Bool("allow-fixture-creation", false, "allow owned local test schema")
	dsnFile := fs.String("fixture-dsn-file", "", "0600 file containing only loopback MySQL fixture DSN")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_ = emit(out, map[string]string{"type": "usage", "mode": "closed_only", "operations": "status, run_window(expected_index,prior_receipt_sha256), close"})
			return 0
		}
		return cliError(out, "invalid_flags")
	}
	if fs.NArg() != 0 || ctx == nil || ctx.Err() != nil {
		return cliError(out, "invalid_start")
	}
	if !*serve {
		if emit(out, map[string]any{"type": "read_only", "actual_provider_calls_possible": false, "explicit_serve_required": true}) != nil {
			return 1
		}
		return 0
	}
	if !*allow || *root == "" || *width < 1 || *width > 100 || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(*revision) || open == nil {
		return cliError(out, "incomplete_closed_declaration")
	}
	dsn, err := readFixtureDSN(*dsnFile)
	if err != nil {
		return cliError(out, "fixture_dsn_unavailable")
	}
	owner, err := open(ctx, taskbench.FullWindowClosedOptions{DefinitionsDir: *definitions, ProfilesDir: *profiles, NewPrivateRoot: *root, Width: *width, DeclaredSourceRevision: *revision, AllowFixtureCreation: *allow, LoopbackAdminDSN: dsn})
	if err != nil || owner == nil {
		return cliError(out, "closed_session_open_failed")
	}
	var closeOnce sync.Once
	var closeErr error
	closeOwner := func() error { closeOnce.Do(func() { closeErr = owner.Close() }); return closeErr }
	defer closeOwner()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = in.Close()
			_ = closeOwner()
		case <-done:
		}
	}()
	if emit(out, map[string]any{"type": "startup", "status": owner.Status(), "declared_source_revision": *revision, "build_attestation": "not_verified_by_closed_cli"}) != nil {
		return 1
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return cliError(out, "operation_cancelled")
		}
		op, err := decodeClosedOperation(scanner.Bytes())
		if err != nil {
			return cliError(out, "invalid_operation")
		}
		switch op.Operation {
		case "status":
			if emit(out, map[string]any{"type": "status", "status": owner.Status()}) != nil {
				return 1
			}
		case "close":
			if closeOwner() != nil {
				return cliError(out, "closed_session_close_failed")
			}
			if emit(out, map[string]any{"type": "closed", "status": owner.Status()}) != nil {
				return 1
			}
			return 0
		case "run_window":
			receipt, err := owner.RunWindow(ctx, *op.Index, *op.Prior)
			if emit(out, struct {
				Type       string                            `json:"type"`
				ReceiptSHA string                            `json:"receipt_sha256"`
				Receipt    taskbench.FullWindowClosedReceipt `json:"receipt"`
				Stopped    bool                              `json:"stopped"`
			}{"window", receipt.ReceiptSHA256, receipt, err != nil}) != nil {
				return 1
			}
			if err != nil {
				return 1
			}
		}
	}
	if ctx.Err() != nil {
		return cliError(out, "operation_cancelled")
	}
	if scanner.Err() != nil {
		return cliError(out, "input_read_failed")
	}
	if closeOwner() != nil {
		return cliError(out, "closed_session_close_failed")
	}
	if emit(out, map[string]any{"type": "closed", "reason": "input_eof", "status": owner.Status()}) != nil {
		return 1
	}
	return 0
}
func readFixtureDSN(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > 4096 {
		return "", fmt.Errorf("fixture declaration unavailable")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) || !actual.Mode().IsRegular() || actual.Mode().Perm() != 0600 {
		return "", fmt.Errorf("fixture declaration changed")
	}
	body, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(body) > 4096 {
		return "", fmt.Errorf("fixture declaration unavailable")
	}
	value := strings.TrimSpace(string(body))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("fixture declaration invalid")
	}
	return value, nil
}
func decodeClosedOperation(raw []byte) (closedOperation, error) {
	var op closedOperation
	reject := func() (closedOperation, error) { return closedOperation{}, fmt.Errorf("invalid closed operation") }
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return reject()
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return reject()
		}
		seen[key] = true
		switch key {
		case "operation":
			if d.Decode(&op.Operation) != nil {
				return reject()
			}
		case "expected_index":
			var n int
			if d.Decode(&n) != nil {
				return reject()
			}
			op.Index = &n
		case "prior_receipt_sha256":
			var v *string
			if d.Decode(&v) != nil || v == nil {
				return reject()
			}
			op.Prior = v
		default:
			return reject()
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return reject()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return reject()
	}
	switch op.Operation {
	case "status", "close":
		if op.Index != nil || op.Prior != nil {
			return reject()
		}
	case "run_window":
		if op.Index == nil || *op.Index < 1 || op.Prior == nil || (*op.Prior != "" && !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*op.Prior)) {
			return reject()
		}
	default:
		return reject()
	}
	return op, nil
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runClosedCLI(ctx, os.Args[1:], os.Stdin, os.Stdout, func(ctx context.Context, o taskbench.FullWindowClosedOptions) (closedSession, error) {
		return taskbench.OpenFullWindowClosed(ctx, o)
	})
	stop()
	os.Exit(code)
}
