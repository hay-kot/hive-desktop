package app

import (
	"context"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// settingsFile labels settings.yaml in activity events, beside the flows and
// actions reloads that already report by file name.
const settingsFile = "settings.yaml"

// settingsReload classifies every settings.yaml field. An empty value means a
// running process adopts a change to that field; a non-empty one is why it
// cannot, phrased as what a relaunch is for. The map is exhaustive over
// settings.FieldNames() and a test fails when it is not, so adding a field to
// the schema forces the decision rather than defaulting it to silence.
var settingsReload = map[string]string{
	"version":          "",
	"polling.interval": "",
	"updates.enabled":  "",
	"updates.channel":  "",

	"notifications.enabled":  "",
	"notifications.delivery": "",
	"notifications.sound":    "",

	"appearance.theme":              "",
	"appearance.terminal_font_size": "",
	"keybindings":                   "",

	"http.enabled": "",
	"http.host":    "",
	"http.port":    "",

	"skills.auto_update": "",
	"skills.targets":     "",
	"paths.tmux":         "",

	"experimental.terminal": "terminal mode's Wails service, bearer token and routes are all fixed at composition (ADR 0037)",

	"development.mocks.mode":         "mock mode decides which objects exist and re-resolves the path snapshot",
	"development.instance.id":        "the instance label is read when the development instance is prepared",
	"development.github.api_base":    "",
	"development.vite.host":          "the dev launcher bridges Vite and Wails addresses before Go starts",
	"development.vite.port":          "the dev launcher bridges Vite and Wails addresses before Go starts",
	"development.wails.host":         "the dev launcher bridges Vite and Wails addresses before Go starts",
	"development.wails.port":         "the dev launcher bridges Vite and Wails addresses before Go starts",
	"development.pprof.enabled":      "pprof mounts on the loopback HTTP server before it starts (ADR 0023)",
	"development.debug.pause_ingest": "the debug pauses are read when the store opens",
	"development.debug.pause_commit": "the debug pauses are read when the store opens",
}

// pathSnapshotReason explains the bootstrap directory entries, which are not
// settings.yaml fields: every location this process uses was derived from one
// immutable snapshot before the app was constructed (ADR 0014).
const pathSnapshotReason = "every location is derived from one path snapshot, resolved before the app is built"

// RestartPendingField is one persisted value the running process is not using
// and cannot adopt without a relaunch.
type RestartPendingField struct {
	// Field is the dotted settings path, or bootstrap.data_dir /
	// bootstrap.config_dir for a directory override.
	Field     string
	Reason    string
	Running   string
	Persisted string
}

// SettingsReload reports what re-reading settings.yaml changed.
type SettingsReload struct {
	// Changed names every field whose value differs from the snapshot the app
	// was serving, whether or not this process could adopt it.
	Changed []string
	// RestartPending is everything persisted that this process is not running,
	// which is a superset of Changed's startup-only fields: a value changed
	// earlier in this session stays pending until the relaunch happens.
	RestartPending []RestartPendingField
}

// SettingsStatus is settings.yaml's standing state: where it lives, whether
// the last (re)load of the file on disk parsed and validated, and what a
// relaunch would change. Reading it causes no reload.
type SettingsStatus struct {
	Path  string
	Valid bool
	// Error is why the served snapshot is older than the file on disk —
	// Store.LoadError — empty when they agree.
	Error          string
	RestartPending []RestartPendingField
}

// SettingsStatus reports settings.yaml's standing state without reloading it.
func (a *App) SettingsStatus(ctx context.Context) SettingsStatus {
	status := SettingsStatus{
		Path:           a.settingsStore.Path(),
		Valid:          true,
		RestartPending: a.RestartPending(ctx),
	}
	if err := a.settingsStore.LoadError(); err != nil {
		status.Valid = false
		status.Error = err.Error()
	}
	return status
}

// ReloadSettings re-reads settings.yaml, applies what this process can adopt,
// and announces the change. It is the one reload path: the watcher, the HTTP
// endpoint and any future caller all go through it.
//
// Failed reload semantics are defined by ADR 0041.
func (a *App) ReloadSettings(ctx context.Context) (SettingsReload, error) {
	previous := a.settingsStore.Current()
	next, err := a.settingsStore.Reload()
	if err != nil {
		a.activityStore.Record(ctx, activity.ConfigReloadFailed(settingsFile, err))
		a.logger.Warn().Err(err).Msg("settings.yaml reload failed; keeping the running values")
		return SettingsReload{}, Wrap(err, KindInvalid, "reloading %s", settingsFile)
	}

	changes := settings.Diff(previous, next)
	if len(changes) == 0 {
		return SettingsReload{RestartPending: a.RestartPending(ctx)}, nil
	}

	changed := make([]string, 0, len(changes))
	for _, change := range changes {
		changed = append(changed, change.Field)
		//nolint:contextcheck // listener reconcile uses the app lifetime so HTTP reload handlers can return before draining.
		a.applySettingsField(change.Field, next)
	}

	a.Events.Publish(ctx, events.SettingsUpdated{Changed: changed})
	a.activityStore.Record(ctx, activity.ConfigReloaded(settingsFile, strings.Join(changed, ", ")))
	return SettingsReload{Changed: changed, RestartPending: a.RestartPending(ctx)}, nil
}

// applySettingsField pushes one changed field into the subsystem holding a copy
// of it. Everything absent from this switch either re-reads the store on every
// use, is re-read by the frontend on settings:updated, or is startup-only —
// which is what settingsReload records.
func (a *App) applySettingsField(field string, next settings.Settings) {
	switch field {
	case "polling.interval":
		a.Settings.applyPolling(next.Polling.Interval.Duration())
	case "paths.tmux":
		a.tmux.SetOverride(next.Paths.Tmux)
	case "development.github.api_base":
		a.github.SetAPIBase(next.GitHubAPIBase())
		if a.fetchers != nil {
			a.fetchers.InvalidateAll()
		}
	case "skills.auto_update":
		if next.Skills.AutoUpdate && a.mock == "" {
			go a.syncInstalledSkills()
		}
	case "http.enabled", "http.host", "http.port":
		a.applyHTTPSettings(next)
	}
}

// RestartPending reports every persisted value that differs from what this
// process mounted and cannot be adopted while it runs. It is the one answer
// behind every "restart needed" hint: the terminal opt-in, the HTTP listener,
// and the data/config directory overrides.
func (a *App) RestartPending(context.Context) []RestartPendingField {
	pending := make([]RestartPendingField, 0, 4)
	for _, change := range settings.Diff(a.mountedSettings(), a.settingsStore.Current()) {
		reason := settingsReload[change.Field]
		if reason == "" {
			continue
		}
		pending = append(pending, RestartPendingField{
			Field:     change.Field,
			Reason:    reason,
			Running:   change.From,
			Persisted: change.To,
		})
	}
	return append(pending, a.pendingPathOverrides()...)
}

// pendingPathOverrides compares the bootstrap pointer file against the path
// snapshot this process resolved from it. A directory chosen in Settings is
// written there and takes effect on the next launch.
func (a *App) pendingPathOverrides() []RestartPendingField {
	bootstrap, err := settings.LoadBootstrap()
	if err != nil {
		a.logger.Warn().Err(err).Msg("bootstrap pointer unreadable; directory overrides not compared")
		return nil
	}
	resolved := settings.ResolvePaths(bootstrap, a.mock)

	var pending []RestartPendingField
	for _, dir := range []struct {
		field             string
		running, resolved string
	}{
		{"bootstrap.data_dir", a.paths.DataDir, resolved.DataDir},
		{"bootstrap.config_dir", a.paths.ConfigDir, resolved.ConfigDir},
	} {
		if dir.running == dir.resolved {
			continue
		}
		pending = append(pending, RestartPendingField{
			Field:     dir.field,
			Reason:    pathSnapshotReason,
			Running:   dir.running,
			Persisted: dir.resolved,
		})
	}
	return pending
}

// mountedSettings is what this process is actually running with for every
// startup-only field — the snapshot taken before the subsystems were built.
func (a *App) mountedSettings() settings.Settings {
	a.mountedMu.Lock()
	defer a.mountedMu.Unlock()
	return a.mounted
}
