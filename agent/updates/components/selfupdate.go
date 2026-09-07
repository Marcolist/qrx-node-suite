package components

// SelfBinary marks a Controller whose update replaces the Agent's own
// running executable (the "agent" component, and every "adapter_*"
// component -- see binary.go's doc comment on why adapters share this
// path). agent/updates.Manager type-asserts for this interface to route
// installs through the two-phase self-update flow instead of the
// synchronous Stop/Start/HealthCheck/Commit flow used for components like
// the dashboard that don't require restarting the process running the code
// that would otherwise perform the health check.
//
// Why a synchronous health check can't work here: Controller.HealthCheck is
// called by Manager.Install from inside the currently-running Agent
// process. For the dashboard, that's fine -- checking a static file exists
// doesn't require the new code to be executing. For the Agent's own binary
// (or an adapter compiled into it), the new code only starts running after
// the OS process is replaced; the still-running old process cannot
// meaningfully health-check code it isn't executing. So a self-binary
// update stages and atomically promotes the new version, then defers
// health-checking (and the resulting commit-or-rollback decision) to the
// next process startup, driven by Manager.ResumeSelfUpdate -- see
// docs/updates.md#atomic-component-updates and its documented limitation
// around crash-loop handling.
type SelfBinary interface {
	Controller
	IsSelfBinary() bool
}
