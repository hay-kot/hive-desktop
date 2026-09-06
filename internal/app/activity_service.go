package app

import (
	"context"
	"log/slog"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/events"
)

const (
	activityDefaultListLimit = 200
	activityMaxListLimit     = 1000
)

// ActivityService owns the user-facing audit log: the frontend's read/write
// RPC surface, and the fire-and-forget activity.Recorder every backend
// subsystem that reports to the Activity view holds. Title-required,
// category/severity defaulting and validity live here rather than on
// ActivityEventStore, because they need the activity package's enum, which a
// store in data/ must not import.
type ActivityService struct {
	store  *stores.ActivityEventStore
	events *events.Bus
	log    *slog.Logger
}

func newActivityService(store *stores.ActivityEventStore, bus *events.Bus) *ActivityService {
	return &ActivityService{store: store, events: bus, log: slog.Default()}
}

// List returns up to limit events with id < before, newest first. Pass
// before <= 0 to start from the most recent event.
func (s *ActivityService) List(ctx context.Context, before int64, limit int) ([]activity.Event, error) {
	if limit <= 0 || limit > activityMaxListLimit {
		limit = activityDefaultListLimit
	}
	rows, err := s.store.List(ctx, before, limit)
	if err != nil {
		return nil, Wrap(err, KindInternal, "listing activity events")
	}
	out := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, activityEventFromStore(row))
	}
	return out, nil
}

// Append validates, persists, and returns the stored event (with its
// assigned id and timestamp), publishing events.ActivityAppended on success.
// It is the error-returning path used by the frontend RPC; backend sites use
// Record.
func (s *ActivityService) Append(ctx context.Context, e activity.Event) (activity.Event, error) {
	if e.Title == "" {
		return activity.Event{}, Errorf(KindInvalid, "activity event requires a title")
	}
	if e.Category == "" {
		e.Category = activity.CategorySystem
	}
	if e.Severity == "" {
		e.Severity = activity.SeverityInfo
	}
	if !e.Category.IsValid() {
		return activity.Event{}, Errorf(KindInvalid, "invalid activity category %q", e.Category)
	}
	if !e.Severity.IsValid() {
		return activity.Event{}, Errorf(KindInvalid, "invalid activity severity %q", e.Severity)
	}

	stored, err := s.store.Append(ctx, stores.ActivityEventCreate{
		Category: e.Category.String(),
		Severity: e.Severity.String(),
		Title:    e.Title,
		Body:     e.Body,
		Source:   e.Source,
		Metadata: e.Metadata,
	})
	if err != nil {
		return activity.Event{}, Wrap(err, KindInternal, "recording an activity event")
	}
	s.events.Publish(ctx, events.ActivityAppended{ID: stored.ID})
	return activityEventFromStore(stored), nil
}

// Record implements activity.Recorder: an emit site should never fail or
// block because the audit log couldn't be written, so a persistence failure
// is logged and swallowed rather than returned.
func (s *ActivityService) Record(ctx context.Context, e activity.Event) {
	if _, err := s.Append(ctx, e); err != nil {
		s.log.Warn("recording activity event failed", "title", e.Title, "error", err)
	}
}

func activityEventFromStore(row stores.ActivityEvent) activity.Event {
	return activity.Event{
		ID:        row.ID,
		CreatedAt: row.CreatedAt,
		Category:  activity.Category(row.Category),
		Severity:  activity.Severity(row.Severity),
		Title:     row.Title,
		Body:      row.Body,
		Source:    row.Source,
		Metadata:  row.Metadata,
	}
}
