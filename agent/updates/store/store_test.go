package store_test

import (
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
