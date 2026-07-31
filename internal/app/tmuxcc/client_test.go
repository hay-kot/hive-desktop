//go:build !server

package tmuxcc

import (
	"context"
	"fmt"
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
	f.setWindows("@1 1 %1 120 40 claude", "@2 0 %2 120 40 shell")
	f.setCapture("%1", "claude> ready")
	f.setCapture("%2", "$ ")

	client := attachFake(t, f, Options{Cols: 120, Rows: 40})

	require.Equal(t, []Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
		{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
	}, client.Windows())

	commands := f.sentCommands()
	require.Equal(t, "refresh-client -C 120,40", commands[0])
	require.Equal(t, `list-windows -F "`+listWindowsFormat+`"`, commands[1])
	// The bound is spelled out rather than built from historyLines: it is a
	// decision about startup cost, so changing it should fail a test.
	require.Equal(t, []string{
		`display-message -p -t %1 "` + cursorFormat + `"`,
		"capture-pane -pe -J -S -2000 -E -1 -t %1",
		"capture-pane -pe -S 0 -t %1",
		`display-message -p -t %2 "` + cursorFormat + `"`,
		"capture-pane -pe -J -S -2000 -E -1 -t %2",
		"capture-pane -pe -S 0 -t %2",
	}, commands[2:8], "each window is snapshotted cursor-first, then history, then screen")

	for _, cmd := range commands {
		require.NotContains(t, cmd, "pause-after", "v1 never enables pause mode")
	}
}

// A caller with nothing measured must not vote a placeholder: tmux would obey
// it and resize the session, and every other client attached to it, to a size
// nobody asked for. Setting no size keeps this client out of the negotiation
// until Resize opts it in.
func TestUnsizedAttachSetsNoClientSizeUntilResized(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")

	ctx := t.Context()
	client, err := Attach(ctx, ctx, Options{Slug: f.slug, newProcess: f.factory()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close(context.WithoutCancel(ctx)) })

	require.Zero(t, f.countCommands("refresh-client"))
	require.Equal(t, 120, client.Windows()[0].Width, "the session keeps the size its other clients gave it")

	require.NoError(t, client.Resize(ctx, 213, 55))
	require.Equal(t, "refresh-client -C 213,55", f.sentCommands()[len(f.sentCommands())-1])
}

// First paint reaches a subscriber that connects after Attach, because the
// broker buffered it.
func TestAttachFirstPaintsEachWindow(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 3 claude", "@2 0 %2 120 3 shell")
	f.setCapture("%1", "claude> ready", "second row", "")
	f.setCapture("%2", "$ ", "", "")

	client := attachFake(t, f, Options{})
	events, unsubscribe := subscribeAndCollect(t, client, lifecycleIs(LifecycleAttached))
	defer unsubscribe()

	require.Equal(t, "claude> ready\r\nsecond row\r\n", outputData(events, "@1"))
	require.Equal(t, "$ \r\n\r\n", outputData(events, "@2"))
	require.Equal(t, LifecycleAttached, lastLifecycle(t, events).Kind)
}

// The scrollback tmux holds for a pane is what makes attaching to a session
// that has been running for hours worth anything: without it the tab opens on
// whatever happens to be on screen and everything before it is gone.
func TestFirstPaintReplaysHistoryAheadOfTheScreen(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 2 claude")
	f.setHistory("%1", "$ echo hi", "hi")
	f.setCapture("%1", "$ ", "")
	f.setCursor("%1", 0, 2)

	client := attachFake(t, f, Options{})
	events, unsubscribe := subscribeAndCollect(t, client, lifecycleIs(LifecycleAttached))
	defer unsubscribe()

	require.Equal(t, "$ echo hi\r\nhi\r\n$ \r\n\x1b[1;3H", outputData(events, "@1"),
		"history scrolls out of the viewport, the screen fills it, the cursor lands last")
}

// An emulator pins its viewport to the last rows written, so the screen has to
// occupy the whole grid: paint it short and the pane's row 0 sits below the
// viewport's, and the cursor-addressed redraws an alternate-screen app makes
// land that many rows off for the rest of the attach.
func TestFirstPaintFillsTheWindowHeight(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 5 claude")
	f.setCapture("%1", "$ echo hi", "hi", "$ ")
	f.setCursor("%1", 2, 2)

	client := attachFake(t, f, Options{})
	events, unsubscribe := subscribeAndCollect(t, client, lifecycleIs(LifecycleAttached))
	defer unsubscribe()

	painted := outputData(events, "@1")
	require.Equal(t, 4, strings.Count(painted, "\r\n"), "five rows are five rows, blank or not")
	require.True(t, strings.HasSuffix(painted, "\x1b[3;3H"), "cursor restored to its own row: %q", painted)
}

// tmux answers with no cursor only when the pane went away mid-snapshot. Moving
// the cursor on a guess would be worse than leaving it where the screen put it.
func TestFirstPaintWithoutACursorLeavesItAlone(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 2 claude")
	f.setCapture("%1", "$ ", "")

	client := attachFake(t, f, Options{})
	events, unsubscribe := subscribeAndCollect(t, client, lifecycleIs(LifecycleAttached))
	defer unsubscribe()

	require.Equal(t, "$ \r\n", outputData(events, "@1"))
}

// The pane keeps writing throughout the attach: the snapshot must land first
// and the live stream must stay contiguous behind it.
func TestAttachReplaysLiveOutputBehindSnapshot(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 1 claude")
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
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})

	f.setWindows("@1 1 %1 120 40 claude", "@2 0 %2 120 40 shell")
	f.emit("%window-add @2")

	events, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Window.ID == "@2" && wc.Window.Name == "shell"
	})
	defer unsubscribe()
	require.NotEmpty(t, events)

	f.awaitCommands(t, "list-windows", 2)
	require.Equal(t, []Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
		{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
	}, client.Windows())
}

// %window-add carries no pane, so %output for the new window is unroutable
// until the reconcile lands and would otherwise be lost — along with the new
// tab's prompt. The snapshot the reconcile takes is what puts both on screen.
func TestReconcileFirstPaintsANewWindow(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})

	f.setCapture("%2", "$ echo hi", "hi")
	f.setWindows("@1 1 %1 120 40 claude", "@2 0 %2 120 2 shell")
	f.emit(`%output %2 unroutable\015\012`)
	f.emit("%window-add @2")

	f.awaitCommands(t, "capture-pane -pe -S 0 -t %2", 1)
	f.emit(`%output %2 SENTINEL`)

	events, unsubscribe := subscribeAndCollect(t, client, outputContains("@2", "SENTINEL"))
	defer unsubscribe()

	require.Equal(t, "$ echo hi\r\nhiSENTINEL", outputData(events, "@2"),
		"the snapshot paints first and live output replays behind it")

	var added bool
	for _, ev := range events {
		if wc, ok := ev.(WindowChanged); ok && wc.Window.ID == "@2" {
			added = true
			continue
		}
		if out, ok := ev.(Output); ok && out.WindowID == "@2" {
			require.True(t, added, "the tab exists before its first byte")
		}
	}
}

// A window can die while the snapshot that would give it its first paint is
// still in flight. The tab's closure has to reach the subscriber regardless, and
// the bytes held for a pane with no tab left have nowhere to go.
func TestWindowClosedDuringItsFirstPaint(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})

	f.setCapture("%2", "$ prompt")
	f.setWindows("@1 1 %1 120 40 claude", "@2 0 %2 120 40 shell")
	f.setOnCommand(func(cmd string) {
		if strings.HasPrefix(cmd, "capture-pane -pe -S 0 -t %2") {
			f.emit(`%output %2 HELD`)
			f.emit("%window-close @2")
		}
	})
	f.emit("%window-add @2")

	events, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Kind == WindowClosed && wc.Window.ID == "@2"
	})
	defer unsubscribe()

	for _, ev := range events {
		out, ok := ev.(Output)
		require.False(t, ok && out.WindowID == "@2", "a closed window renders nothing: %#v", ev)
	}
	require.NotContains(t, client.Windows(), Window{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40})

	client.paint.mu.Lock()
	defer client.paint.mu.Unlock()
	require.NotContains(t, client.paint.buf, "%2", "the gate still holds bytes for a dead pane")
	require.NotContains(t, client.paint.held, "%2", "the gate is still holding a dead pane")
	require.NotContains(t, client.paint.live, "%2")
}

// The server blocks mid-reply once the client's reader is gone. Teardown has to
// be able to kill it anyway — the harness used to hold its lock across that
// write, so Kill deadlocked and this test could not be written.
func TestTeardownWhileTheServerIsMidReply(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})
	ch, unsubscribe := client.Subscribe()
	defer unsubscribe()

	rows := make([]string, 0, 64)
	for i := range 64 {
		rows = append(rows, fmt.Sprintf("@%d 0 %%%d 120 40 shell", 100+i, 100+i))
	}
	f.setWindows(rows...)
	f.setOnCommand(func(cmd string) {
		if strings.HasPrefix(cmd, "list-windows") {
			f.emit("%end 1 1 1")
		}
	})
	f.emit("%window-add @2")

	events := collect(t, ch, lifecycleIs(LifecycleExited))
	require.Equal(t, "protocol error", lastLifecycle(t, events).Message)
}

func TestReconcileIsCoalesced(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})

	f.setWindows("@1 1 %1 120 40 claude", "@2 0 %2 120 40 shell")
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
	f.setWindows("@1 1 %1 120 40 claude", "@2 0 %2 120 40 shell")
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

// tmux sizes a window to the smallest attached client, so a second client
// attaching elsewhere resizes it under us. %layout-change is how that reaches
// the renderer in time to draw the repaint that follows at the right width.
func TestLayoutChangeEmitsResizedWithTheWindowSize(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})

	f.emit("%layout-change @1 b25d,80x24,0,0,1 b25d,80x24,0,0,1 *")

	events, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Kind == WindowResized
	})
	defer unsubscribe()

	resized, ok := events[len(events)-1].(WindowChanged)
	require.True(t, ok)
	require.Equal(t, "@1", resized.Window.ID)
	require.Equal(t, 80, resized.Window.Width)
	require.Equal(t, 24, resized.Window.Height)
}

func TestExtendedOutputIsRoutedLikeOutput(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
	client := attachFake(t, f, Options{})

	f.emit(`%extended-output %1 4212 : caught\040up\015\012`)

	events, unsubscribe := subscribeAndCollect(t, client, outputContains("@1", "caught"))
	defer unsubscribe()
	require.Contains(t, outputData(events, "@1"), "caught up\r\n")
}

func TestOversizedOutputReachesTheSubscriber(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 claude")
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
	f.setWindows("@1 1 %1 120 1 claude")
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

// The destination is an index into the resulting order; which tmux insertion
// expresses it, and whether the moved window keeps the selection, is what this
// pins down. -d is the flag that decides selection, and it has to follow the
// window being moved rather than being fixed.
func TestMoveWindowInsertsAtAPosition(t *testing.T) {
	t.Parallel()

	const (
		alpha   = "@1 0 %1 120 40 alpha"
		bravo   = "@2 0 %2 120 40 bravo"
		charlie = "@3 1 %3 120 40 charlie"
	)

	cases := map[string]struct {
		windowID string
		position int
		want     string
		reordered
	}{
		"a background window to the front": {
			windowID: "@2", position: 0,
			want:      "move-window -d -b -s @2 -t @1",
			reordered: reordered{bravo, alpha, charlie},
		},
		"a background window to the end": {
			windowID: "@1", position: 2,
			want:      "move-window -d -a -s @1 -t @3",
			reordered: reordered{bravo, charlie, alpha},
		},
		"one place along": {
			windowID: "@1", position: 1,
			want:      "move-window -d -a -s @1 -t @2",
			reordered: reordered{bravo, alpha, charlie},
		},
		"the active window keeps the selection": {
			windowID: "@3", position: 0,
			want:      "move-window -b -s @3 -t @1",
			reordered: reordered{charlie, alpha, bravo},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeTmux(t, "hive-demo")
			f.setWindows(alpha, bravo, charlie)
			client := attachFake(t, f, Options{})
			f.setWindows(tc.reordered...)

			windows, err := client.MoveWindow(t.Context(), tc.windowID, tc.position)
			require.NoError(t, err)

			require.Contains(t, f.sentCommands(), tc.want)
			require.Contains(t, f.sentCommands(), "move-window -r",
				"an insert leaves the session's indices with gaps otherwise")
			require.Equal(t, windowIDsOf(tc.reordered), windowIDs(windows),
				"the answer is the order tmux settled on, read back rather than assumed")
			require.Equal(t, windowIDs(windows), windowIDs(client.Windows()))
		})
	}
}

// A move is an unlink and a relink, so tmux reports the window it moved as
// closed. Acting on that would tear the tab and its terminal down and rebuild
// them blank — and, for the window that was selected, move the selection too.
func TestMoveWindowSurvivesTheRelinkClose(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 alpha", "@2 0 %2 120 40 bravo")
	client := attachFake(t, f, Options{})

	f.setOnCommand(func(cmd string) {
		if strings.HasPrefix(cmd, "move-window -") && !strings.HasPrefix(cmd, "move-window -r") {
			f.setWindows("@2 0 %2 120 40 bravo", "@1 1 %1 120 40 alpha")
			f.emit("%window-add @1")
			f.emit("%window-close @1")
		}
	})

	windows, err := client.MoveWindow(t.Context(), "@1", 1)
	require.NoError(t, err)
	require.Equal(t, []string{"@2", "@1"}, windowIDs(windows))
	require.Equal(t, []string{"@2", "@1"}, windowIDs(client.Windows()),
		"the moved window is still in the set the close claimed to remove")

	// The guard lifts with the move: a real close still closes.
	f.setWindows("@2 0 %2 120 40 bravo")
	f.emit("%window-close @1")
	events, unsubscribe := subscribeAndCollect(t, client, func(ev Event) bool {
		wc, ok := ev.(WindowChanged)
		return ok && wc.Kind == WindowClosed && wc.Window.ID == "@1"
	})
	defer unsubscribe()
	require.NotEmpty(t, events)
}

func TestMoveWindowRejectsWhatItCannotPlace(t *testing.T) {
	t.Parallel()

	f := newFakeTmux(t, "hive-demo")
	f.setWindows("@1 1 %1 120 40 alpha", "@2 0 %2 120 40 bravo")
	client := attachFake(t, f, Options{})
	ctx := t.Context()

	_, err := client.MoveWindow(ctx, "@404", 0)
	require.ErrorIs(t, err, ErrUnknownWindow)
	_, err = client.MoveWindow(ctx, "@1", 2)
	require.ErrorIs(t, err, ErrInvalidPosition)
	_, err = client.MoveWindow(ctx, "@1", -1)
	require.ErrorIs(t, err, ErrInvalidPosition)
	require.Zero(t, f.countCommands("move-window"), "nothing rejected reaches tmux")

	// Landing where it already is costs tmux a whole index shift for no reorder.
	windows, err := client.MoveWindow(ctx, "@1", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"@1", "@2"}, windowIDs(windows))
	require.Zero(t, f.countCommands("move-window"))
}

// reordered is the list-windows the fake answers with once the move has run.
type reordered []string

func windowIDsOf(lines []string) []string {
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		w, _ := parseWindowLine(line)
		ids = append(ids, w.ID)
	}
	return ids
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
	f.setWindows("@1 1 %1 120 1 claude")
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
