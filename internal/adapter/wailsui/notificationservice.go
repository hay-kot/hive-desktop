package wailsui

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// NotifyInput is the frontend-facing request for a native notification.
type NotifyInput struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	Body     string         `json:"body"`
	Severity string         `json:"severity"`
	Sound    bool           `json:"sound"`
	Data     map[string]any `json:"data"`
}

// notificationNotifier is the small notification API exposed to the binding.
type notificationNotifier interface {
	Notify(Input) error
	PermissionStatus() (string, error)
	RequestPermission() (bool, error)
}

// NotificationService exposes native notification delivery and authorization to
// the frontend. Window activation remains owned by main.go.
type NotificationService struct {
	notifier notificationNotifier
}

// NewNotificationService constructs the binding over a ready notifier.
func NewNotificationService(notifier notificationNotifier) *NotificationService {
	if notifier == nil {
		return NewUnavailableNotificationService(errors.New("notification service is unavailable"))
	}
	return &NotificationService{notifier: notifier}
}

// NewUnavailableNotificationService constructs a binding that reports a useful
// setup failure instead of panicking when native notifications are unavailable.
func NewUnavailableNotificationService(err error) *NotificationService {
	if err == nil {
		err = errors.New("notification service is unavailable")
	}
	return &NotificationService{notifier: unavailableNotifier{err: err}}
}

// Notify sends a native notification. NotifyInput and Input are the same
// fields either side of the wire boundary -- the former carries the JSON tags
// the binding needs, the latter does not -- so the hand-copy this used to do
// is a conversion now that both live in this package.
func (s *NotificationService) Notify(in NotifyInput) error {
	return s.notifier.Notify(Input(in))
}

// PermissionStatus reports granted, denied, or not-requested.
func (s *NotificationService) PermissionStatus() (string, error) {
	return s.notifier.PermissionStatus()
}

// RequestNotificationPermission asks the operating system for notification
// authorization and returns whether it was granted.
func (s *NotificationService) RequestNotificationPermission() (bool, error) {
	return s.notifier.RequestPermission()
}

// NotificationActivation is the notification:activated payload: which
// workspace and inbox item a clicked notification came from. ItemID is 0 when
// the notification had no item behind it (an app-level notification, or one
// whose item could not be resolved), which the frontend reads as "just raise
// the window".
type NotificationActivation struct {
	ProfileID string `json:"profileId"`
	ItemID    int64  `json:"itemId"`
}

// FlowNotifier adapts the native notifier to dispatch's SystemNotifier, so a
// flow's notify node delivers through exactly the same native path (and icon,
// and permission handling) as an app-level notification.
type FlowNotifier struct{ notifier notificationNotifier }

// NewFlowNotifier wraps a notification service's notifier as the driven port
// dispatch declares.
func NewFlowNotifier(s *NotificationService) FlowNotifier {
	return FlowNotifier{notifier: s.notifier}
}

func (n FlowNotifier) Notify(_ context.Context, in dispatch.SystemNotification) error {
	// The user asked for this one inside Hive, not as a banner. The frontend
	// owns in-app presentation (see useToasts), so this hands the rendered
	// notification over and is done — there is no native call to make, and no
	// notification permission to need.
	if in.InApp {
		emitNotificationToast(NotificationToast{
			Title:    in.Title,
			Body:     in.Body,
			Severity: in.Severity,
		})
		return nil
	}
	if n.notifier == nil {
		return errors.New("native notifications unavailable")
	}
	return n.notifier.Notify(Input{
		Title:    in.Title,
		Body:     in.Body,
		Severity: in.Severity,
		Sound:    in.Sound,
		Data:     in.Data,
	})
}

// emitNotificationToast hands a flow notification to the frontend to surface
// in-app. Safe to call from the output worker's goroutine once the app is
// running; before that (or in a headless build) it is a no-op, which matches
// the native path's own behavior when notifications are unavailable.
func emitNotificationToast(toast NotificationToast) {
	if app := application.Get(); app != nil {
		app.Event.Emit("notification:toast", toast)
	}
}

// NotificationToast is a flow notification the user chose to receive inside
// Hive. It is the payload of the notification:toast event; unlike a banner it
// carries no click target, because the app is already in front of them.
type NotificationToast struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
}

// NotificationGate resolves the app-level notification policy from
// settings.yaml on every delivery, so toggling notifications off in Settings
// silences flow notify nodes immediately rather than at the next restart. An
// unreadable settings file fails closed: never surface notifications the user
// may have switched off.
//
// It also resolves *where* a notification surfaces, which is why it holds the
// window's focus state: the automatic delivery mode means "a banner only when
// I'm looking elsewhere", and only this side of the app knows both halves.
type NotificationGate struct {
	focus  *FocusState
	logger zerolog.Logger
}

// NewNotificationGate builds the gate over the window's focus state.
func NewNotificationGate(focus *FocusState, logger zerolog.Logger) NotificationGate {
	return NotificationGate{focus: focus, logger: logger}
}

func (g NotificationGate) NotificationPolicy() dispatch.NotificationPolicy {
	settings, err := settings.LoadSettings()
	if err != nil {
		g.logger.Warn().Err(err).Msg("notification settings unreadable; suppressing flow notifications")
		return dispatch.NotificationPolicy{}
	}
	return dispatch.NotificationPolicy{
		Allowed: settings.NotificationsEnabledOrDefault(),
		Sound:   settings.NotificationSoundOrDefault(),
		InApp:   g.inApp(settings.NotificationDeliveryOrDefault()),
	}
}

// inApp maps the delivery preference onto this moment: "app" always stays
// inside Hive, "system" always leaves it, and "auto" — the default — keeps a
// notification in-app only while the user is already looking at the window.
// An unknown focus state (no window yet) counts as unfocused, so a
// notification raised during startup still reaches the user.
func (g NotificationGate) inApp(delivery string) bool {
	switch delivery {
	case settings.DeliveryApp:
		return true
	case settings.DeliverySystem:
		return false
	default:
		return g.focus != nil && g.focus.Get()
	}
}

type unavailableNotifier struct {
	err error
}

func (n unavailableNotifier) Notify(Input) error {
	return fmt.Errorf("native notifications unavailable: %w", n.err)
}

func (n unavailableNotifier) PermissionStatus() (string, error) {
	return "", fmt.Errorf("native notifications unavailable: %w", n.err)
}

func (n unavailableNotifier) RequestPermission() (bool, error) {
	return false, fmt.Errorf("native notifications unavailable: %w", n.err)
}
