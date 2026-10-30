// full-window-actual checks independently frozen evidence or serves explicit
// bounded operations. Ordinary builds have empty readiness pins and cannot run.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"io"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"unicode/utf8"
)

type actualSession interface {
	Status() taskbench.FullWindowActualStatus
	RunWindow(context.Context, int, string) (taskbench.FullWindowActualReceipt, error)
	Close() error
}
type checkActual func(taskbench.FullWindowActualReadyOptions) (taskbench.FullWindowActualReadySummary, *taskbench.FullWindowActualReady, error)
type openActual func(context.Context, *taskbench.FullWindowActualReady, string) (actualSession, error)
type actualOperation struct {
	Operation string
	Index     *int
	Prior     *string
}

func actualEmit(out io.Writer, value any) error { return json.NewEncoder(out).Encode(value) }
func actualCLIError(out io.Writer, code string) int {
	_ = actualEmit(out, map[string]string{"type": "error", "code": code})
	return 1
}
func runActualCLI(ctx context.Context, args []string, in io.ReadCloser, out io.Writer, check checkActual, open openActual) int {
	fs := flag.NewFlagSet("full-window-actual", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	checkOnly := fs.Bool("check", false, "read-only frozen readiness check")
	prepareV8 := fs.Bool("prepare-v8", false, "read-only V8 technical preparation; never execution authority")
	prepareV9 := fs.Bool("prepare-v9", false, "read-only V9 technical preparation; never execution authority")
	epochV9 := fs.Bool("epoch-v9", false, "select independently frozen V9 delegated authority for actual serving")
	authorization := fs.String("authorization", "", "external exact-scope signed V8 human authorization")
	serve := fs.Bool("serve-actual", false, "retain one actual owner; only explicit stdin operations execute")
	manifest := fs.String("manifest", "", "independently pinned actual manifest")
	settings := fs.String("private-settings", "", "0600 private settings; read only after readiness")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_ = actualEmit(out, map[string]string{"type": "usage", "operations": "status, run_window(expected_index,prior_receipt_sha256), close"})
			return 0
		}
		return actualCLIError(out, "invalid_flags")
	}
	if (*prepareV9 && (*prepareV8 || *checkOnly || *serve || *settings != "" || *authorization != "" || *epochV9)) || (*epochV9 && (!*serve || *authorization == "" || *prepareV8)) {
		return actualCLIError(out, "invalid_start")
	}
	if fs.NArg() != 0 || ctx == nil || ctx.Err() != nil || (*checkOnly && *serve) || (*prepareV8 && (*checkOnly || *serve || *settings != "" || *authorization != "")) || (*authorization != "" && !*serve) {
		return actualCLIError(out, "invalid_start")
	}
	if !*checkOnly && !*serve && !*prepareV8 && !*prepareV9 {
		if *manifest != "" || *settings != "" {
			return actualCLIError(out, "explicit_mode_required")
		}
		if actualEmit(out, map[string]any{"type": "read_only", "actual_provider_calls_possible_in_this_invocation": false, "explicit_serve_required": true}) != nil {
			return 1
		}
		return 0
	}
	if *manifest == "" || (!*prepareV8 && !*prepareV9 && *authorization == "" && check == nil) || (*checkOnly && *settings != "") || (*serve && (*settings == "" || open == nil || in == nil)) {
		return actualCLIError(out, "incomplete_actual_declaration")
	}
	if *prepareV9 {
		summary, prepared, err := taskbench.CheckFullWindowV9Preparation(taskbench.FullWindowActualReadyOptions{ManifestPath: *manifest})
		if err != nil || prepared == nil {
			return actualCLIError(out, "actual_v9_preparation_rejected")
		}
		if actualEmit(out, map[string]any{"type": "prepared", "read_only": true, "actual_execution_authorized": false, "retry_policy_approved": false, "preparation": summary}) != nil {
			return 1
		}
		return 0
	}
	if *prepareV8 {
		summary, prepared, err := taskbench.CheckFullWindowV8Preparation(taskbench.FullWindowActualReadyOptions{ManifestPath: *manifest})
		if err != nil || prepared == nil {
			return actualCLIError(out, "actual_v8_preparation_rejected")
		}
		if actualEmit(out, map[string]any{"type": "prepared", "read_only": true, "actual_execution_authorized": false, "retry_policy_approved": false, "preparation": summary}) != nil {
			return 1
		}
		return 0
	}
	var summary taskbench.FullWindowActualReadySummary
	var ready *taskbench.FullWindowActualReady
	var err error
	if *authorization != "" && *epochV9 {
		var prepared *taskbench.FullWindowV9Preparation
		summary, prepared, err = taskbench.CheckFullWindowV9Preparation(taskbench.FullWindowActualReadyOptions{ManifestPath: *manifest})
		if err == nil && prepared != nil {
			ready, err = taskbench.AuthorizeFullWindowV9Preparation(prepared, *authorization)
		}
		if err != nil || ready == nil {
			return actualCLIError(out, "actual_v9_authorization_rejected")
		}
	} else if *authorization != "" {
		var prepared *taskbench.FullWindowV8Preparation
		summary, prepared, err = taskbench.CheckFullWindowV8Preparation(taskbench.FullWindowActualReadyOptions{ManifestPath: *manifest})
		if err == nil && prepared != nil {
			ready, err = taskbench.AuthorizeFullWindowV8Preparation(prepared, *authorization)
		}
		if err != nil || ready == nil {
			return actualCLIError(out, "actual_v8_authorization_rejected")
		}
	} else {
		summary, ready, err = check(taskbench.FullWindowActualReadyOptions{ManifestPath: *manifest})
	}
	if err != nil || ready == nil {
		return actualCLIError(out, "actual_readiness_rejected")
	}
	if *checkOnly {
		if actualEmit(out, map[string]any{"type": "checked", "read_only": true, "readiness": summary}) != nil {
			return 1
		}
		return 0
	}
	owner, err := open(ctx, ready, *settings)
	if err != nil || owner == nil {
		return actualCLIError(out, "actual_session_open_failed")
	}
	var once sync.Once
	var closeErr error
	closeOwner := func() error { once.Do(func() { closeErr = owner.Close() }); return closeErr }
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
	if actualEmit(out, map[string]any{"type": "startup", "status": owner.Status(), "readiness": summary}) != nil {
		return 1
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return actualCLIError(out, "operation_cancelled")
		}
		op, err := decodeActualOperation(scanner.Bytes())
		if err != nil {
			return actualCLIError(out, "invalid_operation")
		}
		switch op.Operation {
		case "status":
			if actualEmit(out, map[string]any{"type": "status", "status": owner.Status()}) != nil {
				return 1
			}
		case "close":
			if closeOwner() != nil {
				return actualCLIError(out, "actual_session_close_failed")
			}
			if actualEmit(out, map[string]any{"type": "closed", "status": owner.Status()}) != nil {
				return 1
			}
			return 0
		case "run_window":
			receipt, err := owner.RunWindow(ctx, *op.Index, *op.Prior)
			if actualEmit(out, struct {
				Type       string                            `json:"type"`
				ReceiptSHA string                            `json:"receipt_sha256"`
				Receipt    taskbench.FullWindowActualReceipt `json:"receipt"`
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
		return actualCLIError(out, "operation_cancelled")
	}
	if scanner.Err() != nil {
		return actualCLIError(out, "input_read_failed")
	}
	if closeOwner() != nil {
		return actualCLIError(out, "actual_session_close_failed")
	}
	if actualEmit(out, map[string]any{"type": "closed", "reason": "input_eof", "status": owner.Status()}) != nil {
		return 1
	}
	return 0
}
func decodeActualOperation(raw []byte) (actualOperation, error) {
	if !utf8.Valid(raw) {
		return actualOperation{}, fmt.Errorf("invalid UTF8")
	}
	var op actualOperation
	reject := func() (actualOperation, error) { return actualOperation{}, fmt.Errorf("invalid actual operation") }
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
		if op.Index == nil || *op.Index < 1 || *op.Index > 3660 || op.Prior == nil || (*op.Prior != "" && !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*op.Prior)) {
			return reject()
		}
	default:
		return reject()
	}
	return op, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runActualCLI(ctx, os.Args[1:], os.Stdin, os.Stdout, taskbench.CheckFullWindowActualReady, func(ctx context.Context, ready *taskbench.FullWindowActualReady, path string) (actualSession, error) {
		return taskbench.OpenFullWindowActual(ctx, ready, path)
	})
	stop()
	os.Exit(code)
}
