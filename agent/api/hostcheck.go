package api

import (
	"net"
	"net/http"
)

// RequireAllowedHost wraps the whole mux (NewMux) with a defense-in-depth
// Host-header check against DNS rebinding, per an external security
// audit's F09 finding: a malicious external domain can be made to resolve
// to 127.0.0.1 or this machine's LAN address after the browser's initial
// same-origin check passes, defeating same-origin-based assumptions
// entirely -- the request still lands here with whatever Host header the
// attacker's page's fetch/XHR sent, unauthenticated read endpoints and
// all, regardless of which mode (QRX_DASHBOARD_BIND=local or lan)
// install.sh bound the listener to. Checking the Host header server-side
// is the standard defense (the same technique webpack-dev-server, and
// most local-service HTTP servers, use).
//
// This is explicitly scoped as defense-in-depth short of a full TLS/
// authentication overhaul (docs/security.md's F09 note): it blocks a
// request whose Host header names something that couldn't legitimately be
// how a real client on this machine or LAN addressed this server, but
// does nothing about a same-network attacker who can already reach the
// listening port directly (unauthenticated read endpoints stay
// unauthenticated; the admin token still travels in cleartext with no
// TLS).
func RequireAllowedHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAllowedHost(r.Host) {
			httpError(w, http.StatusForbidden, "request Host header not allowed -- see docs/security.md#dns-rebinding-protection")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isAllowedHost reports whether host (an HTTP request's Host header,
// hostname optionally followed by ":port") names a loopback/private address
// or a literal IP currently assigned to this machine. The last case permits a
// VPS operator to use the VPS's own public IP without permitting arbitrary
// public hostnames a DNS-rebinding attacker controls. Empty
// host is rejected (Go's net/http guarantees a non-empty Host is set from
// the request line/Host header before a handler ever runs, so an empty
// value here would be unexpected, not a legitimate client).
//
// Deliberately does NOT special-case "localhost"-style mDNS (".local")
// hostnames or any operator-configured DNS name for this machine --
// docs/installer.md's own documented access pattern is always by raw IP
// (http://<detected-lan-ip>:port), never a hostname, and a fixed allowlist
// of one detected-at-install-time IP would go stale the moment DHCP
// reassigns it. A deployment that genuinely needs hostname-based access
// over the LAN is outside this fix's scope (see docs/security.md's F09
// note on what's still deferred: TLS, or authenticating read endpoints).
func isAllowedHost(host string) bool {
	if host == "" {
		return false
	}
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	// net.SplitHostPort errors on a bare hostname/IP with no ":port" --
	// hostname already holds the original value in that case, correctly.

	if hostname == "localhost" {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		return false // a real hostname other than "localhost" -- never allowed, see doc comment
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || isLocalInterfaceIP(ip)
}

func isLocalInterfaceIP(candidate net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var local net.IP
		switch value := addr.(type) {
		case *net.IPNet:
			local = value.IP
		case *net.IPAddr:
			local = value.IP
		}
		if local != nil && local.Equal(candidate) {
			return true
		}
	}
	return false
}
