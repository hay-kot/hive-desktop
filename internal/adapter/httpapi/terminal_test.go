package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

const (
	testToken  = "test-terminal-token"
	testOrigin = "wails://localhost"
)

// terminalHarness is main.go's mount shape: the control plane under /api/ and
// the stream as its own raw mount, both over one server.
type terminalHarness struct {
	core   *app.App
	server *httptest.Server
}

func newTerminalHarness(t *testing.T) *terminalHarness {
	t.Helper()
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, "feed")

	core, err := app.New(t.Context(), app.Config{
		Settings: settings.DefaultSettings(),
		MockMode: settings.MockMode(),
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)

	origins := []string{testOrigin}
	mux := http.NewServeMux()
	mux.Handle(PathPrefix, New(core, zerolog.Nop(), testToken, origins).Handler())
	streamPath, stream := TerminalStreamHandler(core, testToken, origins, zerolog.Nop())
	mux.Handle(streamPath, stream)

	h := &terminalHarness{core: core, server: httptest.NewServer(mux)}
	t.Cleanup(func() {
		h.server.Close()
		_ = core.Close()
	})
	return h
}

func (h *terminalHarness) post(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.server.URL+path, bytes.NewReader(encoded))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.server.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// streamURL builds the data-plane URL with the pieces a caller wants to break.
func (h *terminalHarness) streamURL(slug, token, version string) string {
	query := url.Values{}
	query.Set("slug", slug)
	query.Set("token", token)
	query.Set("v", version)
	return "ws" + h.server.URL[len("http"):] + TerminalStreamPath + "?" + query.Encode()
}

func TestTerminalControlPlaneRequiresTheBearerToken(t *testing.T) {
	h := newTerminalHarness(t)

	// Detaching a slug nothing is attached to succeeds, so this exercises the
	// token gate alone rather than tmux availability.
	body := map[string]any{"slug": "not-attached"}

	resp := h.post(t, "/api/terminal/detach", "", body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no Authorization header is rejected")

	resp = h.post(t, "/api/terminal/detach", "wrong-token", body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "a wrong token is rejected")

	resp = h.post(t, "/api/terminal/detach", testToken, body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "the right token is served")
}

// 0x0 is the whole unsized case — "nothing measured yet". One measured
// dimension without the other is a caller bug, and a resize is never unsized.
func TestTerminalSizeValidationSeparatesAttachFromResize(t *testing.T) {
	h := newTerminalHarness(t)

	half := h.post(t, "/api/terminal/attach", testToken, map[string]any{"slug": "hive-x", "cols": 0, "rows": 24})
	_ = half.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, half.StatusCode, "half a measurement is rejected")

	unsizedResize := h.post(t, "/api/terminal/resize", testToken, map[string]any{"slug": "hive-x", "cols": 0, "rows": 0})
	_ = unsizedResize.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, unsizedResize.StatusCode, "a resize always carries a size")
}

func TestTerminalControlPlaneAnswersPreflight(t *testing.T) {
	h := newTerminalHarness(t)

	preflight := func(t *testing.T, origin string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, h.server.URL+"/api/terminal/attach", nil)
		require.NoError(t, err)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := h.server.Client().Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	allowed := preflight(t, testOrigin)
	_ = allowed.Body.Close()
	assert.Equal(t, http.StatusNoContent, allowed.StatusCode)
	assert.Equal(t, testOrigin, allowed.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, allowed.Header.Get("Access-Control-Allow-Methods"), "POST")
	assert.Contains(t, allowed.Header.Get("Access-Control-Allow-Headers"), "Authorization")

	// macOS appends the dev server's per-run port to the webview origin, so the
	// wails scheme matches structurally, not against the list.
	ported := preflight(t, "wails://localhost:53579")
	_ = ported.Body.Close()
	assert.Equal(t, http.StatusNoContent, ported.StatusCode)
	assert.Equal(t, "wails://localhost:53579", ported.Header.Get("Access-Control-Allow-Origin"))

	denied := preflight(t, "https://evil.example")
	_ = denied.Body.Close()
	assert.Equal(t, http.StatusForbidden, denied.StatusCode)
	assert.Empty(t, denied.Header.Get("Access-Control-Allow-Origin"))
}

// The stream rejects before upgrading, so every failure is a plain HTTP status
// the frontend can read rather than a close code it cannot.
func TestTerminalStreamRejectsBadHandshakes(t *testing.T) {
	h := newTerminalHarness(t)

	dial := func(t *testing.T, target string, header http.Header) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPHeader: header})
		if conn != nil {
			_ = conn.CloseNow()
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err, "the handshake must not succeed")
		require.NotNil(t, resp)
		return resp.StatusCode
	}

	assert.Equal(t, http.StatusBadRequest, dial(t, h.streamURL("any", testToken, "2"), nil),
		"an unknown wire version is refused")
	assert.Equal(t, http.StatusUnauthorized, dial(t, h.streamURL("any", "wrong-token", "1"), nil),
		"a wrong token is refused")
	assert.Equal(t, http.StatusUnauthorized, dial(t, h.streamURL("any", "", "1"), nil),
		"a missing token is refused")
	assert.Equal(t, http.StatusForbidden,
		dial(t, h.streamURL("any", testToken, "1"), http.Header{"Origin": []string{"https://evil.example"}}),
		"an origin outside the allowlist is refused")
	assert.Equal(t, http.StatusNotFound, dial(t, h.streamURL("not-attached", testToken, "1"), nil),
		"a slug with no attached client is a 404, not an upgrade")
}

func TestTerminalFramesRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte{0x1b, '[', '1', 'm', 'h', 'i', 0x00, 0xff}

	windowID, paneID, data, err := decodeOutputFrame(encodeOutputFrame("@12", "%34", raw))
	require.NoError(t, err)
	assert.Equal(t, "@12", windowID)
	assert.Equal(t, "%34", paneID)
	assert.Equal(t, raw, data)

	inputWindow, inputData, err := decodeInputFrame(encodeInputFrame("@12", raw))
	require.NoError(t, err)
	assert.Equal(t, "@12", inputWindow)
	assert.Equal(t, raw, inputData)

	_, _, err = decodeInputFrame([]byte{frameOutput, 0x00})
	require.Error(t, err, "an output frame is not input")
	_, _, err = decodeInputFrame([]byte{frameInput, 0x04, '@', '1'})
	require.Error(t, err, "a truncated id is refused")
	_, _, err = decodeInputFrame([]byte{frameInput, 0x00})
	require.Error(t, err, "an empty window id is refused")
}

// The wire kinds are tmuxcc's own strings; a rename here would silently break
// the frontend's tab handling.
func TestTerminalControlFramesCarryStringKinds(t *testing.T) {
	t.Parallel()

	frame, ok := encodeEvent(tmuxcc.LifecycleChanged{Kind: tmuxcc.LifecycleExited, Message: "overflow"})
	require.True(t, ok)
	require.Equal(t, frameLifecycle, frame[0])

	var lifecycle lifecyclePayload
	require.NoError(t, json.Unmarshal(frame[1:], &lifecycle))
	assert.Equal(t, "exited", lifecycle.Kind)
	assert.Equal(t, "overflow", lifecycle.Message)

	frame, ok = encodeEvent(tmuxcc.WindowChanged{
		Kind:   tmuxcc.WindowRenamed,
		Window: tmuxcc.Window{ID: "@3", Name: "shell", Active: true, Width: 213, Height: 55},
	})
	require.True(t, ok)
	require.Equal(t, frameWindowEvent, frame[0])

	var window windowEventPayload
	require.NoError(t, json.Unmarshal(frame[1:], &window))
	assert.Equal(t,
		windowEventPayload{Kind: "renamed", WindowID: "@3", Name: "shell", Active: true, Width: 213, Height: 55},
		window,
		"every window event carries tmux's size, not only the resized one")

	// The size the renderer must draw at is tmux's, so it rides the frame the
	// frontend already resizes on.
	frame, ok = encodeEvent(tmuxcc.WindowChanged{
		Kind:   tmuxcc.WindowResized,
		Window: tmuxcc.Window{ID: "@3", Name: "shell", Width: 80, Height: 24},
	})
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(frame[1:], &window))
	assert.Equal(t, "resized", window.Kind)
	assert.Equal(t, 80, window.Width)
	assert.Equal(t, 24, window.Height)
}
