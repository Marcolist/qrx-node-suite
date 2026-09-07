// Package store implements atomic, staged, rollback-capable on-disk storage
// for one OTA-managed component, per docs/updates.md#atomic-component-updates:
//
//	<baseDir>/<component>/
//	    releases/<version>/...   permanent, content-addressed by version
//	    current   -> releases/<version>   (symlink; text-file fallback where symlinks aren't available)
//	    previous  -> releases/<version>
//	    staged    -> releases/<version>   (present only mid-install, before Promote)
//
// This mirrors the disk layout in docs/deployment.md (e.g.
// /opt/qrx/versions/0.0.7, /opt/qrx/current -> 0.0.7) but is generic over
// any component name, so the same code manages the agent, the dashboard,
// each adapter, and compatibility profiles.
package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidComponent means a component name isn't safe to use as a single
// filesystem path segment. This is defense-in-depth: the primary defense
// against a path-traversal component name is the caller
// (agent/updates.Manager) validating component against its Controllers
// allowlist before ever building a Store (see the F03 fix in
// agent/updates/manager.go's Check). This rejects anything that reaches the
// store layer anyway -- a "../../etc"-style component would otherwise let
// Root() resolve outside baseDir and have readPointer return the contents
// of a file named "current"/"previous" from an attacker-chosen directory.
var ErrInvalidComponent = errors.New("store: invalid component name")

// ErrAlreadyActive means Stage was asked to stage the version that is
// already the component's current one -- see Stage's doc comment for why
// this is refused outright rather than silently reusing the live release
// directory.
var ErrAlreadyActive = errors.New("store: version is already the active version")

// Store manages one component's on-disk releases and current/previous/staged
// pointers under baseDir/component.
type Store struct {
	baseDir   string
	component string
}

// New builds a Store rooted at baseDir/component. It does not touch the
// filesystem until a method is called.
func New(baseDir, component string) *Store {
	return &Store{baseDir: baseDir, component: component}
}

// Component returns the component name this Store manages.
func (s *Store) Component() string { return s.component }

// Root is baseDir/component.
func (s *Store) Root() string {
	return filepath.Join(s.baseDir, s.component)
}

// ReleaseDir is the permanent, versioned directory for one release. Content
// is written here during staging and never modified in place afterward.
func (s *Store) ReleaseDir(v string) string {
	return filepath.Join(s.Root(), "releases", v)
}

func (s *Store) releasesRoot() string {
	return filepath.Join(s.Root(), "releases")
}

func (s *Store) pointerPath(name string) string {
	return filepath.Join(s.Root(), name)
}

// validate rejects a component name that isn't a single, plain path
// segment: empty, "." or ".." are refused, as is anything containing a
// path separator (forward or backward slash, so a value can't smuggle a
// multi-segment traversal past a caller that only checked for "..").
// filepath.Join would otherwise silently resolve such a name relative to
// baseDir instead of refusing it.
func (s *Store) validate() error {
	c := s.component
	if c == "" || c == "." || c == ".." || strings.ContainsAny(c, `/\`) {
		return fmt.Errorf("%w: %q", ErrInvalidComponent, c)
	}
	return nil
}

const (
	pointerCurrent  = "current"
	pointerPrevious = "previous"
	pointerStaged   = "staged"
)
