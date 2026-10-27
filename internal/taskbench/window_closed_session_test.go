package taskbench

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func closedSessionOptions(t *testing.T) FullWindowClosedOptions {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return FullWindowClosedOptions{DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", NewPrivateRoot: filepath.Join(parent, "closed"), Width: 1, DeclaredSourceRevision: strings.Repeat("b", 40), AllowFixtureCreation: true, LoopbackAdminDSN: "root:root@tcp(127.0.0.1:3307)/"}
}
func TestFullWindowClosedStartupAndInvalidOperation(t *testing.T) {
	for _, kind := range []string{"index", "prior", "cancel", "persist", "owner"} {
		t.Run(kind, func(t *testing.T) {
			options := closedSessionOptions(t)
			s, err := OpenFullWindowClosed(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			st := s.Status()
			if st.NextWindowIndex != 1 || st.PlannedPairs != 3660 || st.PlannedArms != 7320 || st.DefinedTurns != 11772 || st.BoundariesExcluded != 1851 || st.NativePackets != 0 || st.ModelAttempts != 0 || st.JEVAttempts != 0 || st.Closed || st.AllowPaid || st.ReadyForPaidRun || st.EvidenceKind != "local_stub" {
				t.Fatalf("startup: %+v", st)
			}
			if _, err := OpenFullWindowClosed(context.Background(), options); err == nil {
				t.Fatal("reopened existing root")
			}
			ctx := context.Background()
			index := 1
			prior := ""
			switch kind {
			case "index":
				index = 2
			case "prior":
				prior = strings.Repeat("a", 64)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "persist":
				if err := os.WriteFile(filepath.Join(options.NewPrivateRoot, "declaration.json"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "owner":
				s.campaign.closeWindowCampaign()
			}
			if _, err := s.RunWindow(ctx, index, prior); err == nil {
				t.Fatal("invalid operation accepted")
			}
			st = s.Status()
			if !st.Closed || st.ModelAttempts != 0 || st.JEVAttempts != 0 || st.NativePackets != 0 {
				t.Fatalf("invalid operation executed: %+v", st)
			}
			if _, err := s.RunWindow(context.Background(), 1, ""); err == nil {
				t.Fatal("stopped session reused")
			}
		})
	}
}
func TestFullWindowClosedConstructorRejectsBeforeRoot(t *testing.T) {
	for _, kind := range []string{"width", "revision", "source", "profiles", "fixture", "remote", "selected_db", "nil_context"} {
		t.Run(kind, func(t *testing.T) {
			o := closedSessionOptions(t)
			ctx := context.Background()
			switch kind {
			case "width":
				o.Width = 101
			case "revision":
				o.DeclaredSourceRevision = "main"
			case "source":
				o.DefinitionsDir = t.TempDir()
			case "profiles":
				o.ProfilesDir = t.TempDir()
			case "fixture":
				o.AllowFixtureCreation = false
			case "remote":
				o.LoopbackAdminDSN = "root:root@tcp(192.0.2.1:3307)/"
			case "selected_db":
				o.LoopbackAdminDSN += "foreign"
			case "nil_context":
				ctx = nil
			}
			if s, err := OpenFullWindowClosed(ctx, o); err == nil {
				s.Close()
				t.Fatal("invalid constructor accepted")
			}
			if _, err := os.Lstat(o.NewPrivateRoot); !os.IsNotExist(err) {
				t.Fatal("invalid constructor wrote root")
			}
		})
	}
}
func TestFullWindowClosedSameOwnerTwoWindows(t *testing.T) {
	o := closedSessionOptions(t)
	if archiveRoot := os.Getenv("TASKBENCH_CLOSED_SESSION_TEST_ROOT"); archiveRoot != "" {
		o.NewPrivateRoot = archiveRoot
	}
	beforeSchemas := closedSessionSchemas(t, o.LoopbackAdminDSN)
	s, err := OpenFullWindowClosed(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 10; i++ {
		st := s.Status()
		st.NextWindowIndex = 999
		if s.Status().NextWindowIndex != 1 {
			t.Fatal("status alias")
		}
	}
	if !reflect.DeepEqual(beforeSchemas, closedSessionSchemas(t, o.LoopbackAdminDSN)) {
		t.Fatal("startup/status created DB schema")
	}
	owner := s.campaign
	first, err := s.RunWindow(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("first: %+v %v", first, err)
	}
	if first.WindowIndex != 1 || first.NativePackets != 3 || !first.PacketsVerified || first.FinishedArms != 2 || first.UnrunCampaignArms != 7318 || first.ReceiptSHA256 == "" {
		t.Fatalf("first: %+v", first)
	}
	second, err := s.RunWindow(context.Background(), 2, first.ReceiptSHA256)
	if err != nil {
		t.Fatalf("second: %+v %v", second, err)
	}
	if s.campaign != owner || second.WindowIndex != 2 || second.NativePackets <= first.NativePackets || second.UnrunCampaignArms != 7316 || second.ReceiptSHA256 == first.ReceiptSHA256 {
		t.Fatalf("owner or receipt: %+v", second)
	}
	packets, err := verifyWindowClosedPackets(s.model, s.judge)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	windows := map[string]bool{}
	for _, p := range packets {
		if names[p.RequestFile] {
			t.Fatal("cross-window packet reused")
		}
		names[p.RequestFile] = true
		windows[p.WindowID] = true
	}
	if len(windows) != 2 {
		t.Fatal("missing second window native packets")
	}
	before := s.Status()
	if _, err := s.RunWindow(context.Background(), 2, first.ReceiptSHA256); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	after := s.Status()
	if !after.Closed || after.NativePackets != before.NativePackets || after.ModelAttempts != before.ModelAttempts || after.JEVAttempts != before.JEVAttempts {
		t.Fatal("duplicate resent")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.Status().Closed {
		t.Fatal("status unsafe after Close")
	}
	if !reflect.DeepEqual(beforeSchemas, closedSessionSchemas(t, o.LoopbackAdminDSN)) {
		t.Fatal("window leaked DB schema")
	}
	t.Logf("closed source=%s first packets=%d second cumulative=%d model=%d jev=%d", s.campaign.prepared.scope.DefinitionSHA256, first.NativePackets, second.NativePackets, after.ModelAttempts, after.JEVAttempts)
}

func TestFullWindowClosedIdleCancellationAndTerminalEvidence(t *testing.T) {
	o := closedSessionOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	s, err := OpenFullWindowClosed(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := s.listener.Addr().String()
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for !s.Status().Closed && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.Status().Closed {
		t.Fatal("idle parent cancellation left live owner")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(o.NewPrivateRoot, "terminal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st FullWindowClosedStatus
	if json.Unmarshal(raw, &st) != nil || !st.Closed || st.NextWindowIndex != 1 || st.NativePackets != 0 {
		t.Fatal("missing terminal unrun status")
	}
	conn, err := net.DialTimeout("tcp", endpoint, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("sink survived close")
	}
}

func TestFullWindowClosedForeignRootStopsWithoutInference(t *testing.T) {
	o := closedSessionOptions(t)
	s, err := OpenFullWindowClosed(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	original := o.NewPrivateRoot + "-original"
	if err := os.Rename(o.NewPrivateRoot, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(o.NewPrivateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunWindow(context.Background(), 1, ""); err == nil {
		t.Fatal("foreign root accepted")
	}
	entries, err := os.ReadDir(o.NewPrivateRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("wrote into replacement root")
	}
	if st := s.Status(); !st.Closed || st.NativePackets != 0 {
		t.Fatal("foreign root sent request")
	}
}

func TestFullWindowClosedObserverFailureStopsOwner(t *testing.T) {
	o := closedSessionOptions(t)
	s, err := OpenFullWindowClosed(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.server.Close()
	receipt, err := s.RunWindow(context.Background(), 1, "")
	if err == nil {
		t.Fatal("failed sink advanced")
	}
	st := s.Status()
	if !st.Closed || st.NextWindowIndex != 1 || receipt.ReportStatus != "stopped" || receipt.UnrunCampaignArms != 7319 {
		t.Fatalf("failure lost unrun accounting: %+v %+v", receipt, st)
	}
	terminalRaw, terminalErr := os.ReadFile(filepath.Join(o.NewPrivateRoot, "terminal.json"))
	var terminal FullWindowClosedStatus
	if terminalErr != nil || json.Unmarshal(terminalRaw, &terminal) != nil || terminal.LastReceiptSHA256 != receipt.ReceiptSHA256 {
		t.Fatal("terminal lost final receipt chain")
	}
	raw, _ := json.Marshal(receipt)
	var summary map[string]any
	_ = json.Unmarshal(raw, &summary)
	if summary["attempted_arms"] != float64(1) || summary["failed_attempted_arms"] != float64(1) || summary["first_unrun_ordinal"] != float64(1) || summary["first_unrun_arm"] != "jev_selective" {
		t.Fatalf("failed attempt mislabeled unrun: %s", raw)
	}
	if _, err := s.RunWindow(context.Background(), 1, ""); err == nil {
		t.Fatal("failure allowed retry")
	}
	if after := s.Status(); after.ModelAttempts != st.ModelAttempts || after.JEVAttempts != st.JEVAttempts {
		t.Fatal("failure resent")
	}
}

func TestFullWindowClosedReportOrphanStopsNextWindow(t *testing.T) {
	for _, kind := range []string{"file", "empty_dir", "replaced_dir"} {
		t.Run(kind, func(t *testing.T) {
			o := closedSessionOptions(t)
			s, err := OpenFullWindowClosed(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			first, err := s.RunWindow(context.Background(), 1, "")
			if err != nil {
				t.Fatal(err)
			}
			report := filepath.Join(o.NewPrivateRoot, "window-000001")
			switch kind {
			case "file":
				err = os.WriteFile(filepath.Join(report, "orphan.json"), []byte("orphan"), 0600)
			case "empty_dir":
				err = os.Mkdir(filepath.Join(report, "orphan"), 0700)
			case "replaced_dir":
				moved := o.NewPrivateRoot + "-moved-report"
				err = os.Rename(report, moved)
				if err == nil {
					err = copyClosedReport(moved, report)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before := s.Status()
			if _, err := s.RunWindow(context.Background(), 2, first.ReceiptSHA256); err == nil {
				t.Fatal("report inventory change allowed next window")
			}
			after := s.Status()
			if !after.Closed || after.ModelAttempts != before.ModelAttempts || after.JEVAttempts != before.JEVAttempts {
				t.Fatal("orphan allowed inference")
			}
		})
	}
}
func copyClosedReport(from, to string) error {
	return filepath.WalkDir(from, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(from, path)
		if e != nil {
			return e
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.Mkdir(target, 0700)
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(target, raw, 0600)
	})
}

func TestFullWindowClosedFirstUnrunRetainsFinalWindowDenominator(t *testing.T) {
	prepared, _ := fixtureWindowCampaign(t)
	scope := prepared.scope
	last := len(scope.Windows)
	var rows []windowAcquisitionRow
	for _, slot := range scope.Slots[scope.Windows[last-1].FirstOrdinal-1:] {
		for _, arm := range slot.ArmOrder {
			rows = append(rows, windowAcquisitionRow{Ordinal: slot.Ordinal, Arm: arm, Attempted: true})
		}
	}
	ordinal, arm := closedFirstUnrun(scope, last, rows)
	if ordinal != 0 || arm != "" {
		t.Fatal("invented extra unrun slot beyond 3660")
	}
	rows[len(rows)-1].Attempted = false
	ordinal, arm = closedFirstUnrun(scope, last, rows)
	if ordinal != 3660 || arm != rows[len(rows)-1].Arm {
		t.Fatal("last unrun arm omitted")
	}
}

func closedSessionSchemas(t *testing.T, dsn string) []string {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT schema_name FROM information_schema.schemata ORDER BY schema_name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "ai_gateway_pair_") {
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}
func TestFullWindowClosedCancelDuringOpen(t *testing.T) {
	o := closedSessionOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	s, err := OpenFullWindowClosed(ctx, o)
	if err != nil {
		return
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.Status().Closed {
		t.Fatal("cancelled construction remained open")
	}
}
