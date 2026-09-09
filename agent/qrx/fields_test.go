package qrx

import "testing"

func TestFieldsUnwrapsQRX007Result(t *testing.T) {
	fields, err := Fields([]byte(`{"ok":true,"method":"getnodestatus","result":{"network":"mainnet","local_height":7}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Str(fields, "network"); !got.Ok() || got.Value != "mainnet" {
		t.Fatalf("network = %#v", got)
	}
	if got := Int64(fields, "local_height"); !got.Ok() || got.Value != 7 {
		t.Fatalf("local_height = %#v", got)
	}
}

func TestFieldsAcceptsLegacyFlatObject(t *testing.T) {
	fields, err := Fields([]byte(`{"network":"alpha","height":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Str(fields, "network"); !got.Ok() || got.Value != "alpha" {
		t.Fatalf("network = %#v", got)
	}
}

func TestStrAcceptsJSONNumber(t *testing.T) {
	fields, err := Fields([]byte(`{"result":{"protocolversion":6}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Str(fields, "protocolversion"); !got.Ok() || got.Value != "6" {
		t.Fatalf("protocolversion = %#v", got)
	}
}

func TestResultUnwrapsArray(t *testing.T) {
	got, err := Result([]byte(`{"ok":true,"result":[{"height":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[{"height":1}]` {
		t.Fatalf("Result() = %s", got)
	}
}
