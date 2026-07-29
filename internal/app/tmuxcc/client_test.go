//go:build !server

package tmuxcc

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func lifecycleIs(kind LifecycleKind) func(Event) bool {
	return func(ev Event) bool {
		lc, ok := ev.(LifecycleChanged)
		return ok && lc.Kind == kind
	}
}

func outputContains(windowID, want string) func(Event) bool {
	return func(ev Event) bool {
		out, ok := ev.(Output)
		return ok && out.WindowID == windowID && strings.Contains(string(out.Data), want)
	}
}

func lastLifecycle(t *testing.T, events []Event) LifecycleChanged {
	t.Helper()
	for _, ev := range slices.Backward(events) {
		if lc, ok := ev.(LifecycleChanged); ok {
			return lc
		}
	}
	t.Fatalf("no lifecycle event in %#v", events)
	return LifecycleChanged{}
}

func TestAttachRunsTheHandshakeSequence(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude", "@2 0 %2 shell")
	f.setCapture("%1", "claude> ready")
	f.setCapture("%2", "$ ")

	client := attachFake(t, f, Options{Cols: 120, Rows: 40})

	require.Equal(t, []Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1"},
		{ID: "@2", Name: "shell", ActivePane: "%2"},
	}, client.Windows())

	commands := f.sentCommands()
	require.Equal(t, "refresh-client -C 120,40", commands[0])
	require.Equal(t, `list-windows -F "`+listWindowsFormat+`"`, commands[1])
	require.Equal(t, "capture-pane -pe -J -t %1", commands[2])
	require.Equal(t, "capture-pane -pe -J -t %2", commands[3])

	for _, cmd := range commands {
		require.NotContains(t, cmd, "pause-after", "v1 never enables pause mode")
	}
}

// First paint reaches a subscriber that connects after Attach, because the
// broker buffered it.
func TestAttachFirstPaintsEachWindow(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude", "@2 0 %2 shell")
	f.setCapture("%1", "claude> ready", "second row")
	f.setCapture("%2", "$ ")

	client := attachFake(t, f, Options{})
	events, unsubscribe := subscribeAndCollect(t, client, lifecycleIs(LifecycleAttached))
	defer unsubscribe()

	require.Equal(t, "claude> ready\r\nsecond row", outputData(events, "@1"))
	require.Equal(t, "$ ", outputData(events, "@2"))
	require.Equal(t, LifecycleAttached, lastLifecycle(t, events).Kind)
}

// The pane keeps writing throughout the attach: the snapshot must land first
// and the live stream must stay contiguous behind it.
func TestAttachReplaysLiveOutputBehindSnapshot(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	f.setCapture("%1", "SNAPSHOT")

	stop, writerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			f.emit(`%output %1 A`)
		}
	}()

	client := attachFake(t, f, Options{})
	close(stop)
	<-writerDone
	f.emit(`%output %1 SENTINEL`)

	events, unsubscribe := subscribeAndCollect(t, client, outputContains("@1", "SENTINEL"))
	defer unsubscribe()

	data := outputData(events, "@1")
	require.True(t, strings.HasPrefix(data, "SNAPSHOT"), "snapshot paints first: %q", data)
	require.True(t, strings.HasSuffix(data, "SENTINEL"))

	middle := strings.TrimSuffix(strings.TrimPrefix(data, "SNAPSHOT"), "SENTINEL")
	require.Equal(t, strings.Repeat("A", len(middle)), middle, "the live stream stays contiguous")
}

// %window-add carries no name or pane, so it schedules a list-windows on the
// command worker — never on the reader goroutine.
func TestWindowAddTriggersReconcile(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	client := attachFake(t, f, Options{})

	f.setWindows("@1 1 %1 claude", "@2 0 %2 shell")
	f.emit("%window-add @2")

	events, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Window.ID == "@2" && wc.Window.Name == "shell"
	})
	defer unsubscribe()
	require.NotEmpty(t, events)

	f.awaitCommands(t, "list-windows", 2)
	require.Equal(t, []Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1"},
		{ID: "@2", Name: "shell", ActivePane: "%2"},
	}, client.Windows())
}

func TestReconcileIsCoalesced(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	client := attachFake(t, f, Options{})

	f.setWindows("@1 1 %1 claude", "@2 0 %2 shell")
	for range 5 {
		f.emit("%window-add @2")
		f.emit("%layout-change @2 b25d,80x24,0,0,1")
	}

	_, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Window.ID == "@2" && wc.Window.Name == "shell"
	})
	defer unsubscribe()

	require.Less(t, f.countCommands("list-windows"), 11, "pending reconciles absorb new triggers")
}

func TestSessionWindowChangedEmitsActiveChanged(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude", "@2 0 %2 shell")
	client := attachFake(t, f, Options{})

	f.emit("%session-window-changed $1 @2")

	events, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Kind == WindowActiveChanged
	})
	defer unsubscribe()

	changed, ok := events[len(events)-1].(WindowChanged)
	require.True(t, ok)
	require.Equal(t, "@2", changed.Window.ID)
	require.True(t, changed.Window.Active)
}

func TestExtendedOutputIsRoutedLikeOutput(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	client := attachFake(t, f, Options{})

	f.emit(`%extended-output %1 4212 : caught\040up\015\012`)

	events, unsubscribe := subscribeAndCollect(t, client, outputContains("@1", "caught"))
	defer unsubscribe()
	require.Contains(t, outputData(events, "@1"), "caught up\r\n")
}

func TestOversizedOutputReachesTheSubscriber(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	client := attachFake(t, f, Options{})

	payload := strings.Repeat("z", 70_000)
	f.emit("%output %1 " + payload)

	events, unsubscribe := subscribeAndCollect(t, client, outputContains("@1", "zzz"))
	defer unsubscribe()
	require.Contains(t, outputData(events, "@1"), payload)
}

// v1 renders one pane per window. The others are consumed so tmux never
// stalls on us, and measured, but never forwarded.
func TestOutputFromANonActivePaneIsDrainedNotForwarded(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	metrics := &fakeMetrics{}
	client := attachFake(t, f, Options{Metrics: metrics})

	f.emit("%window-pane-changed @1 %2")
	f.emit(`%output %1 background\015\012`)
	f.emit(`%output %2 foreground\015\012`)

	events, unsubscribe := subscribeAndCollect(t, client, outputContains("@1", "foreground"))
	defer unsubscribe()

	require.NotContains(t, outputData(events, "@1"), "background")
	require.Equal(t, len("background\r\nforeground\r\n"), metrics.bytes("hive-demo", "@1"),
		"the drained pane is still measured")
}

func TestWriteSendsHexChunks(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	client := attachFake(t, f, Options{})

	require.NoError(t, client.Write(t.Context(), "@1", []byte("hi")))
	require.Contains(t, f.sentCommands(), "send-keys -H -t %1 68 69")

	require.NoError(t, client.Write(t.Context(), "@1", []byte(strings.Repeat("a", sendKeysChunk+4))))
	sends := 0
	for _, cmd := range f.sentCommands() {
		if strings.HasPrefix(cmd, "send-keys") {
			sends++
		}
	}
	require.Equal(t, 3, sends, "payloads are chunked")

	require.ErrorIs(t, client.Write(t.Context(), "@99", []byte("x")), ErrUnknownWindow)
}

func TestWindowCommands(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	client := attachFake(t, f, Options{})
	ctx := t.Context()

	require.NoError(t, client.Resize(ctx, 100, 40))
	require.Contains(t, f.sentCommands(), "refresh-client -C 100,40")
	require.ErrorIs(t, client.Resize(ctx, 0, 40), ErrInvalidSize)
	require.ErrorIs(t, client.Resize(ctx, 80, maxDimension+1), ErrInvalidSize)

	require.NoError(t, client.SelectWindow(ctx, "@1"))
	require.Contains(t, f.sentCommands(), "select-window -t @1")

	id, err := client.NewWindow(ctx)
	require.NoError(t, err)
	require.Equal(t, "@9", id)

	require.NoError(t, client.RenameWindow(ctx, "@1", "my logs"))
	require.Contains(t, f.sentCommands(), `rename-window -t @1 'my logs'`)
	require.ErrorIs(t, client.RenameWindow(ctx, "@1", "bad\nname"), ErrInvalidName)

	require.NoError(t, client.CloseWindow(ctx, "@1"))
	require.Contains(t, f.sentCommands(), "kill-window -t @1")

	// A window id is attacker-reachable input: it never reaches a command line
	// unless it is @<digits> and one of ours.
	require.ErrorIs(t, client.SelectWindow(ctx, "@1; kill-server"), ErrUnknownWindow)
	require.ErrorIs(t, client.CloseWindow(ctx, "@404"), ErrUnknownWindow)
}

func TestCommandErrorSurfacesToCaller(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.failures["kill-window"] = "can't find window: @1"
	client := attachFake(t, f, Options{})

	var cmdErr *CommandError
	require.ErrorAs(t, client.CloseWindow(t.Context(), "@1"), &cmdErr)
	require.Equal(t, "can't find window: @1", cmdErr.Message)
}

func TestExitEndsTheStream(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		trigger func(f *fakeTmux)
		reason  string
	}{
		"%exit notification": {
			trigger: func(f *fakeTmux) { f.emit("%exit server exited") },
			reason:  "server exited",
		},
		"stdout close": {
			trigger: func(f *fakeTmux) { f.closeStreams() },
			reason:  "exited",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeTmux(t, "hive-demo")
			client := attachFake(t, f, Options{})
			ch, unsubscribe := client.Subscribe()
			defer unsubscribe()

			tc.trigger(f)

			events := collect(t, ch, lifecycleIs(LifecycleExited))
			require.Equal(t, tc.reason, lastLifecycle(t, events).Message)

			// The reader is joined and the process reaped by the time the
			// exit event is published, so Close is a no-op that returns.
			require.NoError(t, client.Close(t.Context()))
		})
	}
}

func TestCloseDetachesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	client := attachFake(t, f, Options{})
	ch, unsubscribe := client.Subscribe()
	defer unsubscribe()

	require.NoError(t, client.Close(t.Context()))
	require.NoError(t, client.Close(t.Context()))

	require.Contains(t, f.sentCommands(), "detach")

	events := collect(t, ch, lifecycleIs(LifecycleExited))
	require.Equal(t, "detached", lastLifecycle(t, events).Message)

	select {
	case _, open := <-ch:
		require.False(t, open, "the subscription closes with the client")
	case <-time.After(2 * time.Second):
		t.Fatal("subscription stayed open after close")
	}
}

func TestProtocolDesyncTearsDownTheClient(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	client := attachFake(t, f, Options{})
	ch, unsubscribe := client.Subscribe()
	defer unsubscribe()

	f.emit("%end 1 1 1")

	events := collect(t, ch, lifecycleIs(LifecycleExited))
	require.Equal(t, "protocol error", lastLifecycle(t, events).Message)

	var sawError bool
	for _, ev := range events {
		if lc, ok := ev.(LifecycleChanged); ok && lc.Kind == LifecycleError {
			sawError = true
			require.Contains(t, lc.Message, "protocol desync")
		}
	}
	require.True(t, sawError, "the desync is reported before the exit")
}

func TestMetricsSinkReceivesTelemetry(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 claude")
	f.setCapture("%1", "ready")
	metrics := &fakeMetrics{}
	client := attachFake(t, f, Options{Metrics: metrics})

	f.emit(`%output %1 live\015\012`)
	f.emit("%pause %1")
	f.emit("%continue %1")

	_, unsubscribe := subscribeAndCollect(t, client, lifecycleIs(LifecycleResumed))
	defer unsubscribe()

	require.Equal(t, len("ready")+len("live\r\n"), metrics.bytes("hive-demo", "@1"))
	require.Equal(t, 1, metrics.pauses())
	require.Equal(t, 1, metrics.resumes())
	require.True(t, metrics.sawDepth())
}

func TestAttachRejectsBadOptions(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	ctx := t.Context()

	_, err := Attach(ctx, ctx, Options{Slug: "hive", Cols: 0, Rows: 24, newProcess: f.factory()})
	require.ErrorIs(t, err, ErrInvalidSize)

	_, err = Attach(ctx, ctx, Options{Slug: "", Cols: 80, Rows: 24, newProcess: f.factory()})
	require.ErrorIs(t, err, ErrNotAttached)
}

func TestAttachFailsWhenTheStreamEndsBeforeHandshake(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.closeStreams()

	ctx := t.Context()
	_, err := Attach(ctx, ctx, Options{Slug: "hive-demo", Cols: 80, Rows: 24, newProcess: f.factory()})
	require.Error(t, err)
}

func subscribeAndCollect(t *testing.T, client *Client, stop func(Event) bool) ([]Event, func()) {
	t.Helper()
	ch, unsubscribe := client.Subscribe()
	return collect(t, ch, stop), unsubscribe
}

type fakeMetrics struct {
	mu       sync.Mutex
	streamed map[string]int
	pause    int
	resume   int
	depth    int
}

func (m *fakeMetrics) BytesStreamed(session, window string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streamed == nil {
		m.streamed = map[string]int{}
	}
	m.streamed[session+"/"+window] += n
}

func (m *fakeMetrics) FrameLatency(string, string, time.Duration) {}

func (m *fakeMetrics) PauseEvent(string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pause++
}

func (m *fakeMetrics) ResumeEvent(string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resume++
}

func (m *fakeMetrics) StreamBufferDepth(_, _ string, depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.depth++
	_ = depth
}

func (m *fakeMetrics) bytes(session, window string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamed[session+"/"+window]
}

func (m *fakeMetrics) pauses() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pause
}

func (m *fakeMetrics) resumes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resume
}

func (m *fakeMetrics) sawDepth() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.depth > 0
}
