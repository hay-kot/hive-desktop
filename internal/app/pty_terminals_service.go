package app

import (
	"context"
	"errors"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// terminalDirectory answers the checkout a slug's shell opens in. It belongs to
// the session domain rather than to this one, the same seam StartTmuxSession
// crosses for the tmux backend.
type terminalDirectory interface {
	SessionDirectory(ctx context.Context, slug string) (string, error)
}

// PtyTerminalsService is the process-managed counterpart to TerminalsService:
// the same slug-keyed operations against sessions this process owns outright
// rather than against tmux (ADR 0045). It holds no token, base URL or stream
// path either — what it is reached over is the adapter's (ADR 0036).
type PtyTerminalsService struct {
	manager   *ptyterm.Manager
	metrics   ptyMetricsSink
	directory terminalDirectory
}

// ptyMetricsSink is the latency reporting the transport does. It is the one
// telemetry hook the comparison needs, so it is that rather than tmuxcc's
// fuller sink — there is no pause, resume or buffer depth to report here.
type ptyMetricsSink interface {
	FrameLatency(session, window string, d time.Duration)
}

type nopPtyMetrics struct{}

func (nopPtyMetrics) FrameLatency(string, string, time.Duration) {}

func newPtyTerminalsService(manager *ptyterm.Manager, directory terminalDirectory) *PtyTerminalsService {
	return &PtyTerminalsService{manager: manager, metrics: nopPtyMetrics{}, directory: directory}
}

// Available reports build and platform support. There is no external program to
// find, so unlike tmux this cannot become available while the app is running.
func (s *PtyTerminalsService) Available(ctx context.Context) error {
	return ptyError(s.manager.Available(ctx), "process-managed terminals need macOS or Linux and a desktop build.")
}

// Attach returns the windows of a live session. It never spawns: a slug with no
// session is a KindNotFound the caller answers with Start, which keeps the two
// backends' contracts identical even though starting a shell costs far less
// here than starting a tmux session with an agent in it (ADR 0044).
func (s *PtyTerminalsService) Attach(ctx context.Context, slug string, cols, rows int) ([]ptyterm.Window, error) {
	windows, err := s.manager.Attach(ctx, slug, cols, rows)
	if err != nil {
		return nil, ptyError(err, "attaching to session %q", slug)
	}
	return windows, nil
}

// Start opens a session with one shell in the hive session's checkout, and
// reports whether it had to.
func (s *PtyTerminalsService) Start(ctx context.Context, slug string) (bool, error) {
	if s.manager.HasSession(slug) {
		return false, nil
	}
	dir, err := s.directory.SessionDirectory(ctx, slug)
	if err != nil {
		return false, err
	}
	started, err := s.manager.Start(ctx, slug, dir)
	if err != nil {
		return false, ptyError(err, "starting session %q", slug)
	}
	return started, nil
}

// Kill ends the session and every process in it, and reports whether there was
// one to kill. The hive session, its checkout and its record are untouched.
func (s *PtyTerminalsService) Kill(_ context.Context, slug string) (bool, error) {
	killed, err := s.manager.Kill(slug)
	if err != nil {
		return false, ptyError(err, "killing the terminal for session %q", slug)
	}
	return killed, nil
}

// ListWindows answers a slug's windows; one with no session answers with none
// rather than an error.
func (s *PtyTerminalsService) ListWindows(_ context.Context, slug string) ([]ptyterm.Window, error) {
	return s.manager.ListWindows(slug), nil
}

// Subscribe returns slug's event stream and its unsubscribe func. There is one
// active subscriber per session; a second call closes the first channel.
func (s *PtyTerminalsService) Subscribe(_ context.Context, slug string) (<-chan ptyterm.Event, func(), error) {
	events, unsubscribe, err := s.manager.Subscribe(slug)
	if err != nil {
		return nil, nil, ptyError(err, "subscribing to session %q", slug)
	}
	return events, unsubscribe, nil
}

func (s *PtyTerminalsService) Write(_ context.Context, slug, windowID string, p []byte) error {
	return ptyError(s.manager.Write(slug, windowID, p), "writing to window %q", windowID)
}

// Resize sets the session's size outright. Nothing else is attached to these
// PTYs, so unlike the tmux backend this is not a vote and there is no granted
// size to compare it against.
func (s *PtyTerminalsService) Resize(_ context.Context, slug string, cols, rows int) error {
	return ptyError(s.manager.Resize(slug, cols, rows), "resizing session %q", slug)
}

func (s *PtyTerminalsService) SelectWindow(_ context.Context, slug, windowID string) error {
	return ptyError(s.manager.SelectWindow(slug, windowID), "selecting window %q", windowID)
}

func (s *PtyTerminalsService) NewWindow(_ context.Context, slug string) (string, error) {
	id, err := s.manager.NewWindow(slug)
	if err != nil {
		return "", ptyError(err, "creating a window in session %q", slug)
	}
	return id, nil
}

func (s *PtyTerminalsService) CloseWindow(_ context.Context, slug, windowID string) error {
	return ptyError(s.manager.CloseWindow(slug, windowID), "closing window %q", windowID)
}

func (s *PtyTerminalsService) RenameWindow(_ context.Context, slug, windowID, name string) error {
	return ptyError(s.manager.RenameWindow(slug, windowID, name), "renaming window %q", windowID)
}

// Detach releases nothing: the session is this process's own and outlives every
// transport that streams it, which is what makes switching between sessions
// free. It exists so both backends answer the same vocabulary.
func (s *PtyTerminalsService) Detach(_ context.Context, slug string) error {
	return ptyError(s.manager.Detach(slug), "detaching from session %q", slug)
}

// ObserveFrameLatency records how long an output frame took from PTY read to
// the moment a transport put it on the wire — the measurement the tmux backend
// reports through the same call, so the two are comparable.
func (s *PtyTerminalsService) ObserveFrameLatency(slug, windowID string, latency time.Duration) {
	s.metrics.FrameLatency(slug, windowID, latency)
}

// ptyError classifies a ptyterm failure by sentinel rather than by message.
func ptyError(err error, format string, args ...any) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ptyterm.ErrUnavailable):
		return Wrap(err, KindUnavailable, format, args...)
	case errors.Is(err, ptyterm.ErrInvalidSize), errors.Is(err, ptyterm.ErrInvalidName):
		return Wrap(err, KindInvalid, format, args...)
	case errors.Is(err, ptyterm.ErrNotAttached), errors.Is(err, ptyterm.ErrUnknownWindow):
		return Wrap(err, KindNotFound, format, args...)
	default:
		return Wrap(err, KindInternal, format, args...)
	}
}
