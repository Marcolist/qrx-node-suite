//go:build linux

package platform

import "testing"

// TestSetUseSudo is the F12 fix (external security audit): confirms
// SetUseSudo actually flips UseSudo, since cmd/agentd/main.go wires
// config.Config.QRXCoreServiceUseSudo to it through an anonymous
// interface assertion rather than importing *Systemd directly (kept
// portable to non-Linux builds -- see SetUseSudo's own doc comment) --
// a typo'd method name there would fail silently (ok == false, no-op)
// rather than a compile error, so this pins the method actually exists
// and does what its name says.
func TestSetUseSudo(t *testing.T) {
	s := &Systemd{}
	if s.UseSudo {
		t.Fatal("UseSudo should default to false")
	}
	s.SetUseSudo(true)
	if !s.UseSudo {
		t.Fatal("SetUseSudo(true) should set UseSudo")
	}
	s.SetUseSudo(false)
	if s.UseSudo {
		t.Fatal("SetUseSudo(false) should clear UseSudo")
	}
}

// TestNewImplementsSudoable confirms platform.New()'s return value
// (the ServiceManager interface) satisfies the anonymous
// interface{ SetUseSudo(bool) } assertion cmd/agentd/main.go relies on --
// this is exactly what would silently stop working (UseSudo permanently
// false, sudo never used, Core service control staying broken against a
// real qrxd.service owned by a different user) if Systemd's method set
// ever changed without that call site's assertion changing to match.
func TestNewImplementsSudoable(t *testing.T) {
	mgr := New()
	if _, ok := mgr.(interface{ SetUseSudo(bool) }); !ok {
		t.Fatal("platform.New()'s ServiceManager no longer implements SetUseSudo(bool) -- cmd/agentd/main.go's UseSudo wiring would silently become a no-op")
	}
}
