package qrx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Runner is the single centralized qrx-cli command executor described in
// docs/qrx-0.0.7-interface.md#transport. It owns argument safety (no shell
// concatenation -- every argument is a distinct exec.Cmd arg), timeouts,
// stdout/stderr capture, JSON parsing, and error normalization.
type Runner struct {
	cfg Config
}

// NewRunner builds a Runner for the given QRX Core connection config.
func NewRunner(cfg Config) *Runner {
	return &Runner{cfg: cfg}
}

// CommandError normalizes a failed qrx-cli invocation. It never loses the
// underlying stderr, so adapters can decide whether a failure means "field
// unavailable" vs. "node offline" vs. "unsupported command".
type CommandError struct {
	Command  string
	Args     []string
	ExitCode int
	Stderr   string
	Timeout  bool
	Err      error
}

func (e *CommandError) Error() string {
	if e.Timeout {
		return fmt.Sprintf("qrx-cli %s: timed out", e.Command)
	}
	stderr := strings.TrimSpace(e.Stderr)
	if stderr != "" {
		return fmt.Sprintf("qrx-cli %s: exit %d: %s", e.Command, e.ExitCode, stderr)
	}
	if e.Err != nil {
		return fmt.Sprintf("qrx-cli %s: %v", e.Command, e.Err)
	}
	return fmt.Sprintf("qrx-cli %s: exit %d", e.Command, e.ExitCode)
}

func (e *CommandError) Unwrap() error { return e.Err }

// UnknownCommand reports whether the failure looks like "qrx-cli doesn't
// know this command" as opposed to a runtime/node failure. qrx-cli's exact
// error text for this case has not been confirmed against source (see
// docs/qrx-0.0.7-interface.md) -- this is a best-effort heuristic that
// adapters should treat as a hint, not a certainty.
func (e *CommandError) UnknownCommand() bool {
	s := strings.ToLower(e.Stderr)
	return strings.Contains(s, "unknown command") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "unrecognized")
}

func (r *Runner) baseArgs() []string {
	var args []string
	if r.cfg.Network != "" {
		args = append(args, "-network="+r.cfg.Network)
	}
	if r.cfg.DataDir != "" {
		args = append(args, "-datadir="+r.cfg.DataDir)
	}
	if r.cfg.WalletName != "" {
		args = append(args, "-wallet="+r.cfg.WalletName)
	}
	return args
}

// Call runs a qrx-cli command and returns raw stdout. Every argument is
// passed as a distinct process argument (exec.CommandContext), never
// interpolated into a shell string, so command/argument injection is not
// possible regardless of what a caller passes as args.
func (r *Runner) Call(ctx context.Context, command string, args ...string) ([]byte, error) {
	timeout := r.cfg.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	allArgs := append(r.baseArgs(), command)
	allArgs = append(allArgs, args...)

	cmd := exec.CommandContext(ctx, r.cfg.cliPath(), allArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, &CommandError{Command: command, Args: args, Timeout: true, Err: ctx.Err()}
	}
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if isExitError(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return nil, &CommandError{
			Command:  command,
			Args:     args,
			ExitCode: exitCode,
			Stderr:   stderr.String(),
			Err:      err,
		}
	}
	return stdout.Bytes(), nil
}

func isExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// CallJSON runs a command and unmarshals its stdout into T. Callers must
// treat a JSON-decode failure as "response shape unconfirmed", not crash the
// Agent -- see agent/adapters/qrx007 for how fields are degraded to
// models.Unavailable rather than propagated as a panic.
func CallJSON[T any](ctx context.Context, r *Runner, command string, args ...string) (T, error) {
	var out T
	raw, err := r.Call(ctx, command, args...)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("qrx-cli %s: unexpected response shape: %w", command, err)
	}
	return out, nil
}

// Ping runs a cheap, known-safe command to confirm qrx-cli can reach qrxd at
// all, used by adapter Health() checks. It does not assert anything about
// which higher-level commands are supported.
func (r *Runner) Ping(ctx context.Context) error {
	_, err := r.Call(ctx, "getuptime")
	return err
}
