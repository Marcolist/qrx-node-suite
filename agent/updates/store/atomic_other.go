//go:build !windows

package store

func isWindowsPrivilegeError(err error) bool { return false }
