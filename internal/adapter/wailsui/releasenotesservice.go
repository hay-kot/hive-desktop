package wailsui

import (
	"context"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// ReleaseNote is the frontend-facing view of one published release's notes.
// Date is a plain YYYY-MM-DD string rather than a timestamp: a release is
// dated, not clocked, and formatting it here keeps the frontend from having to
// decide what an instant means in the user's timezone.
type ReleaseNote struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Channel string `json:"channel"`
	Summary string `json:"summary"`
	Body    string `json:"body"`
}

// PendingReleaseNotes tells the frontend whether this launch landed on a newer
// version and how to say so.
type PendingReleaseNotes struct {
	Show bool `json:"show"`
	// Presentation is "modal" or "toast"; empty when Show is false.
	Presentation string        `json:"presentation"`
	Version      string        `json:"version"`
	Entries      []ReleaseNote `json:"entries"`
}

// ReleaseNotesService exposes the embedded changelog: what to show once after
// an update, and the full per-channel history the About pane lists.
type ReleaseNotesService struct {
	core    *app.ReleaseNotesService
	version string
	channel string
}

// NewReleaseNotesService resolves the channel whose releases this build
// receives, the same way attachUpdater does — the build's own channel unless
// settings.yaml overrides it.
//
// A build with no published channel is a source build. Its history falls back
// to dev, which is the channel that receives everything, so a developer can
// still read and lay out the full changelog; Pending independently refuses to
// surface anything on such a build.
func NewReleaseNotesService(core *app.ReleaseNotesService, version string, resolveChannel func(defaultChannel string) string) *ReleaseNotesService {
	channel, ok := ReleaseChannel(version)
	if !ok {
		channel = settings.ChannelDev
	}
	return &ReleaseNotesService{core: core, version: version, channel: resolveChannel(channel)}
}

// Pending reports what this launch should show, if anything.
func (s *ReleaseNotesService) Pending(ctx context.Context) PendingReleaseNotes {
	pending := s.core.Pending(ctx, s.version, s.channel)
	return PendingReleaseNotes{
		Show:         pending.Show,
		Presentation: pending.Presentation,
		Version:      pending.Version,
		Entries:      releaseNotes(pending.Entries),
	}
}

// Acknowledge records the running version as seen, so the surface does not
// return on the next launch.
func (s *ReleaseNotesService) Acknowledge(ctx context.Context) error {
	return s.core.Acknowledge(ctx, s.version)
}

// History lists every release this build's channel receives, newest first.
func (s *ReleaseNotesService) History(ctx context.Context) []ReleaseNote {
	return releaseNotes(s.core.History(ctx, s.channel))
}

func releaseNotes(entries releasenotes.Entries) []ReleaseNote {
	out := make([]ReleaseNote, 0, len(entries))
	for _, entry := range entries {
		out = append(out, ReleaseNote{
			Version: entry.Version,
			Date:    entry.Date.Format(time.DateOnly),
			Channel: entry.Channel,
			Summary: entry.Summary,
			Body:    entry.Body,
		})
	}
	return out
}
