package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// terminalDirectory answers the checkout a hive session's slug names. It
// belongs to the session domain rather than to this one, the same seam
// StartTmuxSession crosses for the tmux backend.
type terminalDirectory interface {
	SessionDirectory(ctx context.Context, slug string) (string, error)
}

// OpenPopupTerminal is one launch. The directory is resolved in order —
// Launcher's own cwd, SessionSlug's checkout, then Dir, then the user's home —
// so a caller with a session in hand does not have to know where it lives, and
// one with neither still gets a shell somewhere sensible rather than wherever
// the app happened to be started from.
type OpenPopupTerminal struct {
	// Launcher is a configured launcher's id, and supplies the command line and
	// optionally the directory. The caller sends the id rather than the command
	// so what a launcher runs is the catalog's answer and not the client's.
	Launcher    string
	SessionSlug string
	Dir         string
	Command     string
	Cols        int
	Rows        int
}

// PopupLauncher is one configured launcher, as much of it as a menu needs.
// What it runs is deliberately absent: a caller invokes it by id.
type PopupLauncher struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
}

// PopupTerminalsService opens ephemeral terminals: a shell, or a command run
// through one, that this process owns outright and that ends when it is closed
// or when the app exits (ADR 0048). It holds no token, base URL or stream path
// — what it is reached over is the adapter's (ADR 0036).
type PopupTerminalsService struct {
	manager   *ptyterm.Manager
	directory terminalDirectory
	catalog   *actions.ActionStore
}

func newPopupTerminalsService(manager *ptyterm.Manager, directory terminalDirectory, catalog *actions.ActionStore) *PopupTerminalsService {
	return &PopupTerminalsService{manager: manager, directory: directory, catalog: catalog}
}

// Available reports build and platform support. There is no external program to
// find, so unlike tmux this cannot become available while the app is running.
func (s *PopupTerminalsService) Available(ctx context.Context) error {
	return popupError(s.manager.Available(ctx), "pop-up terminals need macOS or Linux and a desktop build.")
}

// Launchers returns the configured launchers in file order.
func (s *PopupTerminalsService) Launchers(context.Context) ([]PopupLauncher, error) {
	out := make([]PopupLauncher, 0)
	if s.catalog == nil {
		return out, nil
	}
	for _, l := range s.catalog.Launchers() {
		out = append(out, PopupLauncher{ID: l.ID, Label: l.Label, Icon: l.Icon})
	}
	return out, nil
}

// Open launches a terminal and returns it.
func (s *PopupTerminalsService) Open(ctx context.Context, req OpenPopupTerminal) (ptyterm.Terminal, error) {
	req, err := s.applyLauncher(req)
	if err != nil {
		return ptyterm.Terminal{}, err
	}
	dir, err := s.resolveDir(ctx, req)
	if err != nil {
		return ptyterm.Terminal{}, err
	}
	term, err := s.manager.Open(ctx, ptyterm.Spec{
		Dir:     dir,
		Command: req.Command,
		Cols:    req.Cols,
		Rows:    req.Rows,
	})
	if err != nil {
		return ptyterm.Terminal{}, popupError(err, "opening a terminal in %q", dir)
	}
	return term, nil
}

// applyLauncher folds a named launcher into the launch spec it stands for — a
// launcher is that spec with config in front of it and nothing more (ADR 0049).
// A configured cwd wins over the session's checkout, which is what pins a
// launcher to one directory; without one the launcher follows the session.
func (s *PopupTerminalsService) applyLauncher(req OpenPopupTerminal) (OpenPopupTerminal, error) {
	id := strings.TrimSpace(req.Launcher)
	if id == "" {
		return req, nil
	}
	if strings.TrimSpace(req.Command) != "" {
		return req, Errorf(KindInvalid, "a launcher brings its own command, so %q cannot also be given one", id)
	}
	if s.catalog == nil {
		return req, Errorf(KindUnavailable, "launchers are unavailable")
	}
	launcher, ok := s.catalog.Launcher(id)
	if !ok {
		return req, Errorf(KindNotFound, "unknown launcher %q", id)
	}
	req.Command = launcher.Command
	if launcher.Cwd != "" {
		req.SessionSlug = ""
		req.Dir = launcher.Cwd
	}
	return req, nil
}

// Close ends a terminal and every process in it, and reports whether there was
// one to close.
func (s *PopupTerminalsService) Close(_ context.Context, id string) (bool, error) {
	closed, err := s.manager.Close(id)
	if err != nil {
		return false, popupError(err, "closing terminal %q", id)
	}
	return closed, nil
}

// List reports the open terminals, oldest first.
func (s *PopupTerminalsService) List(context.Context) ([]ptyterm.Terminal, error) {
	return s.manager.List(), nil
}

// Subscribe returns a terminal's event stream and its unsubscribe func. There
// is one active subscriber per terminal; a second call closes the first channel.
func (s *PopupTerminalsService) Subscribe(_ context.Context, id string) (<-chan ptyterm.Event, func(), error) {
	events, unsubscribe, err := s.manager.Subscribe(id)
	if err != nil {
		return nil, nil, popupError(err, "subscribing to terminal %q", id)
	}
	return events, unsubscribe, nil
}

func (s *PopupTerminalsService) Write(_ context.Context, id string, p []byte) error {
	return popupError(s.manager.Write(id, p), "writing to terminal %q", id)
}

// Resize sets the terminal's size outright. Nothing else is attached to these
// PTYs, so unlike the tmux backend this is not a vote.
func (s *PopupTerminalsService) Resize(_ context.Context, id string, cols, rows int) error {
	return popupError(s.manager.Resize(id, cols, rows), "resizing terminal %q", id)
}

func (s *PopupTerminalsService) resolveDir(ctx context.Context, req OpenPopupTerminal) (string, error) {
	if slug := strings.TrimSpace(req.SessionSlug); slug != "" {
		if s.directory == nil {
			return "", Errorf(KindUnavailable, "reading sessions is unavailable")
		}
		return s.directory.SessionDirectory(ctx, slug)
	}
	if dir := strings.TrimSpace(req.Dir); dir != "" {
		return expandHome(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", Wrap(err, KindInternal, "finding a directory to open a terminal in")
	}
	return home, nil
}

// expandHome resolves a leading `~`, which nothing else does: chdir takes a
// path, not a shell word, so a launcher configured with `cwd: ~/src` would
// otherwise fail on a directory that plainly exists.
func expandHome(dir string) (string, error) {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", Wrap(err, KindInternal, "expanding %q", dir)
	}
	return filepath.Join(home, strings.TrimPrefix(dir, "~")), nil
}

// popupError classifies a ptyterm failure by sentinel rather than by message.
func popupError(err error, format string, args ...any) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ptyterm.ErrUnavailable):
		return Wrap(err, KindUnavailable, format, args...)
	case errors.Is(err, ptyterm.ErrInvalidSize), errors.Is(err, ptyterm.ErrInvalidSpec):
		return Wrap(err, KindInvalid, format, args...)
	case errors.Is(err, ptyterm.ErrNotFound):
		return Wrap(err, KindNotFound, format, args...)
	default:
		return Wrap(err, KindInternal, format, args...)
	}
}
