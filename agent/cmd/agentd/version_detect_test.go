package main

import "testing"

func TestNormalizeQRXCoreVersion(t *testing.T) {
	tests := map[string]string{
		"0.0.7-velocity-phase4": "0.0.7",
		" 0.0.7 ":               "0.0.7",
		"0.0.8-rc1":             "0.0.8-rc1",
	}
	for input, want := range tests {
		if got := normalizeQRXCoreVersion(input); got != want {
			t.Errorf("normalizeQRXCoreVersion(%q) = %q, want %q", input, got, want)
		}
	}
}
