package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/desktop/notify"
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
	Notify(notify.Input) error
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

// Notify sends a native notification.
func (s *NotificationService) Notify(in NotifyInput) error {
	return s.notifier.Notify(notify.Input{
		Title:    in.Title,
		Subtitle: in.Subtitle,
		Body:     in.Body,
		Severity: in.Severity,
		Sound:    in.Sound,
		Data:     in.Data,
	})
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

// flowNotifier adapts the native notifier to the pipeline's SystemNotifier,
// so a flow's notify node delivers through exactly the same native path (and
// icon, and permission handling) as an app-level notification.
type flowNotifier struct{ notifier notificationNotifier }

func (n flowNotifier) Notify(_ context.Context, in ingest.SystemNotification) error {
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
	return n.notifier.Notify(notify.Input{
		Title:    in.Title,
		Body:     in.Body,
		Severity: in.Severity,
		Sound:    in.Sound,
		Data:     in.Data,
	})
}

// NotificationToast is a flow notification the user chose to receive inside
// Hive. It is the payload of the notification:toast event; unlike a banner it
// carries no click target, because the app is already in front of them.
type NotificationToast struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
}

// settingsNotificationGate resolves the app-level notification policy from
// settings.yaml on every delivery, so toggling notifications off in Settings
// silences flow notify nodes immediately rather than at the next restart. An
// unreadable settings file fails closed: never surface notifications the user
// may have switched off.
//
// It also resolves *where* a notification surfaces, which is why it holds the
// window's focus state: the automatic delivery mode means "a banner only when
// I'm looking elsewhere", and only this side of the app knows both halves.
type settingsNotificationGate struct {
	focus  *focusState
	logger zerolog.Logger
}

func (g settingsNotificationGate) NotificationPolicy() ingest.NotificationPolicy {
	settings, err := settings.LoadSettings()
	if err != nil {
		g.logger.Warn().Err(err).Msg("notification settings unreadable; suppressing flow notifications")
		return ingest.NotificationPolicy{}
	}
	return ingest.NotificationPolicy{
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
func (g settingsNotificationGate) inApp(delivery string) bool {
	switch delivery {
	case settings.DeliveryApp:
		return true
	case settings.DeliverySystem:
		return false
	default:
		return g.focus != nil && g.focus.get()
	}
}

type unavailableNotifier struct {
	err error
}

func (n unavailableNotifier) Notify(notify.Input) error {
	return fmt.Errorf("native notifications unavailable: %w", n.err)
}

func (n unavailableNotifier) PermissionStatus() (string, error) {
	return "", fmt.Errorf("native notifications unavailable: %w", n.err)
}

func (n unavailableNotifier) RequestPermission() (bool, error) {
	return false, fmt.Errorf("native notifications unavailable: %w", n.err)
}
