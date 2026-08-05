//go:build !server

package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
	"github.com/hay-kot/hive-desktop/internal/tmuxtest"
)

// These tests drive a real tmux server: whether to attach or to offer a start
// is a decision about what tmux is holding, and a faked one would only prove the
// fake. See internal/tmuxtest for how they are kept away from a developer's own.

// privateTmux points every tmux command in the test — the service's included —
// at a server of its own, and returns a runner for the fixture's commands.
func privateTmux(t *testing.T) func(args ...string) error {
	t.Helper()
	socket := tmuxtest.Private(t)

	return func(args ...string) error {
		cmd := exec.Command("tmux", append([]string{"-S", socket}, args...)...) //nolint:noctx // cleanup runs past the test context
		cmd.Env = tmuxtest.ScrubbedEnv()
		return cmd.Run()
	}
}

func newTestTerminals(t *testing.T, starter terminalStarter) *TerminalsService {
	t.Helper()
	return newTestTerminalsIn(t, starter, os.UserHomeDir)
}

// newTestTerminalsIn is newTestTerminals with the home directory the scratch
// terminal opens in, so a test can assert where it landed without opening one in
// the developer's own home.
func newTestTerminalsIn(t *testing.T, starter terminalStarter, home func() (string, error)) *TerminalsService {
	t.Helper()
	manager := tmuxcc.NewManager(t.Context(), tmuxcc.ManagerOptions{Logger: zerolog.Nop()})
	t.Cleanup(func() { _ = manager.Stop(context.WithoutCancel(t.Context())) })
	return newTerminalsService(manager, tmuxcc.NopMetrics, starter, home)
}

// spawningStarter stands in for the session service: it creates the tmux session
// the way hive's spawn would, and records that it was asked to.
type spawningStarter struct {
	tmux  func(args ...string) error
	calls []string
	err   error
}

func (s *spawningStarter) StartTmuxSession(_ context.Context, slug string) error {
	s.calls = append(s.calls, slug)
	if s.err != nil {
		return s.err
	}
	return s.tmux("new-session", "-d", "-s", slug, "-n", "claude", "sh")
}

func TestTerminalsStartThenAttachIsTheColdPath(t *testing.T) {
	tmux := privateTmux(t)
	starter := &spawningStarter{tmux: tmux}
	terminals := newTestTerminals(t, starter)

	// Attaching does not spawn: a session that was never started reports itself
	// so the caller can offer to start it, and nothing runs an agent behind the
	// user's back.
	_, err := terminals.Attach(t.Context(), "hive-cold", 120, 40)
	assert.Equal(t, KindNotFound, KindOf(err))
	assert.Empty(t, starter.calls)

	started, err := terminals.Start(t.Context(), "hive-cold")
	require.NoError(t, err)
	assert.True(t, started, "this call is what created the session")
	assert.Equal(t, []string{"hive-cold"}, starter.calls)

	windows, err := terminals.Attach(t.Context(), "hive-cold", 120, 40)
	require.NoError(t, err)
	require.Len(t, windows, 1, "the attach lands on the session the start spawned")
	assert.Equal(t, "claude", windows[0].Name)
}

func TestTerminalsStartLeavesALiveSessionAlone(t *testing.T) {
	tmux := privateTmux(t)
	require.NoError(t, tmux("-f", "/dev/null", "new-session", "-d", "-s", "hive-live", "-n", "claude", "-x", "120", "-y", "40", "sh"))
	starter := &spawningStarter{tmux: tmux}
	terminals := newTestTerminals(t, starter)

	started, err := terminals.Start(t.Context(), "hive-live")
	require.NoError(t, err)
	assert.False(t, started, "a session tmux is already running is never spawned over")
	assert.Empty(t, starter.calls)

	windows, err := terminals.Attach(t.Context(), "hive-live", 120, 40)
	require.NoError(t, err)
	require.Len(t, windows, 1)
}

func TestTerminalsKillEndsTheSessionAndLeavesItStartableAgain(t *testing.T) {
	tmux := privateTmux(t)
	require.NoError(t, tmux("-f", "/dev/null", "new-session", "-d", "-s", "hive-kill", "-n", "claude", "-x", "120", "-y", "40", "sh"))
	starter := &spawningStarter{tmux: tmux}
	terminals := newTestTerminals(t, starter)

	_, err := terminals.Attach(t.Context(), "hive-kill", 120, 40)
	require.NoError(t, err)

	killed, err := terminals.Kill(t.Context(), "hive-kill")
	require.NoError(t, err)
	assert.True(t, killed)

	// The attach that follows sees a session that is not running rather than a
	// stale client, which is what puts the view back on its start panel.
	_, err = terminals.Attach(t.Context(), "hive-kill", 120, 40)
	assert.Equal(t, KindNotFound, KindOf(err))

	// Killing what is already gone is nothing to do, not a failure.
	killed, err = terminals.Kill(t.Context(), "hive-kill")
	require.NoError(t, err)
	assert.False(t, killed)

	started, err := terminals.Start(t.Context(), "hive-kill")
	require.NoError(t, err)
	assert.True(t, started, "a killed session can be started again")
}

// Reordering is the one operation whose result is not what the caller asked
// for: tmux owns the order, so the service answers with what tmux settled on.
func TestTerminalsMoveWindowReordersAndKeepsTheSelection(t *testing.T) {
	tmux := privateTmux(t)
	require.NoError(t, tmux("-f", "/dev/null", "new-session", "-d", "-s", "hive-move", "-n", "alpha", "-x", "120", "-y", "40", "sh"))
	require.NoError(t, tmux("new-window", "-t", "hive-move", "-n", "bravo", "sh"))
	require.NoError(t, tmux("new-window", "-t", "hive-move", "-n", "charlie", "sh"))
	terminals := newTestTerminals(t, &spawningStarter{tmux: tmux})

	windows, err := terminals.Attach(t.Context(), "hive-move", 120, 40)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "bravo", "charlie"}, windowNames(windows))
	active := activeWindowID(t, windows)

	moved, err := terminals.MoveWindow(t.Context(), "hive-move", windows[0].ID, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"bravo", "charlie", "alpha"}, windowNames(moved))
	assert.Equal(t, active, activeWindowID(t, moved), "a reorder is not a selection")

	// Indices are what tmux's own key bindings and every other attached client
	// address a window by, so an insert must not leave them with holes.
	listed, err := terminals.ListAllWindows(t.Context(), []string{"hive-move"})
	require.NoError(t, err)
	assert.Equal(t, windowNames(moved), windowNames(listed["hive-move"]))
	// The fixture's server runs with no config, so the run starts at tmux's own
	// base-index of 0.
	assert.Equal(t, "0 1 2", tmuxFields(t, "list-windows", "-t", "hive-move", "-F", "#{window_index}"))
}

func TestTerminalsMoveWindowClassifiesWhatItRefuses(t *testing.T) {
	tmux := privateTmux(t)
	require.NoError(t, tmux("-f", "/dev/null", "new-session", "-d", "-s", "hive-badmove", "-n", "alpha", "-x", "120", "-y", "40", "sh"))
	terminals := newTestTerminals(t, &spawningStarter{tmux: tmux})

	_, err := terminals.MoveWindow(t.Context(), "hive-badmove", "@0", 0)
	assert.Equal(t, KindNotFound, KindOf(err), "no client is attached for that slug")

	windows, err := terminals.Attach(t.Context(), "hive-badmove", 120, 40)
	require.NoError(t, err)

	_, err = terminals.MoveWindow(t.Context(), "hive-badmove", "@404", 0)
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = terminals.MoveWindow(t.Context(), "hive-badmove", windows[0].ID, 7)
	assert.Equal(t, KindInvalid, KindOf(err))
}

func windowNames(windows []tmuxcc.Window) []string {
	names := make([]string, 0, len(windows))
	for _, w := range windows {
		names = append(names, w.Name)
	}
	return names
}

func activeWindowID(t *testing.T, windows []tmuxcc.Window) string {
	t.Helper()
	for _, w := range windows {
		if w.Active {
			return w.ID
		}
	}
	t.Fatalf("no active window in %#v", windows)
	return ""
}

// tmuxFields reads a tmux listing as one space-joined line, so a per-row format
// reads as the sequence it describes.
func tmuxFields(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "tmux", args...).Output()
	require.NoError(t, err)
	return strings.Join(strings.Fields(string(out)), " ")
}

// The scratch terminal is the one session the desktop creates itself: hive has
// no record to spawn from, so what it gets instead is the user's home directory.
func TestTerminalsStartScratchOpensItInHomeWithoutAskingHive(t *testing.T) {
	tmux := privateTmux(t)
	home := t.TempDir()
	starter := &spawningStarter{tmux: tmux}
	terminals := newTestTerminalsIn(t, starter, func() (string, error) { return home, nil })

	started, err := terminals.Start(t.Context(), ScratchSlug)
	require.NoError(t, err)
	assert.True(t, started)
	assert.Empty(t, starter.calls, "there is no hive session to spawn from")

	// The home directory is the *session's* working directory rather than the
	// first window's, which is what makes every tab opened later start there too.
	assert.Equal(t, realpath(t, home), realpath(t, tmuxFields(t, "display-message", "-p", "-t", ScratchSlug, "#{session_path}")))

	// Nothing else about it is special: the slug is the whole contract, so the
	// attach and the window it opens are the ones every session uses.
	windows, err := terminals.Attach(t.Context(), ScratchSlug, 120, 40)
	require.NoError(t, err)
	require.Len(t, windows, 1)

	tab, err := terminals.NewWindow(t.Context(), ScratchSlug)
	require.NoError(t, err)
	assert.Equal(t, realpath(t, home), realpath(t, tmuxFields(t, "display-message", "-p", "-t", tab, "#{pane_current_path}")))

	started, err = terminals.Start(t.Context(), ScratchSlug)
	require.NoError(t, err)
	assert.False(t, started, "a scratch terminal that is running is never recreated over")
}

// The + on a row is offered for the session, not for what is on screen, so it
// has to work before anything has attached — which is what a first click on the
// pinned terminal is.
func TestTerminalsNewWindowWithoutAnAttachOpensInTheSessionDirectory(t *testing.T) {
	tmux := privateTmux(t)
	home := t.TempDir()
	terminals := newTestTerminalsIn(t, &spawningStarter{tmux: tmux}, func() (string, error) { return home, nil })

	_, err := terminals.Start(t.Context(), ScratchSlug)
	require.NoError(t, err)

	// No Attach in between: the window is made by a one-shot, and it lands where
	// the session lives rather than in this process's working directory.
	id, err := terminals.NewWindow(t.Context(), ScratchSlug)
	require.NoError(t, err)
	assert.Equal(t, realpath(t, home), realpath(t, tmuxFields(t, "display-message", "-p", "-t", id, "#{pane_current_path}")))

	windows, err := terminals.ListAllWindows(t.Context(), []string{ScratchSlug})
	require.NoError(t, err)
	assert.Len(t, windows[ScratchSlug], 2)
}

func TestTerminalsNewWindowInASessionThatIsNotRunning(t *testing.T) {
	privateTmux(t)
	terminals := newTestTerminals(t, &spawningStarter{})

	_, err := terminals.NewWindow(t.Context(), "hive-gone")
	// A session to add a window to is the caller's to create — a start for a
	// hive session runs its agent, so this must not do it on their behalf.
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestTerminalsStartScratchReportsAnUnreadableHome(t *testing.T) {
	privateTmux(t)
	terminals := newTestTerminalsIn(t, &spawningStarter{}, func() (string, error) { return "", errors.New("no home") })

	_, err := terminals.Start(t.Context(), ScratchSlug)
	assert.Equal(t, KindUnavailable, KindOf(err))
}

// The scratch session lives in tmux's one session namespace alongside hive's, so
// the slug it claims has to be one hive cannot mint — otherwise a session named
// "Scratch" would silently become the scratch terminal, or take it over.
func TestScratchSlugIsUnreachableFromASessionName(t *testing.T) {
	terminals := newTestTerminals(t, nil)
	assert.Equal(t, ScratchSlug, terminals.Scratch(t.Context()).Slug)

	for _, name := range []string{"Scratch", "scratch", "SCRATCH", "  Scratch  ", "scratch/1"} {
		assert.NotEqual(t, ScratchSlug, dispatch.SlugifySessionName(name),
			"a hive session named %q must not slugify onto the scratch terminal", name)
	}
}

func realpath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}

func TestTerminalsStartReportsWhyItCouldNot(t *testing.T) {
	tmux := privateTmux(t)
	starter := &spawningStarter{tmux: tmux, err: Errorf(KindNotFound, "no session named %q", "hive-gone")}
	terminals := newTestTerminals(t, starter)

	_, err := terminals.Start(t.Context(), "hive-gone")
	// The starter's classification survives: a start that failed because nothing
	// knows the slug must not read as a tmux fault.
	assert.Equal(t, KindNotFound, KindOf(err))
	assert.Contains(t, err.Error(), "hive-gone")
}
