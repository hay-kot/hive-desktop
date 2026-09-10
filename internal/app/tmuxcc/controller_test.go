package tmuxcc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func seededController() *controller {
	c := newController()
	c.set([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
		{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
	})
	return c
}

func TestControllerNotifications(t *testing.T) {
	t.Parallel()

	t.Run("window add is a placeholder until reconcile", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		require.Equal(t,
			[]Event{WindowChanged{Kind: WindowAdded, Window: Window{ID: "@3"}}},
			c.apply(WindowAddNotification{Window: "@3"}))
		require.Len(t, c.Windows(), 3)
	})

	t.Run("window close drops the window and its pane", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		events := c.apply(WindowCloseNotification{Window: "@2"})
		require.Equal(t, []Event{WindowChanged{
			Kind:   WindowClosed,
			Window: Window{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
		}}, events)

		_, ok := c.windowForPane("%2")
		require.False(t, ok)
	})

	t.Run("rename updates the window", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		events := c.apply(WindowRenamedNotification{Window: "@2", Name: "logs"})
		require.Equal(t, []Event{WindowChanged{
			Kind:   WindowRenamed,
			Window: Window{ID: "@2", Name: "logs", ActivePane: "%2", Width: 120, Height: 40},
		}}, events)
	})

	t.Run("pane change re-points the rendered pane", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		events := c.apply(WindowPaneChanged{Window: "@2", Pane: "%7"})
		require.Equal(t, []Event{WindowChanged{
			Kind:   WindowActiveChanged,
			Window: Window{ID: "@2", Name: "shell", ActivePane: "%7", Width: 120, Height: 40},
		}}, events)

		w, ok := c.windowForPane("%7")
		require.True(t, ok)
		require.Equal(t, "@2", w.ID)

		previous, ok := c.windowForPane("%2")
		require.True(t, ok, "the previous pane still routes so its output is drained")
		require.NotEqual(t, "%2", previous.ActivePane, "…but it is no longer the rendered pane")
	})

	t.Run("session window change moves the active flag", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		events := c.apply(SessionWindowChanged{Session: "$1", Window: "@2"})
		require.Equal(t, []Event{WindowChanged{
			Kind:   WindowActiveChanged,
			Window: Window{ID: "@2", Name: "shell", Active: true, ActivePane: "%2", Width: 120, Height: 40},
		}}, events)

		windows := c.Windows()
		require.False(t, windows[0].Active)
		require.True(t, windows[1].Active)
	})

	t.Run("pause and resume carry the window", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		require.Equal(t,
			[]Event{LifecycleChanged{Kind: LifecyclePaused, WindowID: "@1"}},
			c.apply(PauseNotification{Pane: "%1"}))
		require.Equal(t,
			[]Event{LifecycleChanged{Kind: LifecycleResumed, WindowID: "@1"}},
			c.apply(ContinueNotification{Pane: "%1"}))
	})

	t.Run("layout change carries the window's new size", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		require.Equal(t, []Event{WindowChanged{
			Kind:   WindowResized,
			Window: Window{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 80, Height: 24},
		}}, c.apply(LayoutChanged{Window: "@1", Width: 80, Height: 24}))

		require.Nil(t, c.apply(LayoutChanged{Window: "@1", Width: 80, Height: 24}),
			"a layout change that moves no boundary is not a resize")
		require.Nil(t, c.apply(LayoutChanged{Window: "@1"}),
			"an unreadable layout leaves the size alone; the reconcile it triggers carries it")
	})

	t.Run("notifications for unknown windows are inert", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		require.Nil(t, c.apply(WindowCloseNotification{Window: "@99"}))
		require.Nil(t, c.apply(WindowRenamedNotification{Window: "@99", Name: "x"}))
		require.Nil(t, c.apply(SessionWindowChanged{Session: "$1", Window: "@99"}))
		require.Nil(t, c.apply(LayoutChanged{Window: "@99", Width: 80, Height: 24}))
		require.Len(t, c.Windows(), 2)
	})
}

func TestControllerReconcileDiffs(t *testing.T) {
	t.Parallel()

	c := seededController()
	since := c.mark()
	events := c.reconcile([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 80, Height: 24},
		{ID: "@3", Name: "logs", ActivePane: "%3", Width: 80, Height: 24},
	}, since)

	require.Equal(t, []Event{
		WindowChanged{Kind: WindowResized, Window: Window{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 80, Height: 24}},
		WindowChanged{Kind: WindowAdded, Window: Window{ID: "@3", Name: "logs", ActivePane: "%3", Width: 80, Height: 24}},
		WindowChanged{Kind: WindowClosed, Window: Window{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40}},
	}, events)

	require.Equal(t, []string{"@1", "@3"}, windowIDs(c.Windows()))
}

// A reorder changes no window, so the snapshot's order is the only evidence it
// happened — and the only thing that carries it.
func TestControllerReconcileAdoptsTmuxOrder(t *testing.T) {
	t.Parallel()

	c := seededController()
	since := c.mark()
	events := c.reconcile([]Window{
		{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
	}, since)

	require.Empty(t, events, "a pure reorder changes nothing about any window")
	require.Equal(t, []string{"@2", "@1"}, windowIDs(c.Windows()))
}

// A %window-add placeholder gains its name and pane from the reconcile that
// follows it.
func TestControllerReconcileFillsPlaceholder(t *testing.T) {
	t.Parallel()

	c := seededController()
	c.apply(WindowAddNotification{Window: "@3"})
	since := c.mark()

	events := c.reconcile([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
		{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
		{ID: "@3", Name: "logs", ActivePane: "%3", Width: 120, Height: 40},
	}, since)

	require.Equal(t, []Event{WindowChanged{
		Kind:   WindowRenamed,
		Window: Window{ID: "@3", Name: "logs", ActivePane: "%3", Width: 120, Height: 40},
	}}, events)

	w, ok := c.windowForPane("%3")
	require.True(t, ok)
	require.Equal(t, "logs", w.Name)
}

func TestParseWindowLine(t *testing.T) {
	t.Parallel()

	w, ok := parseWindowLine("@275 1 %512 213 55 claude session")
	require.True(t, ok)
	require.Equal(t, Window{ID: "@275", Name: "claude session", Active: true, ActivePane: "%512", Width: 213, Height: 55}, w)

	w, ok = parseWindowLine("@276 0 %513 80 24")
	require.True(t, ok)
	require.Equal(t, Window{ID: "@276", ActivePane: "%513", Width: 80, Height: 24}, w)

	_, ok = parseWindowLine("nonsense")
	require.False(t, ok)
	_, ok = parseWindowLine("@277 0 %514 shell")
	require.False(t, ok, "a row without dimensions is not this format")
}

// reconcile's snapshot is fetched before it is merged, so a window added in
// between is in the set and not in the snapshot. Treating that absence as a
// close drops a window tmux is still holding (#278). Only the removal pass is
// unsafe against a stale snapshot; the add and update pass is idempotent.
func TestControllerReconcileKeepsWindowsAddedAfterTheSnapshot(t *testing.T) {
	t.Parallel()

	c := seededController()
	since := c.mark()
	c.apply(WindowAddNotification{Window: "@3"})

	events := c.reconcile([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
		{ID: "@2", Name: "shell", ActivePane: "%2", Width: 120, Height: 40},
	}, since)

	require.Empty(t, events, "a snapshot that predates @3 reports nothing about it")
	require.Equal(t, []string{"@1", "@2", "@3"}, windowIDs(c.Windows()),
		"the window survives the merge and keeps its place in the order")

	// It is a placeholder until a snapshot that has seen it fills it in, and
	// that snapshot is also what may legitimately close it.
	since = c.mark()
	c.reconcile([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1", Width: 120, Height: 40},
	}, since)
	require.Equal(t, []string{"@1"}, windowIDs(c.Windows()),
		"a snapshot taken after the add is authoritative and does close it")
}

func windowIDs(windows []Window) []string {
	ids := make([]string, 0, len(windows))
	for _, w := range windows {
		ids = append(ids, w.ID)
	}
	return ids
}
