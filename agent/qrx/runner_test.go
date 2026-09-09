package qrx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBaseArgsMatchQRX007CLI(t *testing.T) {
	r := NewRunner(Config{
		Network:    "mainnet",
		DataDir:    "/var/lib/qrx",
		WalletName: "node",
	})
	want := []string{"--network", "mainnet", "--datadir", "/var/lib/qrx", "--wallet", "node"}
	if got := r.baseArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("baseArgs() = %#v, want %#v", got, want)
	}
}

func TestCallRejectsStructuredRPCErrorFromZeroExitCLI(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "qrx-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' '{\"ok\":false,\"error\":\"unauthorized\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{CLIPath: cli})
	_, err := r.Call(context.Background(), "getuptime")
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("Call() error = %v, want *RPCError", err)
	}
	if rpcErr.Message != "unauthorized" {
		t.Fatalf("RPC error message = %q", rpcErr.Message)
	}
}
