package comparison

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var errLocalUsageConflict = errors.New("local usage conflict")

func localUsagePath(dir, id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}
func syncLocalDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func saveLocalUsage(dir string, x inboxUsage) error {
	if x.RequestID == "" || x.TenantID == 0 || x.BillID <= 0 || x.ProviderID <= 0 || x.PromptTokens < 0 || x.CompletionTokens < 0 || x.State != "" {
		return errors.New("invalid local usage")
	}
	data, err := json.Marshal(x)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".usage-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	path := localUsagePath(dir, x.RequestID)
	if err = os.Link(name, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		prior, e := readLocalUsage(dir, x.RequestID)
		if e != nil {
			return e
		}
		if prior != x {
			return errLocalUsageConflict
		}
	}
	return syncLocalDir(dir)
}
func readLocalUsage(dir, id string) (inboxUsage, error) {
	var x inboxUsage
	data, err := os.ReadFile(localUsagePath(dir, id))
	if err != nil {
		return x, err
	}
	if err = json.Unmarshal(data, &x); err != nil {
		return x, err
	}
	if x.RequestID != id {
		return x, errors.New("journal identity mismatch")
	}
	return x, nil
}
func TestLocalUsageJournalChild(t *testing.T) {
	if os.Getenv("LOCAL_USAGE_CHILD") != "1" {
		t.Skip("helper only")
	}
	x := inboxUsage{RequestID: "local-request", TenantID: 7, BillID: 9, ProviderID: 11, PromptTokens: 40, CompletionTokens: 2}
	if err := saveLocalUsage(os.Getenv("LOCAL_USAGE_DIR"), x); err != nil {
		t.Fatal(err)
	}
	fmt.Println("LOCAL_USAGE_SYNCED")
	time.Sleep(time.Minute)
}
func TestLocalUsageJournalSurvivesProcessDeath(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLocalUsageJournalChild$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "LOCAL_USAGE_CHILD=1", "LOCAL_USAGE_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LOCAL_USAGE_SYNCED" {
		t.Fatalf("child marker=%q err=%v", line, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	want := inboxUsage{RequestID: "local-request", TenantID: 7, BillID: 9, ProviderID: 11, PromptTokens: 40, CompletionTokens: 2}
	got, err := readLocalUsage(dir, want.RequestID)
	if err != nil || got != want {
		t.Fatalf("record=%+v err=%v", got, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- saveLocalUsage(dir, want) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	bad := want
	bad.CompletionTokens++
	if err := saveLocalUsage(dir, bad); !errors.Is(err, errLocalUsageConflict) {
		t.Fatalf("conflicting usage=%v", err)
	}
	got, err = readLocalUsage(dir, want.RequestID)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	t.Log("synced file survived worker kill; duplicate writes matched; conflicting values rejected")
}
