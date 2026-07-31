// Package ptyterm is a process-managed terminal backend: this process owns a
// PTY per tab and the shell on the far end of it, with no terminal multiplexer
// in between. It is the counterpart to internal/app/tmuxcc and speaks the same
// event vocabulary, so one transport and one renderer serve either (ADR 0045).
//
// What it trades away is persistence. A tmux session outlives the app; these
// sessions are this process's children and die with it. What it buys is a
// stream nothing re-encodes, a size that is set rather than negotiated, and a
// replay buffer this process owns.
package ptyterm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	minDimension = 1
	maxDimension = 1000

	// defaultCols/defaultRows size a window spawned before anything measured
	// the pane. Unlike a tmux attach there is no other client whose size could
	// be inherited, so a PTY has to open at something.
	defaultCols = 80
	defaultRows = 24

	// defaultReplayBytes is one window's replay ring. 256 KiB is a few screens
	// of scrollback at a normal size and a bounded cost per idle tab.
	defaultReplayBytes = 256 << 10
	// defaultBufferBytes bounds one subscriber's queue before the stream is cut.
	// Output coalesces, so this is reached only by a consumer that has stopped
	// reading rather than by one that is merely behind.
	defaultBufferBytes = 4 << 20
)

var (
	// ErrUnavailable is returned on the server build and on unsupported platforms.
	ErrUnavailable = errors.New("ptyterm: process-managed terminals unavailable")
	// ErrInvalidSize is returned for out-of-bounds cols/rows.
	ErrInvalidSize = errors.New("ptyterm: invalid terminal size")
	// ErrNotAttached is returned for a name with no live session.
	ErrNotAttached = errors.New("ptyterm: no session")
	// ErrUnknownWindow is returned for a window id absent from the session.
	ErrUnknownWindow = errors.New("ptyterm: unknown window")
	// ErrInvalidName is returned for a window name or command that cannot be used.
	ErrInvalidName = errors.New("ptyterm: invalid name")
)

// ManagerOptions configures the session set.
type ManagerOptions struct {
	// Environ answers the environment a shell is spawned with — execenv's
	// resolved PATH in the app, whatever a test hands it otherwise. nil means
	// this process's own.
	Environ func(context.Context) []string
	// Shell overrides the command a window runs. Empty resolves $SHELL.
	Shell []string

	ReplayBytes int
	BufferBytes int
}

// Manager owns the process-managed sessions, keyed by the same slug tmuxcc uses
// so the two backends are addressed identically.
type Manager struct {
	environ     func(context.Context) []string
	shell       []string
	replayBytes int
	bufferBytes int

	mu       sync.Mutex
	sessions map[string]*session
	stopped  bool
}

func NewManager(opts ManagerOptions) *Manager {
	m := &Manager{
		environ:     opts.Environ,
		shell:       opts.Shell,
		replayBytes: opts.ReplayBytes,
		bufferBytes: opts.BufferBytes,
		sessions:    map[string]*session{},
	}
	if m.environ == nil {
		m.environ = func(context.Context) []string { return os.Environ() }
	}
	return m
}

// Available reports whether this build and platform can run a PTY at all. There
// is no external program to find, so unlike tmux there is nothing to discover
// and nothing a user can install to change the answer.
func (m *Manager) Available(context.Context) error {
	if !platformSupported() {
		return ErrUnavailable
	}
	return nil
}

// Start creates the session named slug, with one window whose shell opens in
// dir, and reports whether it had to. Starting is separate from attaching for
// the same reason it is in tmux (ADR 0044): the view attaches on its own, and
// running a command in a checkout is the user's call.
func (m *Manager) Start(ctx context.Context, slug, dir string) (bool, error) {
	if err := m.Available(ctx); err != nil {
		return false, err
	}
	if slug == "" {
		return false, fmt.Errorf("%w: empty slug", ErrNotAttached)
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return false, ErrUnavailable
	}
	if _, ok := m.sessions[slug]; ok {
		m.mu.Unlock()
		return false, nil
	}
	m.mu.Unlock()

	sess, err := newSession(slug, sessionOptions{
		Dir:         dir,
		Command:     m.command(),
		Env:         terminalEnv(m.environ(ctx)),
		Cols:        defaultCols,
		Rows:        defaultRows,
		ReplayBytes: m.replayBytes,
		BufferBytes: m.bufferBytes,
	}, m.forget)
	if err != nil {
		return false, err
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		sess.close()
		return false, ErrUnavailable
	}
	// A concurrent Start won the slug while this one was spawning; the loser's
	// session is closed rather than replacing a session something may already
	// be streaming.
	if _, ok := m.sessions[slug]; ok {
		m.mu.Unlock()
		sess.close()
		return false, nil
	}
	m.sessions[slug] = sess
	m.mu.Unlock()
	return true, nil
}

// Attach returns the window set of a live session. It never creates one: a slug
// with no session is ErrNotAttached, which the caller answers with Start.
func (m *Manager) Attach(ctx context.Context, slug string, cols, rows int) ([]Window, error) {
	if err := m.Available(ctx); err != nil {
		return nil, err
	}
	if err := validateAttachSize(cols, rows); err != nil {
		return nil, err
	}
	sess, err := m.session(slug)
	if err != nil {
		return nil, err
	}
	if cols > 0 && rows > 0 {
		_ = sess.resize(cols, rows)
	}
	return sess.listWindows(), nil
}

// HasSession reports whether slug names a live session.
func (m *Manager) HasSession(slug string) bool {
	_, err := m.session(slug)
	return err == nil
}

// Kill ends the session and everything running in it, reporting whether there
// was one to kill.
func (m *Manager) Kill(slug string) (bool, error) {
	m.mu.Lock()
	sess, ok := m.sessions[slug]
	if ok {
		delete(m.sessions, slug)
	}
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	sess.close()
	return true, nil
}

// ListWindows answers a session's windows, or none for a slug with no session —
// callers enumerate every session the app knows about, most of which will never
// have had a terminal opened.
func (m *Manager) ListWindows(slug string) []Window {
	sess, err := m.session(slug)
	if err != nil {
		return nil
	}
	return sess.listWindows()
}

// Subscribe opens slug's event stream. There is one subscriber per session; a
// second call closes the first channel.
func (m *Manager) Subscribe(slug string) (<-chan Event, func(), error) {
	sess, err := m.session(slug)
	if err != nil {
		return nil, nil, err
	}
	ch, unsubscribe := sess.subscribe()
	return ch, unsubscribe, nil
}

func (m *Manager) Write(slug, windowID string, p []byte) error {
	sess, err := m.session(slug)
	if err != nil {
		return err
	}
	return sess.write(windowID, p)
}

// Resize sets the session's size. It is not a vote: one client renders these
// windows, so the size asked for is the size the processes are told.
func (m *Manager) Resize(slug string, cols, rows int) error {
	if err := validateSize(cols, rows); err != nil {
		return err
	}
	sess, err := m.session(slug)
	if err != nil {
		return err
	}
	return sess.resize(cols, rows)
}

func (m *Manager) NewWindow(slug string) (string, error) {
	sess, err := m.session(slug)
	if err != nil {
		return "", err
	}
	return sess.newWindow()
}

func (m *Manager) CloseWindow(slug, windowID string) error {
	sess, err := m.session(slug)
	if err != nil {
		return err
	}
	return sess.closeWindow(windowID)
}

func (m *Manager) RenameWindow(slug, windowID, name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty window name", ErrInvalidName)
	}
	sess, err := m.session(slug)
	if err != nil {
		return err
	}
	return sess.renameWindow(windowID, name)
}

func (m *Manager) SelectWindow(slug, windowID string) error {
	sess, err := m.session(slug)
	if err != nil {
		return err
	}
	if _, err := sess.window(windowID); err != nil {
		return err
	}
	sess.selectWindow(windowID)
	return nil
}

// Detach is a no-op that exists to answer the transport's own vocabulary. The
// stream is dropped when its socket closes; the session lives on until it is
// killed or the app exits, which is what makes switching sessions free.
func (m *Manager) Detach(string) error { return nil }

// Stop closes every session. Idempotent.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	m.stopped = true
	sessions := make([]*session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	m.sessions = map[string]*session{}
	m.mu.Unlock()

	for _, sess := range sessions {
		sess.close()
	}
	return nil
}

func (m *Manager) session(slug string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[slug]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotAttached, slug)
	}
	return sess, nil
}

// forget drops a session whose last window exited, so the slug is free to be
// started again.
func (m *Manager) forget(slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, slug)
}

// command is the shell a window runs. It is started as a login shell so the
// user's startup files run, which is also what gives the session the PATH their
// own terminal has (ADR 0041) — execenv's resolved environment is the floor
// under it, not a replacement for it.
func (m *Manager) command() []string {
	if len(m.shell) > 0 {
		return m.shell
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = fallbackShell()
	}
	return []string{shell, "-l"}
}

func fallbackShell() string {
	for _, candidate := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "/bin/sh"
}

// terminalEnv declares what the emulator on the far end of the socket can do.
// xterm.js renders 256 colours and true colour, and a shell that is not told so
// degrades its prompt for a terminal that is not this one.
func terminalEnv(base []string) []string {
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		switch {
		case hasPrefix(kv, "TERM="), hasPrefix(kv, "COLORTERM="), hasPrefix(kv, "TERM_PROGRAM="):
			continue
		case hasPrefix(kv, "TMUX="), hasPrefix(kv, "TMUX_PANE="):
			// Inherited tmux client variables would tell the shell it is inside
			// a multiplexer that is not there.
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=hive-desktop")
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func defaultWindowName(command []string) string {
	if len(command) == 0 {
		return "shell"
	}
	return filepath.Base(command[0])
}

func validateSize(cols, rows int) error {
	if cols < minDimension || cols > maxDimension || rows < minDimension || rows > maxDimension {
		return fmt.Errorf("%w: %dx%d", ErrInvalidSize, cols, rows)
	}
	return nil
}

// validateAttachSize accepts 0x0, the caller's way of saying it has measured
// nothing yet. Any other partial size is invalid: a caller with a measurement
// has both numbers.
func validateAttachSize(cols, rows int) error {
	if cols == 0 && rows == 0 {
		return nil
	}
	return validateSize(cols, rows)
}

func platformSupported() bool {
	return buildSupportsTerminal && (runtime.GOOS == "darwin" || runtime.GOOS == "linux")
}
