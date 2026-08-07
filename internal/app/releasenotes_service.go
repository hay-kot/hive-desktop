package app

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// How a pending set of release notes should be surfaced.
const (
	PresentationModal = "modal"
	PresentationToast = "toast"
)

// PendingNotes is the answer to "did this launch land on a newer version, and
// what should the user be told about it".
type PendingNotes struct {
	Show bool
	// Presentation is PresentationModal or PresentationToast; empty when
	// Show is false.
	Presentation string
	// Version is the running version the notes describe.
	Version string
	// Entries are the releases crossed to reach Version, newest first. It can
	// be empty on a genuine upgrade when no version in the range carries an
	// entry.
	Entries releasenotes.Entries
}

// ReleaseNotesService decides what the What's New surface shows and remembers
// what the user has already seen. The running version and channel are
// arguments rather than fields because the ldflags that carry the build
// identity bind to package main — the adapter supplies both, the same way
// SystemService receives them.
type ReleaseNotesService struct {
	entries releasenotes.Entries
	state   *releasenotes.State
}

// NewReleaseNotesService builds the service over the embedded changelog. It is
// exported for the same reason NewSettingsService is: it depends on nothing but
// a path and a logger, so the adapter can build one without standing up an App.
func NewReleaseNotesService(paths settings.Paths, logger zerolog.Logger) *ReleaseNotesService {
	entries, err := releasenotes.Load()
	if err != nil {
		// A malformed entry is a committed mistake that the package's own test
		// and the release gate both catch, so it cannot reach a published
		// build. If one somehow does, the app runs without release notes
		// rather than refusing to launch over them.
		logger.Warn().Err(err).Msg("release notes unavailable; changelog failed to parse")
	}
	return &ReleaseNotesService{entries: entries, state: releasenotes.NewState(paths.StateDir)}
}

// Pending reports what this launch should show. It is the launch-time half of
// the feature: an install ends by relaunching the app, so there is no
// in-process moment after an update to hook — the reliable trigger is finding,
// on the next start, that the running version outranks the last one the user
// acknowledged. That also covers updates this app did not perform, like a
// package manager or a manual reinstall.
//
// Recording on first run is a deliberate write from a read: a fresh install
// has no prior version to have upgraded *from*, so it silently adopts the
// running version and shows nothing.
func (s *ReleaseNotesService) Pending(_ context.Context, version, channel string) PendingNotes {
	if _, published := releasenotes.Channel(version); !published {
		return PendingNotes{}
	}

	acknowledged := s.state.Acknowledged()
	if acknowledged == "" {
		_ = s.state.Acknowledge(version)
		return PendingNotes{}
	}
	if !releasenotes.IsNewer(version, acknowledged) {
		return PendingNotes{}
	}

	entries := s.entries.Between(acknowledged, version, channel)
	// Dev builds are cut close to daily, so a modal on nearly every launch
	// would be hostile; they get the toast instead. An upgrade whose range
	// carries no entries also degrades to a toast, because a modal whose body
	// is empty says less than the one line "Updated to X" does.
	presentation := PresentationModal
	if channel == settings.ChannelDev || len(entries) == 0 {
		presentation = PresentationToast
	}
	return PendingNotes{
		Show:         true,
		Presentation: presentation,
		Version:      version,
		Entries:      entries,
	}
}

// Acknowledge records version as seen. The modal defers this to its dismissal,
// so a crash while it is open does not cost the user the notes; the toast has
// nothing to dismiss and acknowledges on sight.
//
// The context is deliberately discarded: the marker write must finish whether
// or not the caller is still listening. A frontend component unmounting mid
// launch cancels its call, and a cancelled write here would resurface the
// notes on every subsequent launch.
func (s *ReleaseNotesService) Acknowledge(_ context.Context, version string) error {
	return s.state.Acknowledge(version)
}

// History is every release a user on channel has received, newest first — the
// About pane's list, and where a dismissed surface can be read again.
func (s *ReleaseNotesService) History(_ context.Context, channel string) releasenotes.Entries {
	return s.entries.VisibleIn(channel)
}
