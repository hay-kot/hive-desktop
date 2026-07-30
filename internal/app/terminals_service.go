package app

import (
	"context"
	"errors"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

// TerminalsService is the slug-keyed driving service both the HTTP and the
// Wails adapter call. It holds no token, base URL or stream path: what the
// terminal is reached over is the adapter's, not the core's (ADR 0036).
type TerminalsService struct {
	manager *tmuxcc.Manager
	metrics tmuxcc.MetricsSink
}

func newTerminalsService(manager *tmuxcc.Manager, metrics tmuxcc.MetricsSink) *TerminalsService {
	return &TerminalsService{manager: manager, metrics: metrics}
}

// Available reports tmux/build/platform availability only. Whether the
// loopback server that carries the transport is up is composed by the adapter
// that owns that transport.
func (s *TerminalsService) Available(ctx context.Context) error {
	return terminalError(s.manager.Available(ctx), "Terminal sessions need tmux 3.2 or newer. Hive searches PATH and the usual install prefixes; set terminal.tmux_path in settings.yaml if yours is elsewhere.")
}

// Attach opens, or returns the windows of, the control client for slug.
func (s *TerminalsService) Attach(ctx context.Context, slug string, cols, rows int) ([]tmuxcc.Window, error) {
	windows, err := s.manager.Attach(ctx, slug, cols, rows)
	if err != nil {
		return nil, terminalError(err, "attaching to session %q", slug)
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

// Resize renegotiates the control client's size. tmux gives every attached
// client of a window the same size and the smallest one wins.
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
