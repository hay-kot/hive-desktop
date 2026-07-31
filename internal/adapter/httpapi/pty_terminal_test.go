package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

// ptyStreamURL is streamURL against the process-managed mount.
func (h *terminalHarness) ptyStreamURL(slug, token, version string) string {
	query := url.Values{}
	query.Set("slug", slug)
	query.Set("token", token)
	query.Set("v", version)
	return "ws" + h.server.URL[len("http"):] + PtyTerminalStreamPath + "?" + query.Encode()
}

// The pty routes sit under the terminal prefix so they inherit its bearer-token
// gate rather than declaring a second one. A regression here would leave
// arbitrary command execution unauthenticated (ADR 0036).
func TestPtyTerminalControlPlaneRequiresTheBearerToken(t *testing.T) {
	h := newTerminalHarness(t)
	body := map[string]any{"slug": "not-attached"}

	resp := h.post(t, PtyTerminalPathPrefix+"detach", "", body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no Authorization header is rejected")

	resp = h.post(t, PtyTerminalPathPrefix+"detach", "wrong-token", body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "a wrong token is rejected")

	resp = h.post(t, PtyTerminalPathPrefix+"detach", testToken, body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "the right token is served")
}

func TestPtyTerminalControlPlaneAnswersPreflight(t *testing.T) {
	h := newTerminalHarness(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, h.server.URL+PtyTerminalPathPrefix+"attach", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, testOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
}

// Attaching never spawns on this backend either, even though a shell costs far
// less than a tmux session with an agent in it: both engines answer a slug with
// no session the same way, so one frontend handles both (ADR 0044).
func TestPtyTerminalAttachNeverSpawns(t *testing.T) {
	h := newTerminalHarness(t)

	resp := h.post(t, PtyTerminalPathPrefix+"attach", testToken, map[string]any{"slug": "nothing-here", "cols": 80, "rows": 24})
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// A slug with no session lists no windows rather than failing: callers
	// enumerate every session the app knows about.
	windows := h.post(t, PtyTerminalPathPrefix+"windows/list", testToken, map[string]any{"slug": "nothing-here"})
	defer func() { _ = windows.Body.Close() }()
	assert.Equal(t, http.StatusOK, windows.StatusCode)

	var body terminalWindowsResponse
	require.NoError(t, json.NewDecoder(windows.Body).Decode(&body))
	assert.Empty(t, body.Windows)
}

func TestPtyTerminalSizeValidationMatchesTheTmuxBackend(t *testing.T) {
	h := newTerminalHarness(t)

	half := h.post(t, PtyTerminalPathPrefix+"attach", testToken, map[string]any{"slug": "hive-x", "cols": 0, "rows": 24})
	_ = half.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, half.StatusCode, "half a measurement is rejected")

	unsizedResize := h.post(t, PtyTerminalPathPrefix+"resize", testToken, map[string]any{"slug": "hive-x", "cols": 0, "rows": 0})
	_ = unsizedResize.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, unsizedResize.StatusCode, "a resize always carries a size")
}

func TestPtyTerminalStreamRejectsBadHandshakes(t *testing.T) {
	h := newTerminalHarness(t)

	dial := func(t *testing.T, target string) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, target, nil) //nolint:bodyclose // closed below
		if conn != nil {
			_ = conn.CloseNow()
		}
		if resp != nil {
			defer func() { _ = resp.Body.Close() }()
			return resp.StatusCode
		}
		require.Error(t, err)
		return 0
	}

	assert.Equal(t, http.StatusBadRequest, dial(t, h.ptyStreamURL("x", testToken, "999")), "a stale wire version fails before the upgrade")
	assert.Equal(t, http.StatusUnauthorized, dial(t, h.ptyStreamURL("x", "wrong", terminalWireVersion)), "a wrong token fails before the upgrade")
	assert.Equal(t, http.StatusNotFound, dial(t, h.ptyStreamURL("no-such-session", testToken, terminalWireVersion)), "a slug with no session fails before the upgrade")
}

// The two encoders must agree byte for byte: the renderer is told nothing about
// which backend it is attached to, so a frame that differs is a rendering bug
// that only shows on one engine (ADR 0045).
func TestPtyFramesMatchTheTmuxWire(t *testing.T) {
	output, ok := encodePtyEvent(ptyterm.Output{WindowID: "w7", Data: []byte("hello")})
	require.True(t, ok)

	windowID, paneID, data, err := decodeOutputFrame(output)
	require.NoError(t, err)
	assert.Equal(t, "w7", windowID)
	assert.Equal(t, "w7", paneID, "a PTY has no panes, so the window id stands in for one")
	assert.Equal(t, "hello", string(data))

	window, ok := encodePtyEvent(ptyterm.WindowChanged{
		Kind:   ptyterm.WindowRenamed,
		Window: ptyterm.Window{ID: "w2", Name: "build", Active: true, Width: 120, Height: 40},
	})
	require.True(t, ok)
	require.Equal(t, frameWindowEvent, window[0])

	var windowPayload windowEventPayload
	require.NoError(t, json.Unmarshal(window[1:], &windowPayload))
	assert.Equal(t, windowEventPayload{Kind: "renamed", WindowID: "w2", Name: "build", Active: true, Width: 120, Height: 40}, windowPayload)

	lifecycle, ok := encodePtyEvent(ptyterm.LifecycleChanged{Kind: ptyterm.LifecycleExited, WindowID: "w2", Message: "exited with status 0"})
	require.True(t, ok)
	require.Equal(t, frameLifecycle, lifecycle[0])

	var lifecyclePayloadDecoded lifecyclePayload
	require.NoError(t, json.Unmarshal(lifecycle[1:], &lifecyclePayloadDecoded))
	assert.Equal(t, lifecyclePayload{Kind: "exited", WindowID: "w2", Message: "exited with status 0"}, lifecyclePayloadDecoded)
}
