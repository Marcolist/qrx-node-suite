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
	// ErrAlreadyInstalled means the manifest's version for this component
	// is already the one currently active -- Install returns this early,
	// before ever calling store.Store.Stage, instead of letting a
	// same-version "reinstall" reach the store layer at all (which would
	// otherwise reuse -- and, on a failed extraction, delete -- the live
	// release directory; see store.ErrAlreadyActive and the F08 fix for an
	// external security audit's finding).
	ErrAlreadyInstalled = errors.New("updates: this version is already installed and active")
)
