package adapters

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"qrx-node-suite/agent/version"
)

var (
	// ErrAdapterNotInstalled means no adapter package registered under that
	// name (nothing to import.import, discover, or activate).
	ErrAdapterNotInstalled = errors.New("adapters: not installed")
	// ErrNoCompatibleAdapter means the compatibility matrix has no
	// SUPPORTED/PARTIAL/EXPERIMENTAL row for the detected QRX Core version.
	// Per docs/updates.md, this must never fall back to silently using an
	// incompatible adapter -- QRX integration is instead marked unsupported.
	ErrNoCompatibleAdapter = errors.New("adapters: no compatible adapter for this QRX Core version")
	// ErrAdapterDisabled means the adapter exists but DisableIncompatible
	// marked it unusable for the current QRX Core version; activation
	// requires AllowUnsupported (the dashboard's manual override).
	ErrAdapterDisabled = errors.New("adapters: disabled for current QRX Core version")
	// ErrHealthCheckFailed means an adapter activated but failed its health
	// probe; the registry leaves the previous adapter (if any) active.
	ErrHealthCheckFailed = errors.New("adapters: health check failed after activation")
	// ErrNoRollbackTarget means Rollback was called with no previously
	// active adapter recorded.
	ErrNoRollbackTarget = errors.New("adapters: no previous adapter to roll back to")
)

// AdapterInfo is a discovery-time snapshot of one installed adapter.
type AdapterInfo struct {
	Name                 string
	Version              string
	SupportedQRXVersions []string
	Active               bool
	Disabled             bool
	DisabledReason       string
}

// ActivateOptions controls Activate's compatibility enforcement.
type ActivateOptions struct {
	// AllowUnsupported bypasses the disabled/compatibility check. This is
	// the dashboard's "Manual override" advanced setting
	// (docs/updates.md#manual-adapter-selection) -- callers setting this
	// must have surfaced a warning to the operator first.
	AllowUnsupported bool
}

// Registry implements the AdapterRegistry responsibilities from
// docs/updates.md: discover, report version/supported versions, select
// (automatic and manual), activate, rollback, validate compatibility, and
// disable incompatible adapters.
type Registry struct {
	mu        sync.RWMutex
	matrix    *version.Matrix
	instances map[string]Adapter
	disabled  map[string]string // name -> reason
	active    string
	previous  string
}

// NewRegistry builds a Registry against a loaded compatibility matrix. The
// matrix is the sole source of truth for compatibility (version.Matrix
// never infers from version numbers) -- see docs/qrx-compatibility.md.
func NewRegistry(matrix *version.Matrix) *Registry {
	return &Registry{
		matrix:    matrix,
		instances: make(map[string]Adapter),
		disabled:  make(map[string]string),
	}
}

func (r *Registry) getOrCreate(name string) (Adapter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.instances[name]; ok {
		return a, nil
	}
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAdapterNotInstalled, name)
	}
	a := factory()
	r.instances[name] = a
	return a, nil
}

// Discover returns every installed adapter (registered via Register) with
// its reported version, supported QRX versions, and current
// active/disabled state. Instantiating an adapter is cheap (no I/O; only
// Activate touches QRX Core), so Discover always reflects every registered
// factory.
func (r *Registry) Discover() ([]AdapterInfo, error) {
	names := Installed()
	sort.Strings(names)
	out := make([]AdapterInfo, 0, len(names))
	for _, name := range names {
		a, err := r.getOrCreate(name)
		if err != nil {
			return nil, err
		}
		r.mu.RLock()
		reason, disabled := r.disabled[name]
		active := r.active == name
		r.mu.RUnlock()
		out = append(out, AdapterInfo{
			Name:                 a.Name(),
			Version:              a.Version(),
			SupportedQRXVersions: a.SupportedQRXVersions(),
			Active:               active,
			Disabled:             disabled,
			DisabledReason:       reason,
		})
	}
	return out, nil
}

// ValidateCompatibility looks up the matrix row for (name, qrxCoreVersion)
// using the adapter's own reported Version(). This never guesses from
// nearby version numbers -- an unlisted combination is CompatibilityState
// Unknown.
func (r *Registry) ValidateCompatibility(name, qrxCoreVersion string) (version.CompatibilityState, version.MatrixEntry, error) {
	a, err := r.getOrCreate(name)
	if err != nil {
		return version.Unknown, version.MatrixEntry{}, err
	}
	entry, status := r.matrix.Lookup(qrxCoreVersion, name, a.Version())
	return status, entry, nil
}

// DisableIncompatible re-evaluates every installed adapter against
// qrxCoreVersion and disables (for automatic/default selection) any whose
// compatibility state is UNSUPPORTED or UNKNOWN. Returns the updated
// discovery snapshot.
func (r *Registry) DisableIncompatible(qrxCoreVersion string) ([]AdapterInfo, error) {
	for _, name := range Installed() {
		status, _, err := r.ValidateCompatibility(name, qrxCoreVersion)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		switch status {
		case version.Unsupported, version.Unknown:
			r.disabled[name] = fmt.Sprintf("compatibility %s for QRX Core %s", status, qrxCoreVersion)
		default:
			delete(r.disabled, name)
		}
		r.mu.Unlock()
	}
	return r.Discover()
}

// SelectAutomatic implements the startup flow from docs/updates.md#automatic-adapter-selection:
// detect QRX Core version (caller-supplied) -> consult the matrix -> pick the
// best installed, compatible adapter -> activate -> health check. If no
// exact compatible adapter is installed, it returns ErrNoCompatibleAdapter
// (or ErrAdapterNotInstalled) and does NOT fall back to an incompatible one;
// callers must mark QRX integration unsupported in that case.
func (r *Registry) SelectAutomatic(ctx context.Context, qrxCoreVersion string) (AdapterInfo, version.CompatibilityState, error) {
	if _, err := r.DisableIncompatible(qrxCoreVersion); err != nil {
		return AdapterInfo{}, version.Unknown, err
	}
	best, ok := r.matrix.BestAdapterFor(qrxCoreVersion)
	if !ok {
		return AdapterInfo{}, version.Unknown, fmt.Errorf("%w: QRX Core %s", ErrNoCompatibleAdapter, qrxCoreVersion)
	}
	if _, installed := factories[best.AdapterName]; !installed {
		return AdapterInfo{}, best.Status, fmt.Errorf("%w: %q recommended for QRX Core %s but not installed",
			ErrAdapterNotInstalled, best.AdapterName, qrxCoreVersion)
	}
	if err := r.Activate(ctx, best.AdapterName, ActivateOptions{}); err != nil {
		return AdapterInfo{}, best.Status, err
	}
	infos, err := r.Discover()
	if err != nil {
		return AdapterInfo{}, best.Status, err
	}
	for _, info := range infos {
		if info.Name == best.AdapterName {
			return info, best.Status, nil
		}
	}
	return AdapterInfo{}, best.Status, fmt.Errorf("internal: activated adapter %q missing from discovery", best.AdapterName)
}

// Activate makes name the active adapter: deactivates the current one (kept
// as the rollback target), activates and health-checks the requested one,
// and reactivates the previous adapter if the new one fails its health
// check. Manual selection with a disabled/incompatible adapter requires
// opts.AllowUnsupported (docs/updates.md#manual-adapter-selection).
func (r *Registry) Activate(ctx context.Context, name string, opts ActivateOptions) error {
	a, err := r.getOrCreate(name)
	if err != nil {
		return err
	}

	r.mu.RLock()
	reason, disabled := r.disabled[name]
	currentName := r.active
	r.mu.RUnlock()
	if disabled && !opts.AllowUnsupported {
		return fmt.Errorf("%w: %s", ErrAdapterDisabled, reason)
	}

	var current Adapter
	if currentName != "" && currentName != name {
		current, _ = r.getOrCreate(currentName)
	}
	if current != nil {
		if err := current.Deactivate(ctx); err != nil {
			return fmt.Errorf("deactivate current adapter %q: %w", currentName, err)
		}
	}

	if err := a.Activate(ctx); err != nil {
		if current != nil {
			_ = current.Activate(ctx) // best-effort: restore previous
		}
		return fmt.Errorf("activate adapter %q: %w", name, err)
	}
	if err := a.Health(ctx); err != nil {
		_ = a.Deactivate(ctx)
		if current != nil {
			_ = current.Activate(ctx)
		}
		return fmt.Errorf("%w: %v", ErrHealthCheckFailed, err)
	}

	r.mu.Lock()
	r.previous = currentName
	r.active = name
	r.mu.Unlock()
	return nil
}

// Rollback reactivates the previously active adapter, swapping active and
// previous so Rollback can itself be undone by calling Activate again.
func (r *Registry) Rollback(ctx context.Context) error {
	r.mu.RLock()
	prev := r.previous
	cur := r.active
	r.mu.RUnlock()
	if prev == "" {
		return ErrNoRollbackTarget
	}
	if err := r.Activate(ctx, prev, ActivateOptions{AllowUnsupported: true}); err != nil {
		return err
	}
	r.mu.Lock()
	r.previous = cur
	r.mu.Unlock()
	return nil
}

// Active returns the currently active adapter, or nil if QRX integration is
// currently unsupported / no adapter has been activated yet.
func (r *Registry) Active() Adapter {
	r.mu.RLock()
	name := r.active
	r.mu.RUnlock()
	if name == "" {
		return nil
	}
	a, _ := r.getOrCreate(name)
	return a
}

// ActiveName returns the active adapter's registry name, or "" if none.
func (r *Registry) ActiveName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}
