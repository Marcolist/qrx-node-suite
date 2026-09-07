package api

import (
	"encoding/json"
	"errors"
	"net/http"
)

// maxRequestBodyBytes bounds every JSON request body this API accepts
// (decodeJSON is the only place any handler reads r.Body at all --
// there is no file-upload endpoint). Every real body here is a handful of
// component names/versions/tokens, never anything approaching this --
// generous headroom for future fields, while still bounding an
// unauthenticated or malicious caller streaming an effectively unbounded
// body at a handler that would otherwise buffer all of it in memory (F10
// fix, external security audit).
const maxRequestBodyBytes = 64 * 1024

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error string `json:"error"`
}

func httpError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil {
		httpError(w, http.StatusBadRequest, "request body required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}
