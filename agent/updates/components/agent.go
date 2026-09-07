package components

import (
	"context"
	"fmt"
	"io"

	"qrx-node-suite/agent/updates/store"
)

// Agent is the Controller for the Agent's own binary. Stop/Start are no-ops
// -- see selfupdate.go for why a self-binary update can't restart itself
// in-process. HealthCheck is meant to run only after the process has
// actually been restarted onto the new binary (Manager.ResumeSelfUpdate,
// driven by cmd/agentd at startup): it confirms the restart really landed
// on the promoted version, which is the one thing this package can verify
// on its own. Deeper liveness (can it bind its port, open its database) is
// cmd/agentd's own startup responsibility, layered on top via the top-level
// /health aggregation -- see docs/updates.md's note on self-update scope.
type Agent struct {
	Store          *store.Store
	RunningVersion string // this process's own compiled-in version, set by cmd/agentd
}

func (a *Agent) IsSelfBinary() bool              { return true }
func (a *Agent) RequiresRestart() bool           { return true }
func (a *Agent) Stop(ctx context.Context) error  { return nil }
func (a *Agent) Start(ctx context.Context) error { return nil }

func (a *Agent) HealthCheck(ctx context.Context) error {
	cur, ok, err := a.Store.Current()
	if err != nil {
		return fmt.Errorf("agent health check: %w", err)
	}
	if !ok {
		return fmt.Errorf("agent health check: no current version recorded")
	}
	if cur != a.RunningVersion {
		return fmt.Errorf("agent health check: restarted process is running version %q but store's current is %q -- restart did not land on the promoted binary",
			a.RunningVersion, cur)
	}
	return nil
}

func (a *Agent) Extract(ctx context.Context, artifact io.Reader, dir string) error {
	return extractBinary(ctx, artifact, dir)
}
