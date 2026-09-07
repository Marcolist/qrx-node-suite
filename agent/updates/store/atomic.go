package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// readPointer resolves a pointer (current/previous/staged) to the version it
// names. Pointers are normally a relative symlink ("releases/<version>");
// where symlinks aren't available (see writePointerAtomic), a pointer is a
// plain text file containing just the version string. Returns ok=false, no
// error, if the pointer doesn't exist yet.
func (s *Store) readPointer(name string) (v string, ok bool, err error) {
	path := s.pointerPath(name)
	target, err := os.Readlink(path)
	if err == nil {
		return filepath.Base(target), true, nil
	}
	if !isNotSymlink(err) {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read pointer %s: %w", name, err)
	}
	// Not a symlink -- try the plain-text fallback format.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read pointer %s: %w", name, err)
	}
	return string(trimTrailingNewline(data)), true, nil
}

// writePointerAtomic makes name (current/previous/staged) point at version,
// atomically: build the new pointer under a temp name, then rename it over
// the real path. os.Rename is atomic on the same filesystem on every
// platform this project targets, so a crash mid-write never leaves a
// half-updated pointer -- readers see either the old or the new target,
// never a partial one.
func (s *Store) writePointerAtomic(name, version string) error {
	if err := os.MkdirAll(s.Root(), 0o755); err != nil {
		return fmt.Errorf("create component root: %w", err)
	}
	path := s.pointerPath(name)
	tmp := path + ".tmp-" + randHex(8)
	relTarget := filepath.Join("releases", version)

	if err := os.Symlink(relTarget, tmp); err != nil {
		if !isSymlinkUnsupported(err) {
			return fmt.Errorf("create pointer symlink %s: %w", name, err)
		}
		// Symlink not available (commonly Windows without Developer Mode /
		// elevation) -- fall back to a plain text pointer file.
		if err := os.WriteFile(tmp, []byte(version+"\n"), 0o644); err != nil {
			return fmt.Errorf("create pointer file %s: %w", name, err)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("activate pointer %s: %w", name, err)
	}
	return nil
}

// removePointer deletes a pointer if present (used to clear "staged" after
// Promote, or after DiscardStaged).
func (s *Store) removePointer(name string) error {
	err := os.Remove(s.pointerPath(name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pointer %s: %w", name, err)
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func trimTrailingNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// isNotSymlink reports whether err from os.Readlink means "this path exists
// but isn't a symlink" (so the plain-text fallback should be tried) as
// opposed to some other failure.
func isNotSymlink(err error) bool {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		// EINVAL on POSIX readlink of a non-symlink; Windows returns a
		// distinct "not a reparse point"-flavored error that also surfaces
		// here as a PathError whose message differs by platform, so this
		// check is intentionally broad: anything that isn't "not exist" is
		// treated as "try the fallback."
		return !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission)
	}
	return false
}

// isSymlinkUnsupported reports whether a symlink creation failure looks like
// "this platform/permission set can't make symlinks" rather than a real
// filesystem error (disk full, path too long, etc).
func isSymlinkUnsupported(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, os.ErrPermission) || isWindowsPrivilegeError(linkErr.Err)
	}
	return errors.Is(err, os.ErrPermission)
}
