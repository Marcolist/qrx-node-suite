// Package version implements QRX Node Suite's version management core:
// the structured component version model, semantic version comparison, and
// an explicit (never inferred) compatibility matrix and compatibility
// profiles. See docs/updates.md and docs/qrx-compatibility.md.
package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var semverRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// SemVer is a parsed semantic version.
type SemVer struct {
	Major, Minor, Patch int
	Prerelease          string // "" if none
}

// ParseSemVer parses a strict major.minor.patch[-prerelease][+build] string.
func ParseSemVer(s string) (SemVer, error) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return SemVer{}, fmt.Errorf("invalid semantic version: %q", s)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return SemVer{Major: major, Minor: minor, Patch: patch, Prerelease: m[4]}, nil
}

// IsValidSemVer reports whether s parses as a semantic version.
func IsValidSemVer(s string) bool {
	_, err := ParseSemVer(s)
	return err == nil
}

// Compare returns -1, 0, or 1 comparing a to b. Panics-free: invalid input
// returns an error via CompareStrict; Compare treats invalid input as
// incomparable-least so callers doing best-effort sorts don't crash.
func Compare(a, b string) int {
	c, err := CompareStrict(a, b)
	if err != nil {
		// Best-effort fallback for callers that can't handle an error (e.g.
		// sort.Slice). Prefer CompareStrict wherever the version source isn't
		// already validated.
		if a == b {
			return 0
		}
		if a < b {
			return -1
		}
		return 1
	}
	return c
}

// CompareStrict compares two semantic versions, returning an error if either
// fails to parse.
func CompareStrict(a, b string) (int, error) {
	va, err := ParseSemVer(a)
	if err != nil {
		return 0, err
	}
	vb, err := ParseSemVer(b)
	if err != nil {
		return 0, err
	}
	if va.Major != vb.Major {
		return cmpInt(va.Major, vb.Major), nil
	}
	if va.Minor != vb.Minor {
		return cmpInt(va.Minor, vb.Minor), nil
	}
	if va.Patch != vb.Patch {
		return cmpInt(va.Patch, vb.Patch), nil
	}
	if va.Prerelease == vb.Prerelease {
		return 0, nil
	}
	// No prerelease outranks any prerelease (1.0.0 > 1.0.0-rc1).
	if va.Prerelease == "" {
		return 1, nil
	}
	if vb.Prerelease == "" {
		return -1, nil
	}
	if va.Prerelease < vb.Prerelease {
		return -1, nil
	}
	return 1, nil
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func GT(a, b string) bool  { return Compare(a, b) > 0 }
func GTE(a, b string) bool { return Compare(a, b) >= 0 }
func LT(a, b string) bool  { return Compare(a, b) < 0 }
func LTE(a, b string) bool { return Compare(a, b) <= 0 }
func EQ(a, b string) bool  { return Compare(a, b) == 0 }

var rangeRe = regexp.MustCompile(`^(>=|<=|>|<|\^|~)?\s*(.+)$`)

// Satisfies evaluates a single-comparator range string against a version.
// Supported forms: "1.2.3" (exact), ">=1.2.3", ">1.2.3", "<=1.2.3", "<1.2.3",
// "^1.2.3" (same major, >=), "~1.2.3" (same major.minor, >=), "*" (any).
func Satisfies(version, rng string) bool {
	rng = strings.TrimSpace(rng)
	if rng == "" || rng == "*" {
		return true
	}
	m := rangeRe.FindStringSubmatch(rng)
	if m == nil {
		return false
	}
	op, target := m[1], m[2]
	v, errV := ParseSemVer(version)
	t, errT := ParseSemVer(target)
	if errV != nil || errT != nil {
		return false
	}
	switch op {
	case ">=":
		return Compare(version, target) >= 0
	case ">":
		return Compare(version, target) > 0
	case "<=":
		return Compare(version, target) <= 0
	case "<":
		return Compare(version, target) < 0
	case "^":
		return v.Major == t.Major && Compare(version, target) >= 0
	case "~":
		return v.Major == t.Major && v.Minor == t.Minor && Compare(version, target) >= 0
	default:
		return Compare(version, target) == 0
	}
}
