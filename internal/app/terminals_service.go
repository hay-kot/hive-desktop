package app

import (
	"context"
	"errors"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

// terminalStarter spawns the tmux session a slug names when tmux has none. It
// belongs to the session domain rather than to this one: the slug names a hive
// session, and what its terminal holds is hive's spawn configuration.
type terminalStarter interface {
	StartTmuxSession(ctx context.Context, slug string) error
}

// TerminalsService is the slug-keyed driving service both the HTTP and the
// Wails adapter call. It holds no token, base URL or stream path: what the
// terminal is reached over is the adapter's, not the core's (ADR 0036).
type TerminalsService struct {
	manager *tmuxcc.Manager
	metrics tmuxcc.MetricsSink
	starter terminalStarter
}

func newTerminalsService(manager *tmuxcc.Manager, metrics tmuxcc.MetricsSink, starter terminalStarter) *TerminalsService {
	return &TerminalsService{manager: manager, metrics: metrics, starter: starter}
}

// Available reports tmux/build/platform availability only. Whether the
// loopback server that carries the transport is up is composed by the adapter
// that owns that transport.
func (s *TerminalsService) Available(ctx context.Context) error {
	return terminalError(s.manager.Available(ctx), "Terminal sessions need tmux 3.2 or newer. Hive searches PATH and the usual install prefixes; set paths.tmux in settings.yaml if yours is elsewhere.")
}

// Attach opens, or returns the windows of, the control client for slug. cols
// and rows are the caller's opening size vote; 0x0 attaches without setting a
// client size at all, which leaves the session at the size its other clients
// gave it until the first Resize.
//
// A slug tmux is not running is a KindNotFound the caller is expected to answer
// with Start — attaching never spawns on its own, because spawning runs the
// session's agent command and that is the user's call to make (ADR 0044). That
// answer comes from a has-session probe rather than from a dead control
// stream's message, which is unclassifiable and reads as an internal fault.
func (s *TerminalsService) Attach(ctx context.Context, slug string, cols, rows int) ([]tmuxcc.Window, error) {
	exists, err := s.manager.HasSession(ctx, slug)
	if err != nil {
		return nil, terminalError(err, "attaching to session %q", slug)
	}
	if !exists {
		return nil, Errorf(KindNotFound, "session %q is not running", slug)
	}
	windows, err := s.manager.Attach(ctx, slug, cols, rows)
	if err != nil {
		return nil, terminalError(err, "attaching to session %q", slug)
	}
	return windows, nil
}

// Start spawns the tmux session slug names, from the hive session's own spawn
// configuration, and reports whether it had to. A session tmux is already
// running is left alone: the probe is what keeps a spawn configuration the
// desktop cannot drive — hive's command-based `spawn:` rather than `windows:` —
// from failing a start for a session that needs none.
func (s *TerminalsService) Start(ctx context.Context, slug string) (bool, error) {
	exists, err := s.manager.HasSession(ctx, slug)
	if err != nil {
		return false, terminalError(err, "starting session %q", slug)
	}
	if exists {
		return false, nil
	}
	if err := s.starter.StartTmuxSession(ctx, slug); err != nil {
		return false, err
	}
	return true, nil
}

// Kill kills the tmux session slug names and reports whether there was one to
// kill. It is the terminal's own lifecycle only: the hive session, its checkout
// and its record are untouched, which is what separates this from a delete or a
// recycle. Whatever was running inside — the agent included — stops with it.
func (s *TerminalsService) Kill(ctx context.Context, slug string) (bool, error) {
	killed, err := s.manager.KillSession(ctx, slug)
	if err != nil {
		return false, terminalError(err, "killing the terminal for session %q", slug)
	}
	return killed, nil
}

// ListWindows answers slug's window set without attaching: an attached slug
// answers from its live client, any other from a one-shot tmux query. A slug
// with no tmux session behind it answers with no windows rather than an error.
func (s *TerminalsService) ListWindows(ctx context.Context, slug string) ([]tmuxcc.Window, error) {
	windows, err := s.manager.ListWindows(ctx, slug)
	if err != nil {
		return nil, terminalError(err, "listing windows of session %q", slug)
	}
	return windows, nil
}

// Subscribe returns slug's event stream and its unsubscribe func. There is one
// active subscriber per session: a second call closes the first channel, which
// is how a replaced transport learns it was replaced.
func (s *TerminalsService) Subscribe(_ context.Context, slug string) (<-chan tmuxcc.Event, func(), error) {
	events, unsubscribe, err := s.manager.Subscribe(slug)
	if err != nil {
		return nil, nil, terminalError(err, "subscribing to session %q", slug)
	}
	return events, unsubscribe, nil
}

// Write sends bytes to a window's active pane.
func (s *TerminalsService) Write(ctx context.Context, slug, windowID string, p []byte) error {
	client, err := s.client(slug)
	if err != nil {
		return err
	}
	return terminalError(client.Write(ctx, windowID, p), "writing to window %q", windowID)
}

// Resize renegotiates the control client's size. Every client attached to a
// window renders the same grid, and tmux's window-size option decides whose
// size that is, so this is a vote rather than a resize.
func (s *TerminalsService) Resize(ctx context.Context, slug string, cols, rows int) error {
	client, err := s.client(slug)
	if err != nil {
		return err
	}
	return terminalError(client.Resize(ctx, cols, rows), "resizing session %q", slug)
}

func (s *TerminalsService) SelectWindow(ctx context.Context, slug, windowID string) error {
	client, err := s.client(slug)
	if err != nil {
		return err
	}
	return terminalError(client.SelectWindow(ctx, windowID), "selecting window %q", windowID)
}

// NewWindow creates a window and returns its id; the tab set itself follows
// from the notification tmux sends afterwards.
func (s *TerminalsService) NewWindow(ctx context.Context, slug string) (string, error) {
	client, err := s.client(slug)
	if err != nil {
		return "", err
	}
	id, err := client.NewWindow(ctx)
	if err != nil {
		return "", terminalError(err, "creating a window in session %q", slug)
	}
	return id, nil
}

func (s *TerminalsService) CloseWindow(ctx context.Context, slug, windowID string) error {
	client, err := s.client(slug)
	if err != nil {
		return err
	}
	return terminalError(client.CloseWindow(ctx, windowID), "closing window %q", windowID)
}

func (s *TerminalsService) RenameWindow(ctx context.Context, slug, windowID, name string) error {
	client, err := s.client(slug)
	if err != nil {
		return err
	}
	return terminalError(client.RenameWindow(ctx, windowID, name), "renaming window %q", windowID)
}

// Detach closes slug's control client, leaving the tmux session itself alone.
// An unknown slug is not an error: detaching what is already gone succeeded.
func (s *TerminalsService) Detach(ctx context.Context, slug string) error {
	return terminalError(s.manager.Detach(ctx, slug), "detaching from session %q", slug)
}

// ObserveFrameLatency records how long an output frame took from tmux decode to
// the moment a transport put it on the wire. Only the transport knows when the
// send happened, so it measures and reports it here.
func (s *TerminalsService) ObserveFrameLatency(slug, windowID string, latency time.Duration) {
	s.metrics.FrameLatency(slug, windowID, latency)
}

func (s *TerminalsService) client(slug string) (*tmuxcc.Client, error) {
	client, ok := s.manager.Client(slug)
	if !ok {
		return nil, Errorf(KindNotFound, "no terminal is attached for session %q", slug)
	}
	return client, nil
}

// terminalError classifies a tmuxcc failure by sentinel rather than by message.
// A residual *tmuxcc.CommandError — tmux refused a command we had already
// validated — is ours, not the caller's, so it falls through to internal.
func terminalError(err error, format string, args ...any) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, tmuxcc.ErrUnavailable):
		return Wrap(err, KindUnavailable, format, args...)
	case errors.Is(err, tmuxcc.ErrInvalidSize), errors.Is(err, tmuxcc.ErrInvalidName):
		return Wrap(err, KindInvalid, format, args...)
	case errors.Is(err, tmuxcc.ErrNotAttached), errors.Is(err, tmuxcc.ErrUnknownWindow):
		return Wrap(err, KindNotFound, format, args...)
	default:
		return Wrap(err, KindInternal, format, args...)
	}
}
