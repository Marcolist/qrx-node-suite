//go:build windows

package store

import (
	"errors"
	"syscall"
)

// errPrivilegeNotHeld is Windows ERROR_PRIVILEGE_NOT_HELD (1314), returned
// by CreateSymbolicLink when the caller lacks SeCreateSymbolicLinkPrivilege
// (i.e. not running elevated / Developer Mode not enabled). This is the
// common case that makes the plain-text pointer fallback necessary on
// Windows -- see docs/deployment.md's Windows notes.
const errPrivilegeNotHeld = syscall.Errno(1314)

func isWindowsPrivilegeError(err error) bool {
	return errors.Is(err, errPrivilegeNotHeld)
}
