package taskbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRepeatedDiagnosticLocalRunner(t *testing.T) { testRepeatedRunner(t, true) }
func captureDiagnosticFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
func captureDiagnosticRun(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		captureDiagnosticFile(t, filepath.Join(to, rel), raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRepeatedDiagnosticCleanupStopsBothOrders(t *testing.T) {
	for _, order := range [][]string{{"model", "jev_selective"}, {"jev_selective", "model"}} {
		t.Run(order[0], func(t *testing.T) {
			cfg := explicitTestOrder(t, pairTestConfig(), order)
			path := filepath.Join(t.TempDir(), "pair")
			arms := 0
			err := runPreparedPair(context.Background(), path, cfg, nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
				arms++
				return &pairFaultDriver{ConversationDriver: rechargeDriver(), stage: "cleanup"}, nil
			}, nil, true, true, true)
			if err == nil || arms != 1 {
				t.Fatal("cleanup failure started next arm", err, arms)
			}
			r, err := ReportPreparedPair(path)
			if err != nil || r.FinishedArms != 1 || r.UnrunArms != 1 {
				t.Fatal("cleanup failure lost denominator", err, r)
			}
			var first []string
			for _, row := range r.Rows {
				if row.Status == "failed" {
					first = append(first, row.Arm)
					if len(row.ExecutionFailures) != 1 || row.ExecutionFailures[0].Stage != "cleanup" {
						t.Fatal("cleanup cause lost")
					}
				} else if row.Status != "not_started" {
					t.Fatal("later arm executed")
				}
			}
			if !reflect.DeepEqual(first, order[:1]) {
				t.Fatal("cleanup wrong arm order", first, order)
			}
		})
	}
}

// Capture source bytes for reproducible, explicitly enabled local probes only.
func captureDiagnosticSources(t *testing.T, root, mode string) {
	t.Helper()
	sums := map[string]string{}
	for _, dir := range []string{"cmd/paired-live", "internal/taskbench", "internal/observability", "internal/agent"} {
		entries, err := os.ReadDir(filepath.Join("../..", dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			rel := filepath.Join(dir, entry.Name())
			raw, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			sums[rel] = hex.EncodeToString(sum[:])
		}
	}
	raw, err := json.MarshalIndent(sums, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	captureDiagnosticFile(t, filepath.Join(root, mode, "source-sha256.json"), raw)
}
