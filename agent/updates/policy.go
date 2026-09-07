package updates

import (
	"context"
	"errors"
	"fmt"
	"time"

	"qrx-node-suite/agent/storage"
)

// Update channels, per docs/updates.md#update-channels. Default is Stable;
// operators must explicitly opt into Beta/Nightly/Development.
const (
	ChannelStable      = "stable"
	ChannelBeta        = "beta"
	ChannelNightly     = "nightly"
	ChannelDevelopment = "development"
	// ChannelManual is QRX Core's default and only sane default -- it is
	// not a channel to fetch updates from, it means "never fetch/install
	// automatically, only via an explicit manual action"
	// (docs/updates.md#qrx-core-updates).
	ChannelManual = "manual"

	DefaultChannel = ChannelStable
)

var validChannels = map[string]bool{
	ChannelStable: true, ChannelBeta: true, ChannelNightly: true, ChannelDevelopment: true, ChannelManual: true,
}

// Policy wraps storage.SettingsStore with typed accessors for everything
// docs/updates.md calls an operator-configurable update policy: per-
// component channel selection, version pinning, the maintenance update
// lock, and the scheduled automatic-update window.
type Policy struct {
	settings *storage.SettingsStore
}

func NewPolicy(settings *storage.SettingsStore) *Policy {
	return &Policy{settings: settings}
}

func channelKey(component string) string { return "update_channel." + component }

// Channel returns the configured channel for a component, defaulting to
// stable if never set. QRX Core does NOT use this method -- its default is
// manual, not stable (docs/updates.md#qrx-core-updates); see ChannelOrDefault.
func (p *Policy) Channel(ctx context.Context, component string) (string, error) {
	return p.ChannelOrDefault(ctx, component, DefaultChannel)
}

// ChannelOrDefault returns the configured channel for a component, or
// fallback if none has ever been set. QRXCoreUpdateManager uses this with
// fallback=ChannelManual, since "never configured" must mean manual for QRX
// Core specifically, not the stable default every other component gets.
func (p *Policy) ChannelOrDefault(ctx context.Context, component, fallback string) (string, error) {
	v, err := p.settings.Get(ctx, channelKey(component))
	if errors.Is(err, storage.ErrSettingNotFound) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// SetChannel sets a component's update channel. Setting anything other than
// stable is the explicit opt-in docs/updates.md requires.
func (p *Policy) SetChannel(ctx context.Context, component, channel string) error {
	if !validChannels[channel] {
		return fmt.Errorf("updates: invalid channel %q", channel)
	}
	return p.settings.Set(ctx, channelKey(component), channel)
}

func pinKey(component string) string { return "pin." + component }

// Pin returns the version a component is pinned to, and whether it's
// pinned at all. An unpinned component tracks its channel normally.
func (p *Policy) Pin(ctx context.Context, component string) (v string, pinned bool, err error) {
	val, err := p.settings.Get(ctx, pinKey(component))
	if errors.Is(err, storage.ErrSettingNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// SetPin pins a component to an exact version. Especially important for
// validators: automatic upgrades must never move a pinned component,
// docs/updates.md#version-pinning.
func (p *Policy) SetPin(ctx context.Context, component, version string) error {
	return p.settings.Set(ctx, pinKey(component), version)
}

// ClearPin removes a pin, letting the component track its channel again.
func (p *Policy) ClearPin(ctx context.Context, component string) error {
	return p.settings.Delete(ctx, pinKey(component))
}

func lastManifestKey(channel string) string { return "last_manifest_released_at." + channel }

// LastManifestReleasedAt returns the released_at of the last manifest this
// Agent acted on for a channel, or "" if none yet -- used by
// manifest.CheckNotReplayed.
func (p *Policy) LastManifestReleasedAt(ctx context.Context, channel string) (string, error) {
	v, err := p.settings.Get(ctx, lastManifestKey(channel))
	if errors.Is(err, storage.ErrSettingNotFound) {
		return "", nil
	}
	return v, err
}

// RecordManifestSeen updates the last-seen released_at for a channel.
func (p *Policy) RecordManifestSeen(ctx context.Context, channel, releasedAt string) error {
	return p.settings.Set(ctx, lastManifestKey(channel), releasedAt)
}

const updateLockKey = "update_lock"

// Locked reports whether the maintenance update lock is engaged
// (docs/updates.md#update-lock) -- while true, no automatic install may
// proceed for any component.
func (p *Policy) Locked(ctx context.Context) (bool, error) {
	v, err := p.settings.Get(ctx, updateLockKey)
	if errors.Is(err, storage.ErrSettingNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == "true", nil
}

// SetLocked engages or releases the maintenance update lock.
func (p *Policy) SetLocked(ctx context.Context, locked bool) error {
	v := "false"
	if locked {
		v = "true"
	}
	return p.settings.Set(ctx, updateLockKey, v)
}

// ScheduledWindow is the allowed weekly window for AUTOMATIC Node Suite
// updates (docs/updates.md#scheduled-updates). QRX Core is never subject to
// this window -- it follows its own manual-by-default policy regardless.
type ScheduledWindow struct {
	Weekday     time.Weekday `json:"weekday"`
	StartHour   int          `json:"start_hour"`
	StartMinute int          `json:"start_minute"`
	EndHour     int          `json:"end_hour"`
	EndMinute   int          `json:"end_minute"`
}

const scheduledWindowKey = "scheduled_update_window"

// Window returns the configured scheduled update window, if any.
func (p *Policy) Window(ctx context.Context) (w ScheduledWindow, configured bool, err error) {
	err = p.settings.GetJSON(ctx, scheduledWindowKey, &w)
	if errors.Is(err, storage.ErrSettingNotFound) {
		return ScheduledWindow{}, false, nil
	}
	if err != nil {
		return ScheduledWindow{}, false, err
	}
	return w, true, nil
}

// SetWindow configures the scheduled update window.
func (p *Policy) SetWindow(ctx context.Context, w ScheduledWindow) error {
	return p.settings.SetJSON(ctx, scheduledWindowKey, w)
}

// ClearWindow removes the scheduled window (automatic updates are then
// unrestricted by time of day, still subject to the update lock).
func (p *Policy) ClearWindow(ctx context.Context) error {
	return p.settings.Delete(ctx, scheduledWindowKey)
}

// InWindow reports whether now falls inside the configured window. A
// window with End earlier in the day than Start is treated as spanning
// midnight (e.g. 23:00-01:00).
func (w ScheduledWindow) InWindow(now time.Time) bool {
	if now.Weekday() != w.Weekday {
		return false
	}
	minutesNow := now.Hour()*60 + now.Minute()
	start := w.StartHour*60 + w.StartMinute
	end := w.EndHour*60 + w.EndMinute
	if start <= end {
		return minutesNow >= start && minutesNow < end
	}
	return minutesNow >= start || minutesNow < end
}
