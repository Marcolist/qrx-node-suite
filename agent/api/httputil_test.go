package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"qrx-node-suite/agent/api"
)

// TestOversizedRequestBodyIsRejected is the F10 fix (external security
// audit: "no request body size limits") -- decodeJSON wraps every request
// body in http.MaxBytesReader, so a body larger than the limit is
// rejected with 413 rather than read fully into memory.
// POST /api/v1/updates/check is unauthenticated and calls decodeJSON
// before doing anything else, so an empty Deps{} is enough to exercise it.
func TestOversizedRequestBodyIsRejected(t *testing.T) {
	mux := api.NewMux(&api.Deps{})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oversized := bytes.Repeat([]byte("a"), 128*1024) // well over the 64 KiB limit
	body := append([]byte(`{"component":"`), oversized...)
	body = append(body, []byte(`"}`)...)

	resp, err := http.Post(srv.URL+"/api/v1/updates/check", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /updates/check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

// TestNormalSizedRequestBodyIsAccepted confirms the limit isn't so tight
// it rejects an ordinary request -- decodeJSON should still reach the
// underlying handler logic for a body well within the limit.
func TestNormalSizedRequestBodyIsAccepted(t *testing.T) {
	mux := api.NewMux(&api.Deps{})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/updates/check", "application/json", bytes.NewReader([]byte(`{"component":"agent"}`)))
	if err != nil {
		t.Fatalf("POST /updates/check: %v", err)
	}
	defer resp.Body.Close()

	// Deps{} has no Updates manager configured, so this specific request
	// fails downstream (503) -- the point here is only that it gets PAST
	// decodeJSON, i.e. not 413/400 from the body-size limit itself.
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, a normal-sized body must not be rejected as too large", resp.StatusCode)
	}
}
