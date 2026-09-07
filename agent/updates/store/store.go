package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Current returns the currently active version, or ok=false if none has
// ever been activated.
func (s *Store) Current() (v string, ok bool, err error) { return s.readPointer(pointerCurrent) }

// Previous returns the rollback target, or ok=false if there isn't one yet
// (e.g. first-ever install).
func (s *Store) Previous() (v string, ok bool, err error) { return s.readPointer(pointerPrevious) }

// Staged returns the version staged for activation, or ok=false if nothing
// is currently staged.
func (s *Store) Staged() (v string, ok bool, err error) { return s.readPointer(pointerStaged) }

// CurrentDir resolves Current() to its release directory. Returns an error
// if nothing is active yet.
func (s *Store) CurrentDir() (string, error) {
	v, ok, err := s.Current()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("store: component %q has no active version", s.component)
	}
	return s.ReleaseDir(v), nil
}

// Stage creates (or returns, if already present) the release directory for
// version and marks it "staged" -- present on disk but not yet active. The
// caller writes the verified artifact's contents into the returned
// directory; Stage itself does not know how to extract any particular
// artifact format (see agent/updates/components for per-component
// staging/extraction).
//
// Refuses to stage a version equal to the one currently active
// (ErrAlreadyActive) OR equal to the rollback target (ErrAlreadyPrevious):
// version's release directory IS the live "current" or "previous"
// directory in either case (ReleaseDir is purely a function of the version
// string), so extracting into it writes over that release's own files in
// place, and a failed extraction's DiscardStaged(true) cleanup would then
// delete a release a pointer still references -- for "current", the
// running process's own binary; for "previous", the one and only rollback
// target RollbackToPrevious can revert to. Both confirmed by direct
// reproduction (F08: a version offered again by the update source, e.g. a
// re-check on the same channel; R03, an external re-review of the F08 fix:
// staging the PREVIOUS version again -- allowed by CheckNotDowngrade with
// AllowDowngrade, and by F08's fix, which only checked "current" -- then a
// failed extraction destroyed the actual rollback target, after which
// RollbackToPrevious still reported success and left "current" pointing at
// a directory that no longer existed). A caller wanting to reactivate the
// previous version should use RollbackToPrevious itself -- an atomic
// pointer swap needing no download or re-extraction at all -- rather than
// reinstalling it through Stage.
func (s *Store) Stage(version string) (dir string, err error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	if cur, ok, err := s.Current(); err != nil {
		return "", err
	} else if ok && cur == version {
		return "", fmt.Errorf("%w: %q is already the active version for component %q", ErrAlreadyActive, version, s.component)
	}
	if prev, ok, err := s.Previous(); err != nil {
		return "", err
	} else if ok && prev == version {
		return "", fmt.Errorf("%w: %q is the rollback target for component %q -- use RollbackToPrevious instead of reinstalling it", ErrAlreadyPrevious, version, s.component)
	}
	dir = s.ReleaseDir(version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("stage %s: create release dir: %w", version, err)
	}
	if err := s.writePointerAtomic(pointerStaged, version); err != nil {
		return "", fmt.Errorf("stage %s: %w", version, err)
	}
	return dir, nil
}

// DiscardStaged removes the staged pointer and, if requested, the release
// directory it pointed to (set removeFiles=false to keep a partially-staged
// download around for a retry). Used when verification or health-checking
// fails before Promote.
func (s *Store) DiscardStaged(removeFiles bool) error {
	staged, ok, err := s.Staged()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if removeFiles {
		if err := os.RemoveAll(s.ReleaseDir(staged)); err != nil {
			return fmt.Errorf("discard staged %s: %w", staged, err)
		}
	}
	return s.removePointer(pointerStaged)
}

// Promote activates the currently staged version atomically: the old
// "current" becomes "previous" (kept on disk for rollback), the staged
// version becomes the new "current", and the staged pointer is cleared.
// Each pointer swap is itself atomic (writePointerAtomic); a crash between
// the two swaps leaves current already advanced and previous not yet
// updated in the worst case, never a torn/partial current pointer.
func (s *Store) Promote() error {
	staged, ok, err := s.Staged()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("store: component %q has nothing staged to promote", s.component)
	}
	if cur, ok, err := s.Current(); err != nil {
		return err
	} else if ok {
		if err := s.writePointerAtomic(pointerPrevious, cur); err != nil {
			return fmt.Errorf("promote %s: update previous pointer: %w", staged, err)
		}
	}
	if err := s.writePointerAtomic(pointerCurrent, staged); err != nil {
		return fmt.Errorf("promote %s: update current pointer: %w", staged, err)
	}
	return s.removePointer(pointerStaged)
}

// PromoteVersion activates an already-installed version directly -- it must
// already exist under releases/ -- without going through Stage/Promote.
// Used for switching between several already-installed QRX Core versions
// (docs/updates.md#qrx-core-version-selection), where there is nothing to
// download or extract: the version is already on disk and the operation is
// just an atomic pointer swap, same as Promote's.
func (s *Store) PromoteVersion(version string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if _, err := os.Stat(s.ReleaseDir(version)); err != nil {
		return fmt.Errorf("promote %s: not installed: %w", version, err)
	}
	if cur, ok, err := s.Current(); err != nil {
		return err
	} else if ok {
		if cur == version {
			return nil // already active
		}
		if err := s.writePointerAtomic(pointerPrevious, cur); err != nil {
			return fmt.Errorf("promote %s: update previous pointer: %w", version, err)
		}
	}
	return s.writePointerAtomic(pointerCurrent, version)
}

// RollbackToPrevious swaps current and previous, so a second Rollback call
// undoes the first. Fails if there is no previous version recorded.
func (s *Store) RollbackToPrevious() error {
	prev, ok, err := s.Previous()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("store: component %q has no previous version to roll back to", s.component)
	}
	cur, curOK, err := s.Current()
	if err != nil {
		return err
	}
	if err := s.writePointerAtomic(pointerCurrent, prev); err != nil {
		return fmt.Errorf("rollback: update current pointer: %w", err)
	}
	if curOK {
		if err := s.writePointerAtomic(pointerPrevious, cur); err != nil {
			return fmt.Errorf("rollback: update previous pointer: %w", err)
		}
	}
	return nil
}

// InstalledVersions lists every version directory present under releases/,
// newest-looking-first is NOT guaranteed (sorted lexically) -- callers
// needing semantic order should sort with agent/version.Compare.
func (s *Store) InstalledVersions() ([]string, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.releasesRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list installed versions: %w", err)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	return versions, nil
}

// Prune deletes installed release directories beyond current, previous, and
// the `retain` most-recently-installed others, per
// docs/updates.md#rollback-system ("retain N previous stable versions").
// retain=2 keeps current+previous+2 extra = up to 4 versions on disk.
func (s *Store) Prune(retain int) error {
	if retain < 0 {
		retain = 0
	}
	versions, err := s.InstalledVersions()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	if cur, ok, err := s.Current(); err != nil {
		return err
	} else if ok {
		keep[cur] = true
	}
	if prev, ok, err := s.Previous(); err != nil {
		return err
	} else if ok {
		keep[prev] = true
	}
	if staged, ok, err := s.Staged(); err != nil {
		return err
	} else if ok {
		keep[staged] = true
	}

	// versions is lexically sorted; walk from the end (assumed newest-ish)
	// keeping up to `retain` extra beyond the pointer-referenced ones.
	extra := 0
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if keep[v] {
			continue
		}
		if extra < retain {
			extra++
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.releasesRoot(), v)); err != nil {
			return fmt.Errorf("prune %s: %w", v, err)
		}
	}
	return nil
}
