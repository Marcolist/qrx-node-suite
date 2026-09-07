package version

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CompatibilityState is the outcome of a compatibility matrix lookup.
type CompatibilityState string

const (
	Supported    CompatibilityState = "SUPPORTED"
	Partial      CompatibilityState = "PARTIAL"
	Experimental CompatibilityState = "EXPERIMENTAL"
	Unsupported  CompatibilityState = "UNSUPPORTED"
	Unknown      CompatibilityState = "UNKNOWN"
)

// MatrixEntry is one explicit, human-authored row of the compatibility
// matrix. Rows are matched by exact QRXCoreVersion + AdapterName equality --
// never by comparing version numbers to guess compatibility. AdapterVersionRange
// and DashboardVersionRange are themselves explicit data on the row, evaluated
// with Satisfies() only after the row has already been selected by exact match.
type MatrixEntry struct {
	QRXCoreVersion        string             `json:"qrx_core_version"`
	AdapterName           string             `json:"adapter_name"`
	AdapterVersionRange   string             `json:"adapter_version_range"`
	DashboardVersionRange string             `json:"dashboard_version_range"`
	Status                CompatibilityState `json:"status"`
	Notes                 string             `json:"notes,omitempty"`
}

// Matrix is the loaded compatibility matrix document.
type Matrix struct {
	MatrixVersion int           `json:"matrix_version"`
	UpdatedAt     string        `json:"updated_at"`
	Entries       []MatrixEntry `json:"entries"`
}

// LoadMatrix reads and parses a compatibility matrix JSON document.
func LoadMatrix(r io.Reader) (*Matrix, error) {
	var m Matrix
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse compatibility matrix: %w", err)
	}
	return &m, nil
}

// LoadMatrixFile loads a compatibility matrix from a file path.
func LoadMatrixFile(path string) (*Matrix, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open compatibility matrix %s: %w", path, err)
	}
	defer f.Close()
	return LoadMatrix(f)
}

// Lookup finds the compatibility state for a specific QRX Core version and
// adapter name/version. It never falls back to nearest-version inference: if
// no row's QRXCoreVersion + AdapterName matches exactly, the result is
// Unknown, full stop.
func (m *Matrix) Lookup(qrxCoreVersion, adapterName, adapterVersion string) (MatrixEntry, CompatibilityState) {
	for _, e := range m.Entries {
		if e.QRXCoreVersion != qrxCoreVersion {
			continue
		}
		if e.AdapterName != adapterName {
			continue
		}
		if e.AdapterVersionRange != "" && !Satisfies(adapterVersion, e.AdapterVersionRange) {
			continue
		}
		return e, e.Status
	}
	return MatrixEntry{}, Unknown
}

// EntriesForCore returns every row recorded for a given QRX Core version,
// e.g. to list all adapters known for it (including UNSUPPORTED rows).
func (m *Matrix) EntriesForCore(qrxCoreVersion string) []MatrixEntry {
	var out []MatrixEntry
	for _, e := range m.Entries {
		if e.QRXCoreVersion == qrxCoreVersion {
			out = append(out, e)
		}
	}
	return out
}

// BestAdapterFor returns the highest-status, matching adapter row for a QRX
// Core version, preferring SUPPORTED > PARTIAL > EXPERIMENTAL. Rows with
// status UNSUPPORTED or an AdapterName of "UNKNOWN" are never returned.
// Returns found=false if nothing usable is recorded for this core version.
func (m *Matrix) BestAdapterFor(qrxCoreVersion string) (MatrixEntry, bool) {
	rank := map[CompatibilityState]int{Supported: 3, Partial: 2, Experimental: 1}
	var best MatrixEntry
	bestRank := 0
	for _, e := range m.EntriesForCore(qrxCoreVersion) {
		if e.AdapterName == "UNKNOWN" || e.AdapterName == "" {
			continue
		}
		r, ok := rank[e.Status]
		if !ok {
			continue // UNSUPPORTED / UNKNOWN rows are never auto-selected
		}
		if r > bestRank {
			best, bestRank = e, r
		}
	}
	return best, bestRank > 0
}
