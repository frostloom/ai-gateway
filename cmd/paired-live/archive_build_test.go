package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveBuildScriptBuildsCommittedSourceAndRecordsHashes(t *testing.T) {
	script, err := filepath.Abs("../../scripts/build-paired-live.py")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		raw, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, raw)
		}
		return strings.TrimSpace(string(raw))
	}
	git("init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(root, "cmd/paired-live"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"go.mod":                  "module github.com/frostloom/ai-gateway\n\ngo 1.25.12\n",
		"cmd/paired-live/main.go": "package main\nimport \"fmt\"\nvar archivedCommit, archivedTree string\nfunc main() { fmt.Println(archivedCommit, archivedTree) }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	head, tree := git("rev-parse", "HEAD"), git("rev-parse", "HEAD^{tree}")
	// This would fail compilation if the script accidentally built disk source.
	dirty := []byte("this is unrelated unfinished work\n")
	if err := os.WriteFile(filepath.Join(root, "cmd/paired-live/main.go"), dirty, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "paired-live")
	cmd := exec.Command("python3", script, "--repo", root, "--output", out)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("archive build: %v: %s", err, raw)
	}
	raw, err := exec.Command(out).Output()
	if err != nil || strings.TrimSpace(string(raw)) != head+" "+tree {
		t.Fatalf("wrong binary provenance: %v %s", err, raw)
	}
	manifestRaw, err := os.ReadFile(out + ".provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := exec.Command("git", "-C", root, "archive", "--format=tar", head).Output()
	if err != nil {
		t.Fatal(err)
	}
	binHash, archiveHash := sha256.Sum256(binary), sha256.Sum256(archive)
	if manifest["commit"] != head || manifest["tree"] != tree || manifest["binary_sha256"] != hex.EncodeToString(binHash[:]) || manifest["source_archive_sha256"] != hex.EncodeToString(archiveHash[:]) {
		t.Fatalf("incorrect manifest: %s", manifestRaw)
	}
	buildInfo, err := exec.Command("go", "version", "-m", out).Output()
	if err != nil || strings.Contains(string(buildInfo), "vcs.modified=") || strings.Contains(string(buildInfo), "vcs.revision=") {
		t.Fatalf("archive builder invented VCS metadata: %v %s", err, buildInfo)
	}
	current, err := os.ReadFile(filepath.Join(root, "cmd/paired-live/main.go"))
	if err != nil || string(current) != string(dirty) {
		t.Fatal("build changed workspace source")
	}
	if raw, err := exec.Command("python3", script, "--repo", root, "--output", out).CombinedOutput(); err == nil {
		t.Fatalf("build overwrote existing output: %s", raw)
	}
}

func TestArchiveBuildRevisionChecksCommittedTreeAndFrozenInputs(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		raw, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, raw)
		}
		return strings.TrimSpace(string(raw))
	}
	write := func(path, content string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("main.go", "package main\n")
	write("eval/task-definitions/case.yaml", "committed definition\n")
	write("eval/runtime-profiles/profile.yaml", "committed profile\n")
	write(".gitignore", "eval/runtime-profiles/ignored.yaml\n")
	git("add", ".")
	git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	head, tree := git("rev-parse", "HEAD"), git("rev-parse", "HEAD^{tree}")
	write("main.go", "dirty source excluded from archive\n")
	write("frontend/page.tsx", "unrelated work\n")
	write(".tmp/probe.go", "uncompiled probe\n")
	oldCommit, oldTree := archivedCommit, archivedTree
	archivedCommit, archivedTree = head, tree
	t.Cleanup(func() { archivedCommit, archivedTree = oldCommit, oldTree })
	if err := verifyExecutionRevision(root, head, "", true); err != nil {
		t.Fatalf("runtime did not select archive provenance: %v", err)
	}
	if err := verifyExecutionRevisionWithArchive(root, head, "", true, head, tree); err != nil {
		t.Fatalf("valid archive build rejected because workspace is dirty: %v", err)
	}
	for _, tc := range []struct{ name, requested, commit, tree string }{
		{"wrong requested revision", strings.Repeat("a", 40), head, tree},
		{"wrong commit", head, strings.Repeat("a", 40), tree},
		{"wrong tree", head, head, strings.Repeat("a", 40)},
		{"partial commit", head, head, ""},
		{"partial tree", head, "", tree},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if verifyExecutionRevisionWithArchive(root, tc.requested, head, false, tc.commit, tc.tree) == nil {
				t.Fatal("invalid archive provenance accepted")
			}
		})
	}
	for _, path := range []string{"eval/task-definitions/case.yaml", "eval/runtime-profiles/profile.yaml", "eval/task-definitions/untracked.yaml", "eval/runtime-profiles/untracked.yaml"} {
		t.Run(path, func(t *testing.T) {
			write(path, "changed frozen input\n")
			if verifyExecutionRevisionWithArchive(root, head, "", true, head, tree) == nil {
				t.Fatal("changed frozen input accepted")
			}
			if strings.Contains(path, "untracked") {
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
			} else {
				git("checkout", "HEAD", "--", path)
			}
		})
	}
	write("eval/runtime-profiles/ignored.yaml", "ignored but read at runtime\n")
	if verifyExecutionRevisionWithArchive(root, head, "", true, head, tree) == nil {
		t.Fatal("ignored untracked frozen input accepted")
	}
}
