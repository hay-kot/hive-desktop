package tmuxcc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrSessionExists reports a NewSession call for a name that already
// addresses a live tmux session. Unlike Attach, which is meant to reuse
// whatever is already there, a caller of NewSession is minting a session it
// expects to be new.
var ErrSessionExists = errors.New("tmuxcc: session already exists")

// NewSession creates a detached tmux session named name, in dir, running
// command through a login shell. command is a shell command line rather than
// an argv — the same contract ptyterm.Spec.Command uses (ADR 0041): tmux execs
// the login shell directly with -c command as its own argv (more than one
// trailing argument after new-session's flags is executed as-is, never
// re-parsed by a shell), so the shell's own startup files are what resolve an
// agent binary's PATH, aliases and functions. Empty command opens an
// interactive login shell instead. This package still holds no environment
// policy of its own for it (ADR 0048's sibling-backend split from ptyterm) —
// the login shell is what does that work here, same as it does for ptyterm.
func (m *Manager) NewSession(ctx context.Context, name, dir, command string) error {
	if err := m.Available(ctx); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidName)
	}
	exists, err := m.HasSession(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: %s", ErrSessionExists, name)
	}

	args := []string{"new-session", "-d", "-s", name, "-c", dir, "--"}
	args = append(args, loginShellArgv(command)...)
	if _, err := m.oneShot(ctx, args...); err != nil {
		return fmt.Errorf("tmuxcc: new session %s: %w", name, err)
	}
	return nil
}

// CapturePane returns the current screen of name's active pane. -p prints to
// stdout and -J joins wrapped lines — the exact capture-pane invocation
// hive's own status detection runs
// (internal/hivecore/core/terminal/tmux.TmuxCapture.CapturePane), so
// terminal.Detector sees the input it was tuned against.
func (m *Manager) CapturePane(ctx context.Context, name string) (string, error) {
	if err := m.Available(ctx); err != nil {
		return "", err
	}
	lines, err := m.oneShot(ctx, "capture-pane", "-t", name, "-p", "-J")
	if err != nil {
		return "", fmt.Errorf("tmuxcc: capture pane %s: %w", name, err)
	}
	return strings.Join(lines, "\n"), nil
}

// SessionNames lists every live tmux session name carrying prefix. A server
// with no sessions at all — or none matching — reports zero names rather than
// an error, the same tolerance ListAllWindows extends to an empty tmux server.
func (m *Manager) SessionNames(ctx context.Context, prefix string) ([]string, error) {
	if err := m.Available(ctx); err != nil {
		return nil, err
	}
	lines, err := m.oneShot(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			names = append(names, line)
		}
	}
	return names, nil
}

// loginShellArgv is the argv new-session execs instead of the default shell:
// $SHELL -l, plus -c command when one is given. It is deliberately not shared
// with ptyterm's near-identical logic — ADR 0048 keeps tmuxcc and ptyterm
// sibling backends with no shared interface, and this is that same
// command-line contract implemented for the other one.
func loginShellArgv(command string) []string {
	shell := []string{resolveLoginShell(), "-l"}
	if strings.TrimSpace(command) == "" {
		return shell
	}
	return append(shell, "-c", command)
}

func resolveLoginShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	for _, candidate := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "/bin/sh"
}
