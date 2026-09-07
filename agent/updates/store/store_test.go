package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"qrx-node-suite/agent/updates/store"
)

func TestStageThenPromoteActivatesAndKeepsPrevious(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")

	dir, err := s.Stage("0.1.0")
	if err != nil {
		t.Fatalf("Stage(0.1.0): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agentd"), []byte("v0.1.0"), 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := s.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	cur, ok, err := s.Current()
	if err != nil || !ok || cur != "0.1.0" {
		t.Fatalf("Current() = %q, %v, %v; want 0.1.0, true, nil", cur, ok, err)
	}
	if _, ok, _ := s.Previous(); ok {
		t.Error("expected no previous version after the very first install")
	}

	// Second release.
	dir2, err := s.Stage("0.2.0")
	if err != nil {
		t.Fatalf("Stage(0.2.0): %v", err)
	}
	os.WriteFile(filepath.Join(dir2, "agentd"), []byte("v0.2.0"), 0o755)
	if err := s.Promote(); err != nil {
		t.Fatalf("Promote(0.2.0): %v", err)
	}

	cur, _, _ = s.Current()
	prev, prevOK, _ := s.Previous()
	if cur != "0.2.0" {
		t.Errorf("Current() = %q, want 0.2.0", cur)
	}
	if !prevOK || prev != "0.1.0" {
		t.Errorf("Previous() = %q, %v; want 0.1.0, true", prev, prevOK)
	}

	currentDir, err := s.CurrentDir()
	if err != nil {
		t.Fatalf("CurrentDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(currentDir, "agentd"))
	if err != nil || string(data) != "v0.2.0" {
		t.Errorf("current artifact content = %q, err %v; want v0.2.0", data, err)
	}
}

func TestRollbackSwapsCurrentAndPrevious(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")

	for _, v := range []string{"0.1.0", "0.2.0"} {
		dir, err := s.Stage(v)
		if err != nil {
			t.Fatalf("Stage(%s): %v", v, err)
		}
		os.WriteFile(filepath.Join(dir, "marker"), []byte(v), 0o644)
		if err := s.Promote(); err != nil {
			t.Fatalf("Promote(%s): %v", v, err)
		}
	}

	if err := s.RollbackToPrevious(); err != nil {
		t.Fatalf("RollbackToPrevious: %v", err)
	}
	cur, _, _ := s.Current()
	if cur != "0.1.0" {
		t.Errorf("after rollback Current() = %q, want 0.1.0", cur)
	}
	prev, _, _ := s.Previous()
	if prev != "0.2.0" {
		t.Errorf("after rollback Previous() = %q, want 0.2.0 (rollback must be itself reversible)", prev)
	}

	// Rollback again should undo the rollback.
	if err := s.RollbackToPrevious(); err != nil {
		t.Fatalf("second RollbackToPrevious: %v", err)
	}
	cur, _, _ = s.Current()
	if cur != "0.2.0" {
		t.Errorf("after second rollback Current() = %q, want 0.2.0", cur)
	}
}

func TestRollbackWithNoPreviousFails(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")
	if _, err := s.Stage("0.1.0"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if err := s.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if err := s.RollbackToPrevious(); err == nil {
		t.Fatal("expected RollbackToPrevious to fail with no previous version")
	}
}

func TestDiscardStagedRemovesFilesAndPointer(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "dashboard")
	dir, err := s.Stage("0.3.0")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>"), 0o644)

	if err := s.DiscardStaged(true); err != nil {
		t.Fatalf("DiscardStaged: %v", err)
	}
	if _, ok, _ := s.Staged(); ok {
		t.Error("expected no staged version after discard")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected release dir to be removed, stat err = %v", err)
	}
}

func TestPromoteWithNothingStagedFails(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")
	if err := s.Promote(); err == nil {
		t.Fatal("expected Promote with nothing staged to fail")
	}
}

func TestPruneKeepsCurrentPreviousAndRetainCount(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")
	versions := []string{"0.1.0", "0.2.0", "0.3.0", "0.4.0", "0.5.0"}
	for _, v := range versions {
		if _, err := s.Stage(v); err != nil {
			t.Fatalf("Stage(%s): %v", v, err)
		}
		if err := s.Promote(); err != nil {
			t.Fatalf("Promote(%s): %v", v, err)
		}
	}
	// current=0.5.0, previous=0.4.0. Prune(1) should additionally keep one
	// more (0.3.0), dropping 0.1.0 and 0.2.0.
	if err := s.Prune(1); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	installed, err := s.InstalledVersions()
	if err != nil {
		t.Fatalf("InstalledVersions: %v", err)
	}
	want := map[string]bool{"0.3.0": true, "0.4.0": true, "0.5.0": true}
	if len(installed) != len(want) {
		t.Fatalf("installed = %v, want exactly %v", installed, want)
	}
	for _, v := range installed {
		if !want[v] {
			t.Errorf("unexpected version retained after prune: %s", v)
		}
	}
}

func TestInstalledVersionsEmptyBeforeAnyStage(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")
	versions, err := s.InstalledVersions()
	if err != nil {
		t.Fatalf("InstalledVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected no installed versions, got %v", versions)
	}
}

// TestStageRefusesAlreadyActiveVersion is a regression test for the F08
// finding (external security audit): Stage(version) computes the release
// directory purely from the version string (ReleaseDir), so staging the
// SAME version that is already "current" reuses the live release
// directory in place. Before this fix, a failed extraction into that
// directory would then have its DiscardStaged(true) cleanup delete the
// still-active release -- confirmed by direct reproduction (stage the
// same version again, corrupt the artifact write to make Extract fail,
// discard staged with removeFiles=true, and watch the live release's own
// files disappear). This proves Stage now refuses outright instead.
func TestStageRefusesAlreadyActiveVersion(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")

	dir, err := s.Stage("1.0.0")
	if err != nil {
		t.Fatalf("Stage(1.0.0): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agentd"), []byte("v1.0.0 binary"), 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := s.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Re-staging the now-active version must be refused, not silently
	// hand back the live release directory.
	if _, err := s.Stage("1.0.0"); !errors.Is(err, store.ErrAlreadyActive) {
		t.Fatalf("Stage(1.0.0) again = %v, want ErrAlreadyActive", err)
	}

	// The live release must be completely untouched -- prove there is
	// nothing staged to discard, so a caller's error-path cleanup
	// (DiscardStaged) has nothing destructive to act on.
	if _, ok, _ := s.Staged(); ok {
		t.Error("expected nothing to be staged after Stage refused the already-active version")
	}
	curDir, err := s.CurrentDir()
	if err != nil {
		t.Fatalf("CurrentDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(curDir, "agentd")); err != nil {
		t.Fatalf("the live release's own file is gone: %v", err)
	}

	// A genuinely different version must still stage normally.
	if _, err := s.Stage("2.0.0"); err != nil {
		t.Fatalf("Stage(2.0.0) (a real new version) should succeed: %v", err)
	}
}

// TestStageRefusesPreviousVersionToo is a regression test for the R03
// finding (external re-review of the F08 fix): F08's fix only refused
// staging the CURRENT version, not the PREVIOUS (rollback-target) one.
// Since AllowDowngrade permits re-offering an older version, and Stage's
// release directory is purely a function of the version string, staging
// the previous version again reused its own live directory as a staging
// target -- and a failed extraction's DiscardStaged(true) cleanup then
// deleted the one and only rollback target, after which
// RollbackToPrevious still reported success and left "current" pointing
// at a directory that no longer existed. Confirmed by direct
// reproduction before this fix. This proves Stage now refuses that too,
// and that the previous release's own files, and rollback itself, both
// keep working.
func TestStageRefusesPreviousVersionToo(t *testing.T) {
	base := t.TempDir()
	s := store.New(base, "agent")

	dir1, err := s.Stage("1.0.0")
	if err != nil {
		t.Fatalf("Stage(1.0.0): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir1, "agentd"), []byte("v1.0.0 binary"), 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := s.Promote(); err != nil {
		t.Fatalf("Promote(1.0.0): %v", err)
	}

	dir2, err := s.Stage("2.0.0")
	if err != nil {
		t.Fatalf("Stage(2.0.0): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "agentd"), []byte("v2.0.0 binary"), 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := s.Promote(); err != nil {
		t.Fatalf("Promote(2.0.0): %v", err)
	}
	// current=2.0.0, previous=1.0.0.

	if _, err := s.Stage("1.0.0"); !errors.Is(err, store.ErrAlreadyPrevious) {
		t.Fatalf("Stage(1.0.0) (the previous/rollback-target version) = %v, want ErrAlreadyPrevious", err)
	}

	// Nothing must have been staged, and the previous release's own file
	// must be completely intact.
	if _, ok, _ := s.Staged(); ok {
		t.Error("expected nothing staged after Stage refused the previous version")
	}
	if _, err := os.Stat(filepath.Join(dir1, "agentd")); err != nil {
		t.Fatalf("the previous release's own file is gone: %v", err)
	}

	// Rollback must still actually work afterward.
	if err := s.RollbackToPrevious(); err != nil {
		t.Fatalf("RollbackToPrevious: %v", err)
	}
	cur, ok, err := s.Current()
	if err != nil || !ok || cur != "1.0.0" {
		t.Fatalf("Current() after rollback = %q, %v, %v; want 1.0.0, true, nil", cur, ok, err)
	}
	if _, err := os.Stat(filepath.Join(s.ReleaseDir(cur), "agentd")); err != nil {
		t.Fatalf("current (after rollback) points at a directory missing its own file: %v", err)
	}
}

// TestComponentPathTraversalRejected is a regression test for the F03
// finding (external security audit): a component name reaching Store
// unvalidated let filepath.Join(baseDir, component) resolve outside
// baseDir, so Current()/Previous() would read a pointer file named
// "current"/"previous" from an attacker-chosen directory instead of
// failing. The primary fix is agent/updates.Manager.Check validating
// component against its Controllers allowlist before ever calling
// store.New; this test exercises the store layer's own defense-in-depth
// (Store.validate) directly, since that's the last line of defense for any
// future caller that reaches store.New with unvalidated input.
func TestComponentPathTraversalRejected(t *testing.T) {
	base := t.TempDir()

	// Plant a file OUTSIDE base that a traversal could read if unvalidated:
	// baseDir/agent-updates/../../outside/current would resolve to
	// <parent-of-base>/outside/current.
	outsideDir := filepath.Join(filepath.Dir(base), "outside-"+filepath.Base(base))
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("mkdir outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)
	secret := filepath.Join(outsideDir, "current")
	if err := os.WriteFile(secret, []byte("SECRET-VERSION-STRING\n"), 0o644); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}

	traversal := "../" + filepath.Base(outsideDir)
	s := store.New(base, traversal)

	if v, ok, err := s.Current(); err == nil {
		t.Fatalf("Current() with traversal component = %q, %v, nil error; want ErrInvalidComponent, got value read: %q", v, ok, v)
	} else if !errors.Is(err, store.ErrInvalidComponent) {
		t.Fatalf("Current() error = %v; want errors.Is(err, store.ErrInvalidComponent)", err)
	}

	if _, err := s.Stage("1.0.0"); !errors.Is(err, store.ErrInvalidComponent) {
		t.Fatalf("Stage() error = %v; want ErrInvalidComponent", err)
	}
	if _, err := s.InstalledVersions(); !errors.Is(err, store.ErrInvalidComponent) {
		t.Fatalf("InstalledVersions() error = %v; want ErrInvalidComponent", err)
	}
	if err := s.PromoteVersion("1.0.0"); !errors.Is(err, store.ErrInvalidComponent) {
		t.Fatalf("PromoteVersion() error = %v; want ErrInvalidComponent", err)
	}

	for _, bad := range []string{"", ".", "..", "..\\outside", "sub/dir", "a/../../etc"} {
		s := store.New(base, bad)
		if _, _, err := s.Current(); !errors.Is(err, store.ErrInvalidComponent) {
			t.Errorf("component %q: Current() error = %v; want ErrInvalidComponent", bad, err)
		}
	}
}
