package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequireConfigPathExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(existing, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	missing := filepath.Join(dir, "does-not-exist.json")

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"empty path -- nothing requested, Mock-mode defaults are correct", "", false},
		{"existing path", existing, false},
		{"missing path -- explicitly requested but absent (F13)", missing, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireConfigPathExists(tc.path)
			if tc.wantErr && err == nil {
				t.Fatalf("requireConfigPathExists(%q) = nil, want an error", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("requireConfigPathExists(%q) = %v, want nil", tc.path, err)
			}
		})
	}
}
