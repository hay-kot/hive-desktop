package wailsui

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app/events"

	"github.com/wailsapp/wails/v3/pkg/application"
	wailsnotify "github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Every event this adapter emits is registered and emitted from this file.
// They are wake-up signals: the frontend re-reads the relevant service on
// receipt, and only log:appended, notification:activated, notification:toast,
// update:available and update:none carry a payload that matters.

// Package-variable initialization instead of init(): this repo enables
// gochecknoinits.
var _ = registerEvents()

func registerEvents() struct{} {
	// connection:updated carries the provider whose credentials changed;
	// log:appended carries the pipeline event log's new tail offset after a
	// producer tick appends at least one row; flows:updated fires after a
	// flows/*.yaml directory reload (an external edit, or the app's own
	// SaveFlow/SaveLayout — see openFlows); actions:updated fires after
	// an actions.yml reload. All are wake-up signals: the frontend re-reads
	// the relevant service on receipt.
	application.RegisterEvent[string]("connection:updated")
	application.RegisterEvent[int64]("log:appended")
	// inbox:updated fires after the flow engine commits at least one run. It
	// is the signal a feed re-read keys off: log:appended only says a source
	// observed something, which may route nowhere at all.
	application.RegisterEvent[string]("inbox:updated")
	application.RegisterEvent[string]("flows:updated")
	application.RegisterEvent[string]("actions:updated")
	application.RegisterEvent[string]("jobs:updated")
	// settings:updated fires after settings.yaml is re-read and found to differ
	// from what the process was serving — a hand edit, a dotfiles sync, another
	// machine's config. Not for the app's own writes: those are already in the
	// snapshot by the time the watcher ticks, so the reload diffs to nothing.
	// Like the others it is a wake-up: every composable holding a value out of
	// settings.yaml re-hydrates.
	application.RegisterEvent[string]("settings:updated")
	// window:focus and window:blur carry the current focus state. Consumers use
	// them to update focus-sensitive UI without querying the native window.
	application.RegisterEvent[bool]("window:focus")
	application.RegisterEvent[bool]("window:blur")
	// activity:appended carries the new event's id after any subsystem (or the
	// frontend, via ActivityService.Record) appends to the activity log. The
	// Activity view re-reads its latest page and advances its unseen marker.
	application.RegisterEvent[int64]("activity:appended")
	// update:available carries the latest UpdateInfo when a self-update check
	// finds a newer desktop release; update:none fires when the check confirms
	// the app is current. The title bar reacts to update:available.
	application.RegisterEvent[UpdateInfo]("update:available")
	application.RegisterEvent[UpdateInfo]("update:none")
	// notification:activated carries the workspace and inbox item behind a
	// native notification the user clicked. Unlike the wake-up signals above
	// its payload is the whole message: the window is already being raised by
	// the time it fires, and the frontend's job is only to route to that item.
	application.RegisterEvent[NotificationActivation]("notification:activated")
	// notification:toast carries a flow notification the user chose to receive
	// inside Hive rather than as an OS banner (Settings -> Notifications ->
	// Delivery). The frontend surfaces it through the same toast stack every
	// other in-app notification uses.
	application.RegisterEvent[NotificationToast]("notification:toast")
	return struct{}{}
}

// Subscribe wires every core event to its Wails wake-up signal and returns a
// cancel that tears every subscription down.
//
// Every subscription uses events.Coalesce, with one exception:
// notification.raised uses events.Buffer, because a toast is not state the
// frontend re-reads on wake-up — it is the message itself, so coalescing it
// would mean a notification arriving right behind a busier one is simply
// never seen. Every other subscriber only needs the latest state, and a busy
// webview must never hold up the producer goroutine that published.
//
// This is where the core's typed payload is deliberately degraded. Wails
// events are wake-up signals by design — an adapter that needs the delta gets
// it from the bus instead.
// SubscribeHooks are the adapter-owned reactions that ride along with the
// wake-up emissions: state this side of the app holds and the frontend cannot
// re-read for it. Each may be nil.
type SubscribeHooks struct {
	// FlowsUpdated rebuilds the tray's profile listing.
	FlowsUpdated func()
	// SettingsUpdated adopts a reloaded settings.yaml into adapter-owned state
	// (the update ticker). It receives the changed fields so it can ignore a
	// section it does not own.
	SettingsUpdated func(changed []string)
}

func Subscribe(ctx context.Context, bus *events.Bus, hooks SubscribeHooks) (cancel func()) {
	cancels := []func(){
		events.Subscribe(ctx, bus, "wailsui.log", events.Coalesce(), func(_ context.Context, e events.LogAppended) {
			emitLogAppended(e.NextOffset)
		}),
		events.Subscribe(ctx, bus, "wailsui.inbox", events.Coalesce(), func(context.Context, events.InboxUpdated) {
			emitInboxUpdated()
		}),
		events.Subscribe(ctx, bus, "wailsui.activity", events.Coalesce(), func(_ context.Context, e events.ActivityAppended) {
			emitActivityAppended(e.ID)
		}),
		events.Subscribe(ctx, bus, "wailsui.jobs", events.Coalesce(), func(context.Context, events.JobsUpdated) {
			// The core carries the job id; the frontend re-reads the job list,
			// so this is the degradation the wake-up contract asks for.
			emitJobsUpdated()
		}),
		events.Subscribe(ctx, bus, "wailsui.actions", events.Coalesce(), func(context.Context, events.ActionsUpdated) {
			emitActionsUpdated()
		}),
		events.Subscribe(ctx, bus, "wailsui.connection", events.Coalesce(), func(_ context.Context, e events.ConnectionUpdated) {
			emitConnectionUpdated(e.Provider)
		}),
		events.Subscribe(ctx, bus, "wailsui.flows", events.Coalesce(), func(context.Context, events.FlowsUpdated) {
			emitFlowsUpdated()
			if hooks.FlowsUpdated != nil {
				hooks.FlowsUpdated()
			}
		}),
		events.Subscribe(ctx, bus, "wailsui.settings", events.Coalesce(), func(_ context.Context, e events.SettingsUpdated) {
			// Adopt before waking the frontend: the System screen re-reads the
			// updater's state on settings:updated, and it must not read it back
			// before this side has applied the reload to it.
			if hooks.SettingsUpdated != nil {
				hooks.SettingsUpdated(e.Changed)
			}
			emitSettingsUpdated()
		}),
		// A notify terminal's delivery is not state to re-read: it is the
		// message, so every one gets a slot in the queue rather than risking
		// a coalesced drop. InApp is the only one this adapter acts on here —
		// an OS banner already went out through the notifier port itself by
		// the time this publishes (see app.go's observedNotifier).
		events.Subscribe(ctx, bus, "wailsui.notification", events.Buffer(32), func(_ context.Context, e events.NotificationRaised) {
			if !e.InApp {
				return
			}
			emitNotificationToast(NotificationToast{
				Title:    e.Title,
				Body:     e.Body,
				Severity: e.Severity,
			})
		}),
	}
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// emitLogAppended pushes the pipeline event log's new tail offset to the
// frontend after a producer tick appends at least one row. Safe to call
// from the producer goroutine once the app is running.
func emitLogAppended(nextOffset int64) {
	if app := application.Get(); app != nil {
		app.Event.Emit("log:appended", nextOffset)
	}
}

// emitInboxUpdated wakes the feed views after the flow engine committed a run:
// membership claims, queued actions and node-run metrics may all have changed.
func emitInboxUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("inbox:updated", "changed")
	}
}

// emitActivityAppended pushes the activity:appended wake-up (carrying the new
// event's id) to the frontend after any subsystem records an activity event.
// Safe to call from any goroutine once the app is running.
func emitActivityAppended(id int64) {
	if app := application.Get(); app != nil {
		app.Event.Emit("activity:appended", id)
	}
}

// emitJobsUpdated wakes frontend consumers after any successful job lifecycle
// transition. The payload is intentionally only a wake-up signal.
func emitJobsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("jobs:updated", "changed")
	}
}

// emitNotificationActivated tells the frontend which item a clicked
// notification came from, so it can route to it.
func emitNotificationActivated(activation NotificationActivation) {
	if app := application.Get(); app != nil {
		app.Event.Emit("notification:activated", activation)
	}
}

// NotificationActivationFrom reads the item a clicked notification was sent
// about out of the payload the notify executor attached to it. The user info
// makes a native round trip, so its numbers come back in whatever shape the
// platform's serialization chose — hence the tolerant decode. App-level
// notifications carry no such payload and report false.
func NotificationActivationFrom(result wailsnotify.NotificationResult) (NotificationActivation, bool) {
	profileID, _ := result.Response.UserInfo["profileId"].(string)
	if profileID == "" {
		return NotificationActivation{}, false
	}
	activation := NotificationActivation{ProfileID: profileID}
	switch id := result.Response.UserInfo["itemId"].(type) {
	case float64:
		activation.ItemID = int64(id)
	case int64:
		activation.ItemID = id
	case int:
		activation.ItemID = int64(id)
	case json.Number:
		activation.ItemID, _ = id.Int64()
	case string:
		activation.ItemID, _ = strconv.ParseInt(id, 10, 64)
	}
	return activation, true
}

// emitWindowFocus pushes the current focused state to the frontend. Safe to
// call from native window event callbacks once the app is running.
func emitWindowFocus() {
	if app := application.Get(); app != nil {
		app.Event.Emit("window:focus", true)
	}
}

// emitWindowBlur pushes the current unfocused state to the frontend. Safe to
// call from native window event callbacks once the app is running.
func emitWindowBlur() {
	if app := application.Get(); app != nil {
		app.Event.Emit("window:blur", false)
	}
}

// emitConnectionUpdated pushes the connection:updated wake-up to the
// frontend, naming the provider whose credentials changed. Safe to call from
// any goroutine once the app is running.
func emitConnectionUpdated(provider string) {
	if app := application.Get(); app != nil {
		app.Event.Emit("connection:updated", provider)
	}
}

// emitFlowsUpdated pushes the flows:updated wake-up to the frontend. Safe to
// call from any goroutine once the app is running.
func emitFlowsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("flows:updated", "changed")
	}
}

// emitActionsUpdated wakes frontend consumers after a successful catalog
// change or a watcher reload. Service mutations call it only after success.
func emitActionsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("actions:updated", "changed")
	}
}

// emitSettingsUpdated wakes every frontend consumer holding a value read out of
// settings.yaml after a reload. The changed field list stays on this side: the
// frontend re-hydrates whole sections, so naming them would only invite a
// consumer to skip a re-read it should be doing.
func emitSettingsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("settings:updated", "changed")
	}
}

// emitNotificationToast hands a flow notification to the frontend to surface
// in-app. Called from the notification.raised subscription above once the
// core has published; before the app is running (or in a headless build) it
// is a no-op, which matches the native path's own behavior when
// notifications are unavailable.
//
// A var, not a func — same seam as emitUpdateAvailable/emitUpdateNone below —
// so a test can swap it for a spy without a running Wails application.
var emitNotificationToast = func(toast NotificationToast) {
	if app := application.Get(); app != nil {
		app.Event.Emit("notification:toast", toast)
	}
}

// emitUpdateAvailable pushes update:available, carrying the latest
// UpdateInfo, when a self-update check finds a newer desktop release; the
// title bar reacts to it. emitUpdateNone pushes update:none when a check
// confirms the app is current. Neither is reached through the core's event
// bus — update checking is adapter-owned end to end (see UpdaterService) —
// so both are called directly from there rather than from a Subscribe
// handler above; they live here only so every emission this adapter makes is
// registered and emitted from this one file.
var (
	emitUpdateAvailable = func(info UpdateInfo) {
		if app := application.Get(); app != nil {
			app.Event.Emit("update:available", info)
		}
	}
	emitUpdateNone = func(info UpdateInfo) {
		if app := application.Get(); app != nil {
			app.Event.Emit("update:none", info)
		}
	}
)
