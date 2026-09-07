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

import "path/filepath"

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

const (
	pointerCurrent  = "current"
	pointerPrevious = "previous"
	pointerStaged   = "staged"
)
