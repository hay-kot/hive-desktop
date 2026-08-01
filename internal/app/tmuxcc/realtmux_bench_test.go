//go:build !server

package tmuxcc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Benchmarks in this file drive a real tmux server on a private socket. They
// are the only measurement that captures what dominates attach in production —
// the per-command round trip to tmux — because the in-memory fake answers over
// a pipe with none of it.

const (
	// benchHistoryLimit is tmux's own default history-limit, which a `-f
	// /dev/null` server is guaranteed to have and which historyLines matches —
	// so a saturated pane is exactly the worst case a first paint is bounded to.
	benchHistoryLimit = 2000
	benchCols         = "200"
	benchRows         = "50"
)

// benchServer is a tmux server of our own. Two things make it safe and
// repeatable, and neither is optional: every command is scoped to a private
// socket, so no benchmark can reach the developer's own server (the one this
// process may itself be running inside), and the server starts with `-f
// /dev/null`, so a developer's ~/.tmux.conf cannot change what is measured —
// history-limit in particular, which sets how much a first paint replays.
type benchServer struct {
	socket string
	binary string
}

func startBenchServer(tb testing.TB) *benchServer {
	tb.Helper()
	binary, err := exec.LookPath("tmux")
	if err != nil {
		tb.Skip("tmux not installed")
	}
	// Not tb.TempDir: a unix socket path is capped near 104 bytes on darwin and
	// the per-test directory name alone can exceed it.
	dir, err := os.MkdirTemp("", "hcbench") //nolint:usetesting // see above
	if err != nil {
		tb.Fatalf("temp dir: %v", err)
	}
	s := &benchServer{socket: filepath.Join(dir, "s"), binary: binary}
	tb.Cleanup(func() {
		// -S scopes the kill to this benchmark's own server. It must never be
		// dropped: the developer's shell may itself be running inside tmux, and
		// an unscoped kill-server would take it down.
		_ = exec.Command(binary, "-S", s.socket, "kill-server").Run() //nolint:noctx // teardown
		_ = os.RemoveAll(dir)
	})
	return s
}

func (s *benchServer) run(tb testing.TB, args ...string) string {
	tb.Helper()
	full := append([]string{"-f", "/dev/null", "-S", s.socket}, args...)
	out, err := exec.Command(s.binary, full...).CombinedOutput() //nolint:noctx // bench setup
	if err != nil {
		tb.Fatalf("tmux %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// fillFile writes the scrollback fixture: wide, SGR-coloured log lines, which
// is what a build or an agent session actually leaves in a pane. Line *width*
// is the point — `seq` fills the same number of rows with 5-byte lines and
// understates the bytes a first paint carries by more than 20x.
func fillFile(tb testing.TB, dir string) string {
	tb.Helper()
	var buf strings.Builder
	for i := range benchHistoryLimit * 3 {
		fmt.Fprintf(&buf,
			"\033[32m%06d\033[0m \033[1;34mINFO\033[0m module.pkg.name handler=request id=%d elapsed=%dms status=ok payload=%d retries=0 trace=abcdef0123456789\n",
			i, i, i%900, i*7)
	}
	path := filepath.Join(dir, "fill.txt")
	if err := os.WriteFile(path, []byte(buf.String()), 0o600); err != nil {
		tb.Fatalf("write fill fixture: %v", err)
	}
	return path
}

// newSession builds a session with windows windows. When fillHistory is set
// every pane's scrollback is driven to the server's history-limit, which is the
// worst case a first paint has to serialize; an empty pane measures almost
// nothing of what attach actually costs in use.
//
// Panes run /bin/sh and the fixture is catted by absolute path on purpose: an
// interactive login shell would load the developer's rc files, and a `cat`
// aliased to a pager fills no history at all because the pager holds the
// alternate screen.
func (s *benchServer) newSession(tb testing.TB, slug string, windows int, fillHistory bool) {
	tb.Helper()
	s.run(tb, "new-session", "-d", "-s", slug, "-x", benchCols, "-y", benchRows, "/bin/sh")
	for i := 1; i < windows; i++ {
		s.run(tb, "new-window", "-t", slug, "/bin/sh")
	}
	if !fillHistory {
		return
	}
	fill := fillFile(tb, filepath.Dir(s.socket))
	// Far more lines than the limit, so history ends up saturated rather than
	// at whatever depth a race left it: the depth is then the limit itself and
	// does not vary run to run.
	for _, w := range s.windowIDs(tb, slug) {
		s.run(tb, "send-keys", "-t", w, "/bin/cat "+fill, "Enter")
	}
	s.settle(tb, slug)
}

func (s *benchServer) windowIDs(tb testing.TB, slug string) []string {
	tb.Helper()
	out := s.run(tb, "list-windows", "-t", slug, "-F", "#{window_id}")
	return strings.Split(out, "\n")
}

// settle waits for every pane's history to stop growing. send-keys returns as
// soon as tmux has queued the keystroke, so without this the benchmark races a
// shell that is still printing. Stability is the signal rather than a target
// depth, because the visible screen holds rows that never enter history and the
// exact saturated depth is therefore a few rows under the limit.
func (s *benchServer) settle(tb testing.TB, slug string) {
	tb.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var previous []int
	for {
		current := make([]int, 0, 8)
		for _, w := range s.windowIDs(tb, slug) {
			n, _ := strconv.Atoi(s.run(tb, "display-message", "-p", "-t", w, "#{history_size}"))
			current = append(current, n)
		}
		if slices.Equal(current, previous) && current[0] > 0 {
			return
		}
		if time.Now().After(deadline) {
			tb.Fatalf("timed out waiting for scrollback to settle at %v", current)
		}
		previous = current
		time.Sleep(150 * time.Millisecond)
	}
}

// historyDepth reports the settled scrollback of the session's first pane, so a
// benchmark can state the depth it actually measured against.
func (s *benchServer) historyDepth(tb testing.TB, slug string) int {
	tb.Helper()
	n, _ := strconv.Atoi(s.run(tb, "display-message", "-p", "-t", s.windowIDs(tb, slug)[0], "#{history_size}"))
	return n
}

// attachEnv points this process's tmux resolution at the bench socket.
// tmuxcc reads $TMUX to find the server (see socketFromTMUX), so setting it is
// what keeps Attach and the setup commands on the same server.
func (s *benchServer) attachEnv(tb testing.TB) {
	tb.Helper()
	tb.Setenv("TMUX", s.socket+",0,0")
}

func benchAttachOptions(slug, binary string) Options {
	return Options{
		Slug:   slug,
		Cols:   200,
		Rows:   50,
		Binary: binary,
		Logger: zerolog.Nop(),
	}
}

// BenchmarkAttachRealTmux measures the whole attach sequence — handshake,
// size vote, list-windows, and a first paint per window — against real tmux.
// The window counts are the interesting axis: the sequence runs three
// round-trip commands per window, so this is where their cost shows up.
func BenchmarkAttachRealTmux(b *testing.B) {
	for _, tc := range []struct {
		name    string
		windows int
		history bool
	}{
		{"1window_empty", 1, false},
		{"1window_fullhistory", 1, true},
		{"4windows_fullhistory", 4, true},
		{"8windows_fullhistory", 8, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := startBenchServer(b)
			s.attachEnv(b)
			const slug = "bench"
			s.newSession(b, slug, tc.windows, tc.history)
			if tc.history {
				b.Logf("scrollback depth per pane: %d lines", s.historyDepth(b, slug))
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				client, err := Attach(context.Background(), context.Background(), benchAttachOptions(slug, s.binary))
				if err != nil {
					b.Fatalf("attach: %v", err)
				}
				b.StopTimer()
				_ = client.Close(context.Background())
				b.StartTimer()
			}
		})
	}
}
