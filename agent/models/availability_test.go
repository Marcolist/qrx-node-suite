package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAvailableZeroValueIsSerialized(t *testing.T) {
	raw, err := json.Marshal(Avail(int64(0)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"value":0`) {
		t.Fatalf("available zero disappeared from JSON: %s", raw)
	}
}

func TestAvailableFalseIsSerialized(t *testing.T) {
	raw, err := json.Marshal(Avail(false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"value":false`) {
		t.Fatalf("available false disappeared from JSON: %s", raw)
	}
}

func TestUnavailableValueIsOmitted(t *testing.T) {
	raw, err := json.Marshal(Unavail[int64]("missing"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"value"`) {
		t.Fatalf("unavailable placeholder leaked into JSON: %s", raw)
	}
}
