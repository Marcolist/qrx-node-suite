package updates

import "errors"

var (
	// ErrUpdatesLocked means the maintenance update lock is engaged; only
	// an explicit InstallOptions.Force bypasses it.
	ErrUpdatesLocked = errors.New("updates: maintenance update lock is engaged")
	// ErrOutsideWindow means this is an automatic (non-manual) install
	// attempted outside the configured scheduled update window.
	ErrOutsideWindow = errors.New("updates: outside the scheduled automatic-update window")
	// ErrPinned means the component is pinned to a version other than the
	// one the manifest offers.
	ErrPinned = errors.New("updates: component is pinned to a different version")
	// ErrNoUpdateForComponent means the fetched manifest has no entry for
	// the requested component.
	ErrNoUpdateForComponent = errors.New("updates: manifest has no entry for this component")
	// ErrUnknownComponent means no Controller is registered for this
	// component name.
	ErrUnknownComponent = errors.New("updates: no controller registered for this component")
	// ErrNoRollbackTarget means the component's store has no previous
	// version recorded.
	ErrNoRollbackTarget = errors.New("updates: no previous version to roll back to")
)
