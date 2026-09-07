package version

import "testing"

func loadTestMatrix(t *testing.T) *Matrix {
	t.Helper()
	m, err := LoadMatrixFile("../data/compatibility-matrix.json")
	if err != nil {
		t.Fatalf("LoadMatrixFile: %v", err)
	}
	return m
}

func TestMatrixLookupExactMatch(t *testing.T) {
	m := loadTestMatrix(t)

	_, status := m.Lookup("0.0.7", "qrx007", "1.0.2")
	if status != Supported {
		t.Errorf("qrx007/0.0.7 = %v, want SUPPORTED", status)
	}

	_, status = m.Lookup("0.0.6", "legacy006", "1.2.5")
	if status != Partial {
		t.Errorf("legacy006/0.0.6 = %v, want PARTIAL", status)
	}

	_, status = m.Lookup("0.0.7", "legacy006", "1.2.5")
	if status != Experimental {
		t.Errorf("legacy006/0.0.7 = %v, want EXPERIMENTAL", status)
	}
}

func TestMatrixLookupNeverInfersFromVersionNumbers(t *testing.T) {
	m := loadTestMatrix(t)

	// 0.0.71 is "close" to 0.0.7 numerically but has no explicit row --
	// must be Unknown, not inferred as SUPPORTED by proximity.
	_, status := m.Lookup("0.0.71", "qrx007", "1.0.2")
	if status != Unknown {
		t.Errorf("unlisted core version = %v, want UNKNOWN (must not infer)", status)
	}

	// Adapter version outside the declared range on an otherwise-matching row.
	_, status = m.Lookup("0.0.7", "qrx007", "2.0.0")
	if status != Unknown {
		t.Errorf("qrx007 2.0.0 (outside ^1.0.0) = %v, want UNKNOWN", status)
	}
}

func TestMatrixLookupUnsupportedUnknownAdapter(t *testing.T) {
	m := loadTestMatrix(t)
	_, status := m.Lookup("0.0.8", "qrx007", "1.0.0")
	if status != Unknown {
		t.Errorf("0.0.8 has no qrx007 row = %v, want UNKNOWN", status)
	}
}

func TestBestAdapterForPrefersSupportedOverExperimental(t *testing.T) {
	m := loadTestMatrix(t)
	best, ok := m.BestAdapterFor("0.0.7")
	if !ok {
		t.Fatal("expected a best adapter for 0.0.7")
	}
	if best.AdapterName != "qrx007" || best.Status != Supported {
		t.Errorf("best adapter = %+v, want qrx007/SUPPORTED", best)
	}
}

func TestBestAdapterForUnsupportedCoreReturnsNotFound(t *testing.T) {
	m := loadTestMatrix(t)
	_, ok := m.BestAdapterFor("0.0.8")
	if ok {
		t.Error("0.0.8 has only an UNSUPPORTED/UNKNOWN row; BestAdapterFor must not select it")
	}
	_, ok = m.BestAdapterFor("9.9.9")
	if ok {
		t.Error("unlisted core version must not select an adapter")
	}
}

func TestCompatibilityProfileLoadAndSwitchSafety(t *testing.T) {
	p, err := LoadCompatibilityProfileFile("../data/compatibility-profiles/qrx-0.0.7.json")
	if err != nil {
		t.Fatalf("LoadCompatibilityProfileFile: %v", err)
	}
	if p.QRXCoreVersion != "0.0.7" {
		t.Errorf("qrx_core_version = %q, want 0.0.7", p.QRXCoreVersion)
	}
	if !p.HasCapability("velocity") {
		t.Error("expected velocity feature flag to be true for 0.0.7")
	}
	if p.HasCapability("nonexistent_flag") {
		t.Error("missing flag must default to false, not panic or true")
	}
	// The shipped profile has not been verified against a real switch, so
	// every dimension must default to unknown and therefore block.
	if p.CoreVersionSwitchSafety.AllKnown() {
		t.Error("unverified profile should not report AllKnown()")
	}
}
