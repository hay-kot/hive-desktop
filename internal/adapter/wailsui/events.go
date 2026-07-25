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
// receipt, and only log:appended, notification:activated and
// notification:toast carry a payload that matters.

// Package-variable initialization instead of init(): this repo enables
// gochecknoinits.
var _ = registerEvents()

func registerEvents() struct{} {
	// auth:updated carries the new auth state string; log:appended carries the
	// pipeline event log's new tail offset after a producer tick appends at
	// least one row; flows:updated fires after a flows/*.yaml directory reload
	// (an external edit, or the app's own SaveFlow/SaveLayout — see
	// buildFlowsStore); actions:updated fires after an actions.yml reload.
	// All are wake-up signals: the frontend re-reads the relevant service on
	// receipt.
	application.RegisterEvent[string]("auth:updated")
	application.RegisterEvent[int64]("log:appended")
	application.RegisterEvent[string]("flows:updated")
	application.RegisterEvent[string]("actions:updated")
	application.RegisterEvent[string]("jobs:updated")
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
// Every subscription uses events.Coalesce: the frontend re-reads on receipt,
// so a dropped intermediate is not observable, and a busy webview must never
// hold up the producer goroutine that published.
//
// This is where the core's typed payload is deliberately degraded. Wails
// events are wake-up signals by design — an adapter that needs the delta gets
// it from the bus instead.
func Subscribe(ctx context.Context, bus *events.Bus, onFlowsUpdated func()) (cancel func()) {
	cancels := []func(){
		events.Subscribe(ctx, bus, "wailsui.log", events.Coalesce(), func(_ context.Context, e events.LogAppended) {
			EmitLogAppended(e.NextOffset)
		}),
		events.Subscribe(ctx, bus, "wailsui.activity", events.Coalesce(), func(_ context.Context, e events.ActivityAppended) {
			EmitActivityAppended(e.ID)
		}),
		events.Subscribe(ctx, bus, "wailsui.jobs", events.Coalesce(), func(context.Context, events.JobsUpdated) {
			// The core carries the job id; the frontend re-reads the job list,
			// so this is the degradation the wake-up contract asks for.
			EmitJobsUpdated()
		}),
		events.Subscribe(ctx, bus, "wailsui.actions", events.Coalesce(), func(context.Context, events.ActionsUpdated) {
			EmitActionsUpdated()
		}),
		events.Subscribe(ctx, bus, "wailsui.auth", events.Coalesce(), func(context.Context, events.AuthUpdated) {
			EmitAuthUpdated()
		}),
		events.Subscribe(ctx, bus, "wailsui.flows", events.Coalesce(), func(context.Context, events.FlowsUpdated) {
			EmitFlowsUpdated()
			if onFlowsUpdated != nil {
				onFlowsUpdated()
			}
		}),
	}
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// EmitLogAppended pushes the pipeline event log's new tail offset to the
// frontend after a producer tick appends at least one row. Safe to call
// from the producer goroutine once the app is running.
func EmitLogAppended(nextOffset int64) {
	if app := application.Get(); app != nil {
		app.Event.Emit("log:appended", nextOffset)
	}
}

// EmitActivityAppended pushes the activity:appended wake-up (carrying the new
// event's id) to the frontend after any subsystem records an activity event.
// Safe to call from any goroutine once the app is running.
func EmitActivityAppended(id int64) {
	if app := application.Get(); app != nil {
		app.Event.Emit("activity:appended", id)
	}
}

// EmitJobsUpdated wakes frontend consumers after any successful job lifecycle
// transition. The payload is intentionally only a wake-up signal.
func EmitJobsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("jobs:updated", "changed")
	}
}

// EmitNotificationActivated tells the frontend which item a clicked
// notification came from, so it can route to it.
func EmitNotificationActivated(activation NotificationActivation) {
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

// EmitWindowFocus pushes the current focused state to the frontend. Safe to
// call from native window event callbacks once the app is running.
func EmitWindowFocus() {
	if app := application.Get(); app != nil {
		app.Event.Emit("window:focus", true)
	}
}

// EmitWindowBlur pushes the current unfocused state to the frontend. Safe to
// call from native window event callbacks once the app is running.
func EmitWindowBlur() {
	if app := application.Get(); app != nil {
		app.Event.Emit("window:blur", false)
	}
}

// EmitAuthUpdated pushes the auth:updated wake-up to the frontend. Safe to
// call from any goroutine once the app is running.
func EmitAuthUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("auth:updated", "changed")
	}
}

// EmitFlowsUpdated pushes the flows:updated wake-up to the frontend. Safe to
// call from any goroutine once the app is running.
func EmitFlowsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("flows:updated", "changed")
	}
}

// EmitActionsUpdated wakes frontend consumers after a successful catalog
// change or a watcher reload. Service mutations call it only after success.
func EmitActionsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("actions:updated", "changed")
	}
}
