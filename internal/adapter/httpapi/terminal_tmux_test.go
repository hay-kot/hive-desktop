package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests drive a real tmux server. Each one gets its own TMUX_TMPDIR, its
// own session name, and an unconditional kill-server, so they never touch a
// developer's live tmux and never see each other's.

var tmuxVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)`)

func requireTmux(t *testing.T) {
	t.Helper()
	// A tmux client resolves its server from $TMUX before TMUX_TMPDIR, so any
	// invocation that misses the scrub would create sessions on — and
	// kill-server — the tmux hosting this very process. Not worth the bet.
	if os.Getenv("TMUX") != "" {
		t.Skip("running inside tmux; the real-tmux integration tests run in CI or Docker only")
	}
	out, err := exec.CommandContext(t.Context(), "tmux", "-V").Output()
	if err != nil {
		t.Skip("tmux is not installed; the terminal integration tests need one")
	}
	match := tmuxVersionPattern.FindStringSubmatch(string(out))
	if match == nil {
		t.Skipf("could not read a version from %q", strings.TrimSpace(string(out)))
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < 3 || (major == 3 && minor < 2) {
		t.Skipf("tmux %d.%d is older than the 3.2 control-mode floor", major, minor)
	}
}

type tmuxFixture struct {
	t    *testing.T
	slug string
}

// startTmux boots a private tmux server holding one detached session named
// slug, with a single `sh` window called claude.
func startTmux(t *testing.T, slug string) *tmuxFixture {
	t.Helper()
	requireTmux(t)

	// t.TempDir() bakes the test name into the path, and a tmux socket is a unix
	// socket bound by the ~104 byte sun_path limit — on macOS the two together
	// overflow it.
	dir, err := os.MkdirTemp("", "hvtmux") //nolint:usetesting // socket path length, see above
	require.NoError(t, err)
	t.Setenv("TMUX_TMPDIR", dir)
	t.Cleanup(func() {
		kill := exec.Command("tmux", "kill-server")
		kill.Env = scrubbedTmuxEnv()
		_ = kill.Run()
		_ = os.RemoveAll(dir)
	})

	fixture := &tmuxFixture{t: t, slug: slug}
	fixture.tmux("-f", "/dev/null", "new-session", "-d", "-s", slug, "-n", "claude", "-x", "120", "-y", "40", "sh")
	return fixture
}

func (f *tmuxFixture) tmux(args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), "tmux", args...)
	cmd.Env = scrubbedTmuxEnv()
	out, err := cmd.CombinedOutput()
	require.NoErrorf(f.t, err, "tmux %v: %s", args, out)
	return string(out)
}

// scrubbedTmuxEnv drops $TMUX/$TMUX_PANE so every fixture command resolves the
// server from the test's TMUX_TMPDIR, exactly as tmuxcc's detachedEnv does for
// the control client.
func scrubbedTmuxEnv() []string {
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		kept = append(kept, kv)
	}
	return kept
}

// windowSize reports the session's own idea of its window size, which is what
// an attach must not change unless it voted a size.
func (f *tmuxFixture) windowSize() string {
	f.t.Helper()
	return strings.TrimSpace(f.tmux("list-windows", "-t", f.slug, "-F", "#{window_width}x#{window_height}"))
}

func (f *tmuxFixture) newWindow(name string) {
	f.t.Helper()
	f.tmux("new-window", "-t", f.slug, "-n", name, "sh")
}

// awaitPane polls capture-pane until the session's visible screen contains
// want, which is how a test observes what actually reached the shell.
func (f *tmuxFixture) awaitPane(target, want string) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = f.tmux("capture-pane", "-p", "-t", target)
		if strings.Contains(last, want) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	f.t.Fatalf("pane %s never showed %q; last capture:\n%s", target, want, last)
}

type attachResult struct {
	Windows []struct {
		WindowID string `json:"windowId"`
		Name     string `json:"name"`
		Active   bool   `json:"active"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	} `json:"windows"`
}

func (h *terminalHarness) attach(t *testing.T, slug string) attachResult {
	t.Helper()
	resp := h.post(t, "/api/terminal/attach", testToken, map[string]any{"slug": slug, "cols": 120, "rows": 40})
	require.Equal(t, http.StatusOK, resp.StatusCode, "attach succeeds against a live tmux session")

	var out attachResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	_ = resp.Body.Close()
	return out
}

func (h *terminalHarness) dial(t *testing.T, slug string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, dialResp, err := websocket.Dial(ctx, h.streamURL(slug, testToken, terminalWireVersion), nil)
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// readUntil reads frames until match accepts one, and fails on the deadline.
func readUntil(t *testing.T, conn *websocket.Conn, what string, match func([]byte) bool) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for seen := 0; ; seen++ {
		kind, frame, err := conn.Read(ctx)
		require.NoErrorf(t, err, "waiting for %s after %d frames", what, seen)
		require.Equal(t, websocket.MessageBinary, kind, "the wire is binary only")
		if match(frame) {
			return frame
		}
	}
}

func isLifecycle(frame []byte, kind string) bool {
	if len(frame) == 0 || frame[0] != frameLifecycle {
		return false
	}
	var payload lifecyclePayload
	return json.Unmarshal(frame[1:], &payload) == nil && payload.Kind == kind
}

func isWindowEvent(frame []byte, kind, windowID string) bool {
	if len(frame) == 0 || frame[0] != frameWindowEvent {
		return false
	}
	var payload windowEventPayload
	if json.Unmarshal(frame[1:], &payload) != nil {
		return false
	}
	return payload.Kind == kind && (windowID == "" || payload.WindowID == windowID)
}

func outputContains(t *testing.T, frame []byte, want string) bool {
	t.Helper()
	if len(frame) == 0 || frame[0] != frameOutput {
		return false
	}
	_, _, data, err := decodeOutputFrame(frame)
	require.NoError(t, err)
	return strings.Contains(string(data), want)
}

// Attach is what hands the frontend its initial tab set; the stream carries the
// deltas from there, plus the first paint and the attached lifecycle frame.
func TestTmuxAttachReturnsWindowsAndStreamsEvents(t *testing.T) {
	tmux := startTmux(t, "hive-attach")
	tmux.newWindow("shell")
	h := newTerminalHarness(t)

	attached := h.attach(t, tmux.slug)
	require.Len(t, attached.Windows, 2, "both of the session's windows are reported")
	names := []string{attached.Windows[0].Name, attached.Windows[1].Name}
	assert.ElementsMatch(t, []string{"claude", "shell"}, names)

	// The frontend renders at tmux's size, not the one it voted for, so attach
	// has to hand it that size up front.
	for _, window := range attached.Windows {
		assert.Positive(t, window.Width, "attach reports tmux's own window width")
		assert.Positive(t, window.Height, "attach reports tmux's own window height")
	}

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	// A window created through the control plane surfaces on the stream, which
	// is how the tab bar learns about it.
	resp := h.post(t, "/api/terminal/windows/new", testToken, map[string]any{"slug": tmux.slug})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var created struct {
		WindowID string `json:"windowId"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	_ = resp.Body.Close()
	require.NotEmpty(t, created.WindowID)
	readUntil(t, conn, "an added window event", func(f []byte) bool { return isWindowEvent(f, "added", created.WindowID) })

	rename := h.post(t, "/api/terminal/windows/rename", testToken,
		map[string]any{"slug": tmux.slug, "windowId": created.WindowID, "name": "renamed"})
	_ = rename.Body.Close()
	require.Equal(t, http.StatusNoContent, rename.StatusCode)
	readUntil(t, conn, "a renamed window event", func(f []byte) bool {
		return isWindowEvent(f, "renamed", created.WindowID)
	})

	selected := h.post(t, "/api/terminal/windows/select", testToken,
		map[string]any{"slug": tmux.slug, "windowId": attached.Windows[0].WindowID})
	_ = selected.Body.Close()
	require.Equal(t, http.StatusNoContent, selected.StatusCode)

	closed := h.post(t, "/api/terminal/windows/close", testToken,
		map[string]any{"slug": tmux.slug, "windowId": created.WindowID})
	_ = closed.Body.Close()
	require.Equal(t, http.StatusNoContent, closed.StatusCode)
	readUntil(t, conn, "a closed window event", func(f []byte) bool { return isWindowEvent(f, "closed", created.WindowID) })
}

func TestTmuxOutputFrameCarriesPaneOutput(t *testing.T) {
	tmux := startTmux(t, "hive-output")
	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	tmux.tmux("send-keys", "-t", tmux.slug, "echo HIVE_SENTINEL", "Enter")
	frame := readUntil(t, conn, "the sentinel output frame", func(f []byte) bool {
		return outputContains(t, f, "HIVE_SENTINEL")
	})

	windowID, paneID, _, err := decodeOutputFrame(frame)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(windowID, "@"), "output frames carry the tmux window id")
	assert.True(t, strings.HasPrefix(paneID, "%"), "and the pane id the panes phase will route on")
}

// Input rides the WebSocket rather than REST, so this is the only proof that
// keystrokes reach the pane at all.
func TestTmuxInputFrameReachesThePane(t *testing.T) {
	tmux := startTmux(t, "hive-input")
	h := newTerminalHarness(t)
	attached := h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	frame := encodeInputFrame(attached.Windows[0].WindowID, []byte("echo HELLO_FROM_WS\r"))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, frame))

	tmux.awaitPane(tmux.slug, "HELLO_FROM_WS")
}

// The attach size is a vote tmux obeys, so a caller with nothing measured must
// send none: a placeholder would resize the session — and every agent redrawing
// inside it — to a size nobody asked for.
func TestTmuxUnsizedAttachLeavesTheSessionAtItsOwnSize(t *testing.T) {
	tmux := startTmux(t, "hive-unsized")
	h := newTerminalHarness(t)

	resp := h.post(t, "/api/terminal/attach", testToken, map[string]any{"slug": tmux.slug, "cols": 0, "rows": 0})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var attached attachResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&attached))
	_ = resp.Body.Close()

	require.Len(t, attached.Windows, 1)
	assert.Equal(t, 120, attached.Windows[0].Width, "the fixture's 120x40 survives the attach")
	assert.Equal(t, 40, attached.Windows[0].Height)
	assert.Equal(t, "120x40", tmux.windowSize())

	// The first measured vote is what joins this client to the negotiation.
	resize := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": tmux.slug, "cols": 100, "rows": 30})
	_ = resize.Body.Close()
	require.Equal(t, http.StatusNoContent, resize.StatusCode)
	assert.Equal(t, "100x30", tmux.windowSize())
}

func TestTmuxResizeAndDetachLeaveTheSessionRunning(t *testing.T) {
	tmux := startTmux(t, "hive-detach")
	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	resize := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": tmux.slug, "cols": 100, "rows": 30})
	_ = resize.Body.Close()
	assert.Equal(t, http.StatusNoContent, resize.StatusCode)

	// The vote is not the answer: tmux decides the window size and says so with
	// %layout-change, and that is what the renderer follows.
	frame := readUntil(t, conn, "a resized window event", func(f []byte) bool { return isWindowEvent(f, "resized", "") })
	var resized windowEventPayload
	require.NoError(t, json.Unmarshal(frame[1:], &resized))
	assert.Equal(t, 100, resized.Width, "tmux honoured the only attached client's size")
	assert.Positive(t, resized.Height)

	detach := h.post(t, "/api/terminal/detach", testToken, map[string]any{"slug": tmux.slug})
	_ = detach.Body.Close()
	require.Equal(t, http.StatusNoContent, detach.StatusCode)

	// Detach closes the subscription, which closes the socket underneath it.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			break
		}
	}

	out, err := exec.CommandContext(t.Context(), "tmux", "has-session", "-t", tmux.slug).CombinedOutput()
	require.NoErrorf(t, err, "detaching the control client leaves the tmux session alive: %s", out)
}

// Two sessions on one server: a control client must see only its own.
func TestTmuxAttachIsScopedToItsSession(t *testing.T) {
	tmux := startTmux(t, "hive-scope-one")
	tmux.tmux("-f", "/dev/null", "new-session", "-d", "-s", "hive-scope-two", "-n", "other", "sh")
	h := newTerminalHarness(t)

	attached := h.attach(t, tmux.slug)
	require.Len(t, attached.Windows, 1)
	assert.Equal(t, "claude", attached.Windows[0].Name, "the window set is the attached session's")

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	// The other session's output must not appear on this stream, so it is
	// written first and the attached session's sentinel is what we wait for: by
	// the time that arrives, the other session's would have too.
	tmux.tmux("send-keys", "-t", "hive-scope-two", "echo OTHER_SESSION_MARK", "Enter")
	tmux.awaitPane("hive-scope-two", "OTHER_SESSION_MARK")
	tmux.tmux("send-keys", "-t", tmux.slug, "echo OWN_SESSION_MARK", "Enter")

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for {
		_, frame, err := conn.Read(ctx)
		require.NoError(t, err)
		require.False(t, outputContains(t, frame, "OTHER_SESSION_MARK"), "the stream is scoped to its own session")
		if outputContains(t, frame, "OWN_SESSION_MARK") {
			return
		}
	}
}

// v1 renders one pane per window; a background pane is drained so tmux never
// stalls on us, but its bytes are neither forwarded nor charged to the buffer.
func TestTmuxInactivePaneIsDrainedNotForwarded(t *testing.T) {
	tmux := startTmux(t, "hive-panes")
	h := newTerminalHarness(t)

	active := strings.TrimSpace(tmux.tmux("display-message", "-p", "-t", tmux.slug, "#{pane_id}"))
	tmux.tmux("split-window", "-t", tmux.slug, "sh")
	inactive := strings.TrimSpace(tmux.tmux("display-message", "-p", "-t", tmux.slug, "#{pane_id}"))
	require.NotEqual(t, active, inactive)
	tmux.tmux("select-pane", "-t", active)

	h.attach(t, tmux.slug)
	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	tmux.tmux("send-keys", "-t", inactive, "yes background-flood | head -n 20000", "Enter")
	tmux.tmux("send-keys", "-t", active, "echo ACTIVE_PANE_MARK", "Enter")

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for {
		_, frame, err := conn.Read(ctx)
		require.NoError(t, err, "the stream stays alive through the flood")
		if frame[0] == frameOutput {
			_, paneID, data, decodeErr := decodeOutputFrame(frame)
			require.NoError(t, decodeErr)
			assert.Equal(t, active, paneID, "only the active pane is forwarded")
			require.NotContains(t, string(data), "background-flood")
			if strings.Contains(string(data), "ACTIVE_PANE_MARK") {
				return
			}
		}
	}
}

// Overflow is fatal by design: a terminal stream cannot drop-oldest, so the
// client is torn down and the frontend re-attaches for a clean resync. Reading
// nothing at all is what fills the 8 MiB buffer — a reader that merely dawdles
// does not, because the kernel's socket buffers absorb megabytes before the
// broker's backlog grows at all, and every one of those bytes is then queued
// ahead of the exit frame.
func TestTmuxBrokerOverflowEndsTheStream(t *testing.T) {
	tmux := startTmux(t, "hive-overflow")
	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	tmux.tmux("send-keys", "-t", tmux.slug,
		"yes 0123456789abcdef0123456789abcdef0123456789abcdef | head -n 400000", "Enter")

	awaitClientTornDown(t, h, tmux.slug)

	// Still not reading. The broker has published the exit reason and is offering
	// it to a write pump parked inside a congested socket write; staying away a
	// while longer is what pins that offer outliving a subscriber that is behind
	// — which, after an overflow, it is by definition.
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	sawOverflow := false
	for {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if isLifecycle(frame, "exited") {
			var payload lifecyclePayload
			require.NoError(t, json.Unmarshal(frame[1:], &payload))
			assert.Equal(t, "overflow", payload.Message, "the exit reason reaches the frontend")
			sawOverflow = true
		}
	}
	require.True(t, sawOverflow, "an EXITED(overflow) frame arrived before the socket closed")

	// The slug is free again: the frontend's reconnect gets a fresh client.
	resize := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": tmux.slug, "cols": 80, "rows": 24})
	_ = resize.Body.Close()
	assert.Equal(t, http.StatusNotFound, resize.StatusCode)
}

// awaitClientTornDown blocks — without reading the socket, which is the point —
// until the control plane stops knowing slug. That 404 is the observable trailing
// edge of teardown: the manager drops the slug only after the broker has
// published the exit reason and closed.
func awaitClientTornDown(t *testing.T, h *terminalHarness, slug string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": slug, "cols": 80, "rows": 24})
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return
		}
		require.True(t, time.Now().Before(deadline), "the flood never overflowed the broker")
		time.Sleep(50 * time.Millisecond)
	}
}

// Shutdown rides the subscription channel: App.Close stops the manager, which
// closes the broker, which ends the write pump, which closes the socket the
// read pump is blocked on. Nothing here is bound to the HTTP server's own
// shutdown, which does not track a hijacked connection.
func TestTmuxAppCloseEndsTheStreamWithoutLeakingGoroutines(t *testing.T) {
	tmux := startTmux(t, "hive-shutdown")

	settleGoroutines(t)
	before := runtime.NumGoroutine()

	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)
	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	require.NoError(t, h.core.Close())

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	_ = conn.CloseNow()
	h.server.Close()

	// A plain loop, not assert.Eventually: that helper runs its condition on a
	// goroutine of its own, which the count would include.
	after := runtime.NumGoroutine()
	for range 500 {
		if after <= before {
			break
		}
		time.Sleep(20 * time.Millisecond)
		after = runtime.NumGoroutine()
	}
	if after > before {
		buf := make([]byte, 1<<20)
		t.Log(string(buf[:runtime.Stack(buf, true)]))
	}
	assert.LessOrEqual(t, after, before, "the terminal stream leaked a goroutine")
}

// An oversized input frame is a protocol violation, not something to truncate.
func TestTmuxStreamRejectsOversizedInputFrames(t *testing.T) {
	tmux := startTmux(t, "hive-inputcap")
	h := newTerminalHarness(t)
	attached := h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	oversized := encodeInputFrame(attached.Windows[0].WindowID, []byte(strings.Repeat("x", maxInputFrameBytes+1)))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, oversized))

	for {
		if _, _, err := conn.Read(ctx); err != nil {
			require.Equal(t, websocket.StatusMessageTooBig, websocket.CloseStatus(err),
				"a frame over the cap closes the stream")
			break
		}
	}
}

// settleGoroutines waits for goroutines left by earlier tests to exit, so the
// baseline belongs to this test.
func settleGoroutines(t *testing.T) {
	t.Helper()
	baseline := runtime.NumGoroutine()
	for range 50 {
		time.Sleep(10 * time.Millisecond)
		current := runtime.NumGoroutine()
		if current >= baseline {
			return
		}
		baseline = current
	}
}
