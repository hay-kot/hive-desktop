package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/tmuxtest"
)

// These tests drive a real tmux server. Each one gets a server of its own and
// its own session name, so they never see each other's. See internal/tmuxtest
// for how they are kept away from a developer's own.

type tmuxFixture struct {
	t      *testing.T
	slug   string
	socket string
}

// startTmux boots a private tmux server holding one detached session named
// slug, with a single `sh` window called claude.
func startTmux(t *testing.T, slug string) *tmuxFixture {
	t.Helper()

	fixture := &tmuxFixture{t: t, slug: slug, socket: tmuxtest.Private(t)}
	fixture.tmux("-f", "/dev/null", "new-session", "-d", "-s", slug, "-n", "claude", "-x", "120", "-y", "40", "sh")
	return fixture
}

func (f *tmuxFixture) tmux(args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), "tmux", append([]string{"-S", f.socket}, args...)...)
	cmd.Env = tmuxtest.ScrubbedEnv()
	out, err := cmd.CombinedOutput()
	require.NoErrorf(f.t, err, "tmux %v: %s", args, out)
	return string(out)
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

type terminalWindowResult struct {
	WindowID string `json:"windowId"`
	Name     string `json:"name"`
	Active   bool   `json:"active"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type attachResult struct {
	Windows []terminalWindowResult `json:"windows"`
}

type sessionWindowsResult struct {
	Sessions map[string][]terminalWindowResult `json:"sessions"`
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
	// The webview the app actually runs in imposes no read limit; this client
	// defaults to 32 KiB, under half of one maximally coalesced output frame. A
	// subscriber that falls behind is exactly when the broker merges hardest, so
	// keeping the default would have the test kill its own connection mid-flood
	// and read none of the frames after it.
	conn.SetReadLimit(-1)
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

// A session tmux is not running answers 404 rather than tmux's own complaint
// dressed as an internal fault: that is the answer the terminal view turns into
// its "start this session" panel, so it has to be classified, not narrated.
func TestTmuxAttachReportsASessionThatIsNotRunning(t *testing.T) {
	startTmux(t, "hive-known")
	h := newTerminalHarness(t)

	resp := h.post(t, "/api/terminal/attach", testToken, map[string]any{"slug": "hive-never-spawned", "cols": 120, "rows": 40})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	var failure struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&failure))
	_ = resp.Body.Close()
	assert.Equal(t, "not_found", failure.Kind)
	assert.Contains(t, failure.Message, "hive-never-spawned")

	// Starting it is the caller's next move — and with no hive session behind
	// the slug there is nothing to build one from, which is its own 404.
	start := h.post(t, "/api/terminal/start", testToken, map[string]any{"slug": "hive-never-spawned"})
	assert.Equal(t, http.StatusNotFound, start.StatusCode)
	_ = start.Body.Close()
}

// A stream that drops without a detach — a stalled write, a reloaded webview —
// leaves the control client attached to a session that never stopped. Reconnect
// re-attaches onto that live client and builds fresh emulators, so the attach
// owes it a first paint: what the dropped socket already rendered is gone from
// the backlog, and everything the pane shows was written before the new stream
// existed.
func TestTmuxReattachAfterATransportDropRepaints(t *testing.T) {
	tmux := startTmux(t, "hive-redrop")
	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	tmux.tmux("send-keys", "-t", tmux.slug, "echo HIVE_BEFORE_DROP", "Enter")
	tmux.awaitPane(tmux.slug, "HIVE_BEFORE_DROP")
	readUntil(t, conn, "the marker on the live stream", func(f []byte) bool {
		return outputContains(t, f, "HIVE_BEFORE_DROP")
	})

	// No detach: the control client outlives this socket, which is the whole
	// point of the case.
	require.NoError(t, conn.CloseNow())

	reattached := h.attach(t, tmux.slug)
	require.Len(t, reattached.Windows, 1)

	resumed := h.dial(t, tmux.slug)
	readUntil(t, resumed, "the pane repainted onto the new stream", func(f []byte) bool {
		return outputContains(t, f, "HIVE_BEFORE_DROP")
	})
}

// Killing is the terminal's lifecycle alone: tmux loses the session, and the
// attach that follows is the not-running answer the start panel is built on.
func TestTmuxKillEndsTheSession(t *testing.T) {
	tmux := startTmux(t, "hive-doomed")
	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)

	resp := h.post(t, "/api/terminal/kill", testToken, map[string]any{"slug": tmux.slug})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Killed bool `json:"killed"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	_ = resp.Body.Close()
	assert.True(t, body.Killed)

	attach := h.post(t, "/api/terminal/attach", testToken, map[string]any{"slug": tmux.slug, "cols": 120, "rows": 40})
	_ = attach.Body.Close()
	assert.Equal(t, http.StatusNotFound, attach.StatusCode)

	again := h.post(t, "/api/terminal/kill", testToken, map[string]any{"slug": tmux.slug})
	require.Equal(t, http.StatusOK, again.StatusCode, "killing what is already gone is nothing to do")
	require.NoError(t, json.NewDecoder(again.Body).Decode(&body))
	_ = again.Body.Close()
	assert.False(t, body.Killed)
}

// A slug tmux is already running needs no start, and must not be respawned over.
func TestTmuxStartIsANoOpForALiveSession(t *testing.T) {
	tmux := startTmux(t, "hive-running")
	h := newTerminalHarness(t)

	resp := h.post(t, "/api/terminal/start", testToken, map[string]any{"slug": tmux.slug})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Started bool `json:"started"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	_ = resp.Body.Close()
	assert.False(t, body.Started, "the session was already there")
}

// The sidebar's "always show windows" option lists windows for sessions this
// webview is not attached to, so the listing must work with no control client.
// It answers the whole sidebar at once: per slug, every unattached session cost
// a has-session probe and a list-windows, so the sweep spawned two tmux
// processes per row.
func TestTmuxListWindowsAnswersWithoutAnAttach(t *testing.T) {
	tmux := startTmux(t, "hive-list")
	tmux.newWindow("shell")
	h := newTerminalHarness(t)

	resp := h.post(t, "/api/terminal/windows/list", testToken,
		map[string]any{"slugs": []string{tmux.slug, "hive-never-spawned"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out sessionWindowsResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	_ = resp.Body.Close()

	listed := out.Sessions[tmux.slug]
	require.Len(t, listed, 2)
	assert.ElementsMatch(t, []string{"claude", "shell"}, []string{listed[0].Name, listed[1].Name})

	// A hive session with no tmux session behind it is a normal state for the
	// sidebar, so it is simply absent rather than an error.
	assert.NotContains(t, out.Sessions, "hive-never-spawned")
}

// The move is the one control-plane operation whose reply carries a whole
// window set: the order is tmux's, so the caller renders what tmux settled on
// rather than the order it asked for. The pane must survive the round trip —
// tmux reports a moved window as closed, and a torn-down tab would come back
// blank and unselected.
func TestTmuxMoveWindowReordersWithoutDisturbingThePane(t *testing.T) {
	tmux := startTmux(t, "hive-move")
	tmux.newWindow("shell")
	tmux.newWindow("logs")
	h := newTerminalHarness(t)

	attached := h.attach(t, tmux.slug)
	require.Len(t, attached.Windows, 3)
	require.Equal(t, []string{"claude", "shell", "logs"}, windowNames(attached))
	first := attached.Windows[0].WindowID

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	moved := h.move(t, tmux.slug, first, 2)
	assert.Equal(t, []string{"shell", "logs", "claude"}, windowNames(moved))
	assert.Equal(t, "logs", activeName(t, moved), "a reorder is not a selection")
	assert.Equal(t, "0 1 2", strings.Join(strings.Fields(tmux.tmux("list-windows", "-t", tmux.slug, "-F", "#{window_index}")), " "),
		"the insert's index gaps are renumbered away")

	// The moved window is still live: no close reached the stream, and its pane
	// still carries output on the same window id.
	tmux.tmux("send-keys", "-t", first, "echo HIVE_STILL_HERE", "Enter")
	frame := readUntil(t, conn, "output from the moved window", func(f []byte) bool {
		return outputContains(t, f, "HIVE_STILL_HERE")
	})
	windowID, _, _, err := decodeOutputFrame(frame)
	require.NoError(t, err)
	assert.Equal(t, first, windowID)
}

func TestTmuxMoveWindowRejectsAPositionThatIsNotThere(t *testing.T) {
	tmux := startTmux(t, "hive-move-bad")
	h := newTerminalHarness(t)
	attached := h.attach(t, tmux.slug)

	resp := h.post(t, "/api/terminal/windows/move", testToken,
		map[string]any{"slug": tmux.slug, "windowId": attached.Windows[0].WindowID, "position": 4})
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = h.post(t, "/api/terminal/windows/move", testToken,
		map[string]any{"slug": tmux.slug, "windowId": "@404", "position": 0})
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// A negative index never reaches the core: the request rejects it.
	resp = h.post(t, "/api/terminal/windows/move", testToken,
		map[string]any{"slug": tmux.slug, "windowId": attached.Windows[0].WindowID, "position": -1})
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func (h *terminalHarness) move(t *testing.T, slug, windowID string, position int) attachResult {
	t.Helper()
	resp := h.post(t, "/api/terminal/windows/move", testToken,
		map[string]any{"slug": slug, "windowId": windowID, "position": position})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out attachResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	_ = resp.Body.Close()
	return out
}

func windowNames(result attachResult) []string {
	names := make([]string, 0, len(result.Windows))
	for _, w := range result.Windows {
		names = append(names, w.Name)
	}
	return names
}

func activeName(t *testing.T, result attachResult) string {
	t.Helper()
	for _, w := range result.Windows {
		if w.Active {
			return w.Name
		}
	}
	t.Fatalf("no active window in %#v", result.Windows)
	return ""
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

// Overflow resyncs first and only ends the stream when that does not hold, and
// this exercises both against real tmux. Reading nothing at all is what fills
// the 8 MiB buffer — a reader that merely dawdles does not, because the kernel's
// socket buffers absorb megabytes before the broker's backlog grows at all — and
// it is also what guarantees the second half: a subscriber taking nothing cannot
// be rescued by a repaint, so the flood refills the backlog inside the resync
// cooldown and the client falls back to the exit it always took.
func TestTmuxBrokerOverflowResyncsThenEndsTheStream(t *testing.T) {
	tmux := startTmux(t, "hive-overflow")
	h := newTerminalHarness(t)
	h.attach(t, tmux.slug)

	conn := h.dial(t, tmux.slug)
	readUntil(t, conn, "the attached lifecycle frame", func(f []byte) bool { return isLifecycle(f, "attached") })

	// Sized to cross the 8 MiB bound several times over: the first crossing is
	// answered by a resync, and what ends the stream is the one after it.
	tmux.tmux("send-keys", "-t", tmux.slug,
		"yes 0123456789abcdef0123456789abcdef0123456789abcdef | head -n 1000000", "Enter")

	awaitClientTornDown(t, h, tmux.slug)

	// Still not reading. The broker has published the exit reason and is offering
	// it to a write pump parked inside a congested socket write; staying away a
	// while longer is what pins that offer outliving a subscriber that is behind
	// — which, after an overflow, it is by definition.
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	sawDegraded, sawOverflow := false, false
	for {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if isLifecycle(frame, "degraded") {
			sawDegraded = true
		}
		if isLifecycle(frame, "exited") {
			var payload lifecyclePayload
			require.NoError(t, json.Unmarshal(frame[1:], &payload))
			assert.Equal(t, "overflow", payload.Message, "the exit reason reaches the frontend")
			sawOverflow = true
		}
	}
	require.True(t, sawDegraded, "a DEGRADED frame named the gap before the stream gave up on it")
	require.True(t, sawOverflow, "an EXITED(overflow) frame arrived before the socket closed")

	// The slug is free again: the frontend's reconnect gets a fresh client.
	resize := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": tmux.slug, "cols": 80, "rows": 24})
	_ = resize.Body.Close()
	assert.Equal(t, http.StatusNotFound, resize.StatusCode)
}

// awaitClientTornDown blocks — without reading the socket, which is the point —
// until the control plane stops knowing slug. That 404 is the observable trailing
// edge of teardown: the manager drops the slug only after the broker has
// published the exit reason and closed. It outlasts the resync the first
// overflow spends, so the deadline covers a recovery attempt as well as the
// flood behind it.
func awaitClientTornDown(t *testing.T, h *terminalHarness, slug string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for {
		resp := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": slug, "cols": 80, "rows": 24})
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return
		}
		require.True(t, time.Now().Before(deadline), "the flood never outran the resync")
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

// trailingCursor matches the cursor position a first paint ends with.
var trailingCursor = regexp.MustCompile(`\x1b\[(\d+);(\d+)H$`)

// paintedRows splits a first paint into the rows it wrote and the cursor it
// ended on, which is the shape every alignment claim below is made against.
func paintedRows(t *testing.T, painted string) ([]string, string) {
	t.Helper()
	cursor := trailingCursor.FindString(painted)
	require.NotEmpty(t, cursor, "a first paint ends by restoring tmux's cursor: %q", painted)
	return strings.Split(strings.TrimSuffix(painted, cursor), "\r\n"), cursor
}

// scrollPane fills a pane past its own height so the early lines are in tmux's
// history rather than on the visible screen, and returns a line that is.
func scrollPane(f *tmuxFixture, target string) string {
	f.t.Helper()
	f.tmux("send-keys", "-t", target, `for i in $(seq 1 80); do echo "scrollback-$i"; done`, "Enter")
	f.awaitPane(target, "scrollback-80")
	return "scrollback-1"
}

// The whole point of attaching to a session that has been running for hours is
// seeing what it did before you got there. First paint carries tmux's own
// scrollback so the tab opens with history behind it, not just the screen.
func TestTmuxFirstPaintReplaysScrollback(t *testing.T) {
	tmux := startTmux(t, "hive-scrollback")
	h := newTerminalHarness(t)

	scrolledAway := scrollPane(tmux, tmux.slug)
	require.NotContains(t, tmux.tmux("capture-pane", "-p", "-t", tmux.slug), scrolledAway,
		"the fixture only proves anything if that line really has left the screen")

	h.attach(t, tmux.slug)
	conn := h.dial(t, tmux.slug)
	frame := readUntil(t, conn, "the first paint", func(f []byte) bool { return outputContains(t, f, scrolledAway) })

	_, _, data, err := decodeOutputFrame(frame)
	require.NoError(t, err)
	rows, _ := paintedRows(t, string(data))
	require.Greater(t, len(rows), 40, "history is painted above the screen, so the paint outgrows the grid")
	require.Contains(t, strings.Join(rows[:len(rows)-40], "\r\n"), scrolledAway,
		"a line that scrolled away is painted into the scrollback, not into the screen")
}

// A pane whose program is on the alternate screen still has scrollback in
// tmux's normal buffer, and the paint has to carry both: the history above, and
// the alternate screen filling the grid exactly. Painted short, the emulator
// would seat the pane's top row partway down its viewport, and every
// cursor-addressed redraw the program made afterwards would land rows off.
func TestTmuxFirstPaintAlignsAnAlternateScreenPane(t *testing.T) {
	tmux := startTmux(t, "hive-altscreen")
	h := newTerminalHarness(t)

	scrolledAway := scrollPane(tmux, tmux.slug)
	tmux.tmux("send-keys", "-t", tmux.slug, `printf '\033[?1049h\033[H\033[2JALT-TOP-ROW\n'`, "Enter")
	tmux.awaitPane(tmux.slug, "ALT-TOP-ROW")

	h.attach(t, tmux.slug)
	conn := h.dial(t, tmux.slug)
	frame := readUntil(t, conn, "the first paint", func(f []byte) bool { return outputContains(t, f, "ALT-TOP-ROW") })

	_, _, data, err := decodeOutputFrame(frame)
	require.NoError(t, err)
	rows, _ := paintedRows(t, string(data))
	require.Greater(t, len(rows), 40)

	screen := rows[len(rows)-40:]
	assert.Contains(t, screen[0], "ALT-TOP-ROW", "the alternate screen's own first row is the grid's first row")
	assert.Contains(t, strings.Join(rows[:len(rows)-40], "\r\n"), scrolledAway,
		"the normal buffer's history is still reachable above an alternate screen")
}
