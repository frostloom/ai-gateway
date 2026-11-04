package taskbench

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// With only the new epoch frozen, both public V12 paths must actually try the
// selected manifest. An old-epoch recheck stops at unfrozen build anchors.
func TestWindowV12PublicPreparationSelectsFreshEpoch(t *testing.T) {
	saved := []string{fullWindowV12ManifestSHA, fullWindowV12ArchiveCommit, fullWindowV12ArchiveTree, fullWindowV11ManifestSHA, fullWindowV11ArchiveCommit, fullWindowV11ArchiveTree}
	t.Cleanup(func() {
		fullWindowV12ManifestSHA, fullWindowV12ArchiveCommit, fullWindowV12ArchiveTree = saved[0], saved[1], saved[2]
		fullWindowV11ManifestSHA, fullWindowV11ArchiveCommit, fullWindowV11ArchiveTree = saved[3], saved[4], saved[5]
	})
	fullWindowV12ManifestSHA = strings.Repeat("a", 64)
	fullWindowV12ArchiveCommit = strings.Repeat("b", 40)
	fullWindowV12ArchiveTree = strings.Repeat("c", 40)
	fullWindowV11ManifestSHA, fullWindowV11ArchiveCommit, fullWindowV11ArchiveTree = "", "", ""
	manifest := filepath.Join(t.TempDir(), "absent-manifest.json")
	assertStage := func(t *testing.T, err error, want string) {
		t.Helper()
		var stage interface{ ReadinessStage() string }
		if !errors.As(err, &stage) || stage.ReadinessStage() != want {
			t.Fatalf("public dispatch did not reach %s stage: %v", want, err)
		}
	}
	t.Run("technical_prepare", func(t *testing.T) {
		_, p, e := CheckFullWindowV12Preparation(FullWindowActualReadyOptions{manifest})
		if p != nil {
			t.Fatal("missing manifest issued preparation")
		}
		assertStage(t, e, "manifest")
	})
	t.Run("authorization_fresh_recheck", func(t *testing.T) {
		p := &FullWindowV12Preparation{ready: &FullWindowActualReady{manifestPath: manifest}}
		r, e := AuthorizeFullWindowV12Preparation(p, filepath.Join(t.TempDir(), "must-not-read-authority.json"))
		if r != nil {
			t.Fatal("missing manifest issued authority")
		}
		assertStage(t, e, "manifest")
	})
	t.Run("old_epoch_stays_unfrozen", func(t *testing.T) {
		_, p, e := CheckFullWindowV11Preparation(FullWindowActualReadyOptions{manifest})
		if p != nil {
			t.Fatal("unfrozen old epoch prepared")
		}
		assertStage(t, e, "build_anchors")
	})
}
