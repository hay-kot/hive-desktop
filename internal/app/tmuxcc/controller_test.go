package tmuxcc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func seededController() *controller {
	c := newController()
	c.set([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1"},
		{ID: "@2", Name: "shell", ActivePane: "%2"},
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
			Window: Window{ID: "@2", Name: "shell", ActivePane: "%2"},
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
			Window: Window{ID: "@2", Name: "logs", ActivePane: "%2"},
		}}, events)
	})

	t.Run("pane change re-points the rendered pane", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		events := c.apply(WindowPaneChanged{Window: "@2", Pane: "%7"})
		require.Equal(t, []Event{WindowChanged{
			Kind:   WindowActiveChanged,
			Window: Window{ID: "@2", Name: "shell", ActivePane: "%7"},
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
			Window: Window{ID: "@2", Name: "shell", Active: true, ActivePane: "%2"},
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

	t.Run("notifications for unknown windows are inert", func(t *testing.T) {
		t.Parallel()
		c := seededController()
		require.Nil(t, c.apply(WindowCloseNotification{Window: "@99"}))
		require.Nil(t, c.apply(WindowRenamedNotification{Window: "@99", Name: "x"}))
		require.Nil(t, c.apply(SessionWindowChanged{Session: "$1", Window: "@99"}))
		require.Len(t, c.Windows(), 2)
	})
}

func TestControllerReconcileDiffs(t *testing.T) {
	t.Parallel()

	c := seededController()
	events := c.reconcile([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1"},
		{ID: "@3", Name: "logs", ActivePane: "%3"},
	})

	require.Equal(t, []Event{
		WindowChanged{Kind: WindowAdded, Window: Window{ID: "@3", Name: "logs", ActivePane: "%3"}},
		WindowChanged{Kind: WindowClosed, Window: Window{ID: "@2", Name: "shell", ActivePane: "%2"}},
	}, events)

	require.Equal(t, []string{"@1", "@3"}, windowIDs(c.Windows()))
}

// A %window-add placeholder gains its name and pane from the reconcile that
// follows it.
func TestControllerReconcileFillsPlaceholder(t *testing.T) {
	t.Parallel()

	c := seededController()
	c.apply(WindowAddNotification{Window: "@3"})

	events := c.reconcile([]Window{
		{ID: "@1", Name: "claude", Active: true, ActivePane: "%1"},
		{ID: "@2", Name: "shell", ActivePane: "%2"},
		{ID: "@3", Name: "logs", ActivePane: "%3"},
	})

	require.Equal(t, []Event{WindowChanged{
		Kind:   WindowRenamed,
		Window: Window{ID: "@3", Name: "logs", ActivePane: "%3"},
	}}, events)

	w, ok := c.windowForPane("%3")
	require.True(t, ok)
	require.Equal(t, "logs", w.Name)
}

func TestParseWindowLine(t *testing.T) {
	t.Parallel()

	w, ok := parseWindowLine("@275 1 %512 claude session")
	require.True(t, ok)
	require.Equal(t, Window{ID: "@275", Name: "claude session", Active: true, ActivePane: "%512"}, w)

	w, ok = parseWindowLine("@276 0 %513")
	require.True(t, ok)
	require.Equal(t, Window{ID: "@276", ActivePane: "%513"}, w)

	_, ok = parseWindowLine("nonsense")
	require.False(t, ok)
}

func windowIDs(windows []Window) []string {
	ids := make([]string, 0, len(windows))
	for _, w := range windows {
		ids = append(ids, w.ID)
	}
	return ids
}
