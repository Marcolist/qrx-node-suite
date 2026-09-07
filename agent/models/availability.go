// Package models holds typed, normalized domain models shared by adapters, the
// Guardian, the REST API, and (via JSON) the dashboard.
//
// Every field that QRX Core might not expose is wrapped in Value[T] rather than
// left as a bare zero value, so "unknown" can never be confused with "actually
// zero" (see docs/architecture.md, principle 11).
package models

// AvailabilityState describes whether a value is real, missing, unsupported by
// the active adapter/QRX version, or failed to load.
type AvailabilityState string

const (
	// Available means Value holds real data from QRX Core (via an adapter) or a
	// local measurement.
	Available AvailabilityState = "available"
	// Unavailable means the data could not be obtained right now (e.g. QRX Core
	// is offline, or the field was absent from an otherwise successful response).
	Unavailable AvailabilityState = "unavailable"
	// Unsupported means the active QRX Core version/adapter does not expose this
	// data at all -- distinct from a transient failure.
	Unsupported AvailabilityState = "unsupported"
	// Error means retrieving the value failed and the failure itself is
	// noteworthy (Reason carries detail). Never silently downgraded to Unavailable.
	Error AvailabilityState = "error"
)

// Value wraps a typed field with explicit availability. JSON consumers must
// check State before trusting Value.
type Value[T any] struct {
	State  AvailabilityState `json:"state"`
	Value  T                 `json:"value,omitempty"`
	Reason string            `json:"reason,omitempty"`
}

// Avail wraps a known value.
func Avail[T any](v T) Value[T] {
	return Value[T]{State: Available, Value: v}
}

// Unavail marks a field as currently unavailable, with an optional reason.
func Unavail[T any](reason string) Value[T] {
	return Value[T]{State: Unavailable, Reason: reason}
}

// Unsupp marks a field as not exposed by the active QRX Core version/adapter.
func Unsupp[T any](reason string) Value[T] {
	return Value[T]{State: Unsupported, Reason: reason}
}

// Failed marks a field as failed to retrieve, distinct from merely unavailable.
func Failed[T any](reason string) Value[T] {
	return Value[T]{State: Error, Reason: reason}
}

// Ok reports whether the value is Available and safe to use.
func (v Value[T]) Ok() bool {
	return v.State == Available
}
