package qrx

import (
	"encoding/json"

	"qrx-node-suite/agent/models"
)

// Fields decodes a qrx-cli JSON response into a generic map so individual
// fields can be pulled out under several plausible names without the whole
// call failing when one field is missing or misnamed. This exists because
// QRX 0.0.7's exact response field names are unverified (see
// docs/qrx-0.0.7-interface.md) -- adapters must degrade gracefully, not
// assume a fixed shape.
func Fields(raw []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Str extracts a string field, trying each key in order.
func Str(fields map[string]json.RawMessage, keys ...string) models.Value[string] {
	for _, k := range keys {
		raw, ok := fields[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return models.Avail(s)
		}
	}
	return models.Unavail[string]("field not present in qrx-cli response")
}

// Int64 extracts an int64 field, trying each key in order.
func Int64(fields map[string]json.RawMessage, keys ...string) models.Value[int64] {
	for _, k := range keys {
		raw, ok := fields[k]
		if !ok {
			continue
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err == nil {
			return models.Avail(n)
		}
	}
	return models.Unavail[int64]("field not present in qrx-cli response")
}

// Int extracts an int field, trying each key in order.
func Int(fields map[string]json.RawMessage, keys ...string) models.Value[int] {
	v := Int64(fields, keys...)
	if !v.Ok() {
		return models.Unavail[int](v.Reason)
	}
	return models.Avail(int(v.Value))
}

// Bool extracts a bool field, trying each key in order.
func Bool(fields map[string]json.RawMessage, keys ...string) models.Value[bool] {
	for _, k := range keys {
		raw, ok := fields[k]
		if !ok {
			continue
		}
		var b bool
		if err := json.Unmarshal(raw, &b); err == nil {
			return models.Avail(b)
		}
	}
	return models.Unavail[bool]("field not present in qrx-cli response")
}

// Itoa is a tiny allocation-free-ish integer formatter, used to build CLI
// arguments (e.g. a "limit" argument) without pulling in strconv's larger
// surface for one call site. Kept trivial and tested.
func Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
