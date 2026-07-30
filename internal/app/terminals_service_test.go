//go:build !server

package app

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

// These tests drive a real tmux server: whether to attach or to offer a start
// is a decision about what tmux is holding, and a faked one would only prove the
// fake. Each gets its own TMUX_TMPDIR and an unconditional kill-server, so they
// never touch a developer's live tmux.

var tmuxVersion = regexp.MustCompile(`(\d+)\.(\d+)`)

func requireTmux(t *testing.T) {
	t.Helper()
	// A tmux client resolves its server from $TMUX before TMUX_TMPDIR, so a
	// missed scrub would kill-server the tmux hosting this very process.
	if os.Getenv("TMUX") != "" {
		t.Skip("running inside tmux; the real-tmux tests run in CI or Docker only")
	}
	out, err := exec.CommandContext(t.Context(), "tmux", "-V").Output()
	if err != nil {
		t.Skip("tmux is not installed")
	}
	match := tmuxVersion.FindStringSubmatch(string(out))
	if match == nil {
		t.Skipf("could not read a version from %q", strings.TrimSpace(string(out)))
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < 3 || (major == 3 && minor < 2) {
		t.Skipf("tmux %d.%d is older than the 3.2 control-mode floor", major, minor)
	}
}

// privateTmux points every tmux command in the test — the service's included —
// at a server of its own, and returns a runner for the fixture's commands.
func privateTmux(t *testing.T) func(args ...string) error {
	t.Helper()
	requireTmux(t)

	// t.TempDir() bakes the test name into the path, and a tmux socket is a unix
	// socket bound by the ~104 byte sun_path limit — on macOS the two overflow it.
	dir, err := os.MkdirTemp("", "hvtmux") //nolint:usetesting // socket path length, see above
	require.NoError(t, err)
	t.Setenv("TMUX_TMPDIR", dir)

	run := func(args ...string) error {
		cmd := exec.Command("tmux", args...) //nolint:noctx // cleanup runs past the test context
		cmd.Env = os.Environ()
		return cmd.Run()
	}
	t.Cleanup(func() {
		_ = run("kill-server")
		_ = os.RemoveAll(dir)
	})
	return run
}

func newTestTerminals(t *testing.T, starter terminalStarter) *TerminalsService {
	t.Helper()
	manager := tmuxcc.NewManager(t.Context(), tmuxcc.ManagerOptions{Logger: zerolog.Nop()})
	t.Cleanup(func() { _ = manager.Stop(context.WithoutCancel(t.Context())) })
	return newTerminalsService(manager, tmuxcc.NopMetrics, starter)
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
