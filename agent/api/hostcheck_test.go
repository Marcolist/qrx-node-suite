package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsAllowedHost is the F09 fix (external security audit: DNS
// rebinding against an unauthenticated read endpoint or the LAN-bound
// dashboard) -- only a loopback or private-network address (with or
// without a port) may reach the API; any public-looking hostname,
// including one an attacker's DNS record could be pointed at this
// server's IP after the fact, must not.
func TestIsAllowedHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1:8787", true},
		{"127.0.0.1", true},
		{"localhost:8787", true},
		{"localhost", true},
		{"[::1]:8787", true},
		{"::1", true},
		{"192.168.1.5:8787", true}, // RFC 1918
		{"10.0.0.12:8787", true},   // RFC 1918
		{"172.16.4.4:8787", true},  // RFC 1918
		{"169.254.1.1:8787", true}, // link-local
		{"evil.example.com:8787", false},
		{"evil.example.com", false},
		{"8.8.8.8:8787", false}, // public IP
		{"8.8.8.8", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := isAllowedHost(tc.host); got != tc.want {
				t.Errorf("isAllowedHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestIsAllowedHostAllowsAssignedInterfaceAddresses(t *testing.T) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || ip.IsUnspecified() {
			continue
		}
		host := net.JoinHostPort(ip.String(), "8787")
		if !isAllowedHost(host) {
			t.Errorf("assigned interface address %q was rejected", host)
		}
	}
}

func TestRequireAllowedHostRejectsPublicHostname(t *testing.T) {
	called := false
	h := RequireAllowedHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Fatal("the wrapped handler must not run for a disallowed Host")
	}
}

func TestRequireAllowedHostAllowsLoopback(t *testing.T) {
	called := false
	h := RequireAllowedHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("the wrapped handler should run for an allowed Host")
	}
}
