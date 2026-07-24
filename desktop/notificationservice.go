package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/desktop"
	"github.com/hay-kot/hive-desktop/internal/desktop/notify"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline"
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

func (n flowNotifier) Notify(_ context.Context, in pipeline.SystemNotification) error {
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

// settingsNotificationGate resolves the app-level notification policy from
// settings.yaml on every delivery, so toggling notifications off in Settings
// silences flow notify nodes immediately rather than at the next restart. An
// unreadable settings file fails closed: never surface banners the user may
// have switched off.
type settingsNotificationGate struct{ logger zerolog.Logger }

func (g settingsNotificationGate) NotificationPolicy() pipeline.NotificationPolicy {
	settings, err := desktop.LoadSettings()
	if err != nil {
		g.logger.Warn().Err(err).Msg("notification settings unreadable; suppressing flow notifications")
		return pipeline.NotificationPolicy{}
	}
	return pipeline.NotificationPolicy{
		Allowed: settings.NotificationsEnabledOrDefault() && settings.SystemNotificationsEnabledOrDefault(),
		Sound:   settings.NotificationSoundOrDefault(),
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
