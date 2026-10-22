package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

// The claim is independent of output location and is never automatically removed,
// even if fixture setup or the first request fails. Reconciliation is manual.
func claimAuthorization(root string, plan []byte, out string) (string, error) {
	var scope taskbench.CampaignPreparationPlan
	if json.Unmarshal(plan, &scope) != nil || (scope.Schema != "campaign-preflight-plan.v1" && scope.Schema != "campaign-preflight-plan.v2") {
		return "", fmt.Errorf("invalid claim scope")
	}
	canonical, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	dir := filepath.Join(root, ".tmp", "paired-live-authorizations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("authorization already claimed or claim unavailable")
	}
	raw, _ := json.Marshal(map[string]string{"schema": "paired-live-claim.v1", "output": out, "status": "consumed_before_execution"})
	_, w := f.Write(append(raw, '\n'))
	s := f.Sync()
	c := f.Close()
	if err := errors.Join(w, s, c); err != nil {
		return "", err
	}
	// Persist new directory entries as well as the claim itself. On first use,
	// MkdirAll may have created both .tmp and its child.
	for _, parent := range []string{dir, filepath.Dir(dir), root} {
		d, err := os.Open(parent)
		if err != nil {
			return "", err
		}
		if err := errors.Join(d.Sync(), d.Close()); err != nil {
			return "", err
		}
	}
	return path, nil
}

func repositoryRoot() (string, error) {
	raw, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("repository unavailable")
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", err
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(string(module), "module github.com/frostloom/ai-gateway\n") {
		return "", fmt.Errorf("wrong repository")
	}
	return root, nil
}

func executingBuildRevision() (string, bool) {
	b, ok := debug.ReadBuildInfo()
	if !ok {
		return "", true
	}
	rev, modified := "", true
	for _, s := range b.Settings {
		if s.Key == "vcs.revision" {
			rev = s.Value
		}
		if s.Key == "vcs.modified" {
			modified = s.Value != "false"
		}
	}
	return rev, modified
}

func verifyExecutionRevision(root, requested, built string, modified bool) error {
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != requested || built != requested || modified {
		return fmt.Errorf("reviewed revision must match HEAD and an unmodified VCS-stamped build")
	}
	// A source edit after building must not run under an older revision claim.
	for _, args := range [][]string{{"diff", "--name-only", "-z", "HEAD"}, {"ls-files", "--others", "--exclude-standard", "-z"}} {
		cmd := append([]string{"-C", root}, args...)
		raw, err := exec.Command("git", cmd...).Output()
		if err != nil {
			return fmt.Errorf("source state unavailable")
		}
		for _, path := range strings.Split(string(raw), "\x00") {
			if strings.HasSuffix(path, ".go") || path == "go.mod" || path == "go.sum" || path == "go.work" || path == "go.work.sum" || strings.HasPrefix(path, "eval/task-definitions/") || strings.HasPrefix(path, "eval/runtime-profiles/") {
				return fmt.Errorf("runtime source or frozen inputs differ from reviewed revision")
			}
		}
	}
	return nil
}
