package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

func newPopupHarness(t *testing.T, manager *fakeSessionManager) *PopupTerminalsService {
	t.Helper()
	sessions := newSessionsService(&fakeSessionLauncher{}, manager, manager, &fakeSessionTmux{}, &fakeJobRunner{}, nil, nil, nil)
	pty := ptyterm.NewManager(ptyterm.ManagerOptions{Shell: []string{"/bin/sh"}})
	t.Cleanup(func() { _ = pty.Stop(t.Context()) })
	return newPopupTerminalsService(pty, sessions)
}

// Where a terminal opens is the session domain's answer, not this service's:
// the checkout is what makes a terminal for a session mean anything.
func TestPopupTerminalsService_OpensInTheSessionCheckout(t *testing.T) {
	manager, detail := activeSession()
	checkout := t.TempDir()
	manager.details["s1"] = dispatch.SessionDetail{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: detail.State,
		Path: checkout,
	}
	svc := newPopupHarness(t, manager)

	term, err := svc.Open(t.Context(), OpenPopupTerminal{SessionSlug: "review-81"})
	require.NoError(t, err)
	require.Equal(t, checkout, term.Dir)

	// Every open is its own terminal: there is nothing keyed on the session, so
	// a second pop-up over the same checkout is a second shell.
	second, err := svc.Open(t.Context(), OpenPopupTerminal{SessionSlug: "review-81"})
	require.NoError(t, err)
	require.NotEqual(t, term.ID, second.ID)

	open, err := svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, open, 2)
}

// The resolution order is the contract: a slug wins over a path, and a caller
// with neither still lands somewhere a shell makes sense.
func TestPopupTerminalsService_ResolvesTheDirectoryInOrder(t *testing.T) {
	manager, _ := activeSession()
	svc := newPopupHarness(t, manager)
	dir := t.TempDir()

	explicit, err := svc.Open(t.Context(), OpenPopupTerminal{Dir: dir})
	require.NoError(t, err)
	require.Equal(t, dir, explicit.Dir)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	fallback, err := svc.Open(t.Context(), OpenPopupTerminal{})
	require.NoError(t, err)
	require.Equal(t, home, fallback.Dir)
}

// A recycled session has no checkout left, so there is nowhere to open one. The
// tmux backend refuses the same case for the same reason.
func TestPopupTerminalsService_RefusesASessionWithNoCheckout(t *testing.T) {
	manager, detail := activeSession()
	manager.sessions[0].State = "recycled"
	manager.details["s1"] = dispatch.SessionDetail{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: "recycled", Path: t.TempDir(),
	}
	svc := newPopupHarness(t, manager)

	_, err := svc.Open(t.Context(), OpenPopupTerminal{SessionSlug: "review-81"})
	require.Equal(t, KindConflict, KindOf(err))
}

func TestPopupTerminalsService_ClassifiesFailures(t *testing.T) {
	manager, _ := activeSession()
	svc := newPopupHarness(t, manager)

	_, err := svc.Open(t.Context(), OpenPopupTerminal{SessionSlug: "no-such-session"})
	require.Equal(t, KindNotFound, KindOf(err))

	_, err = svc.Open(t.Context(), OpenPopupTerminal{Dir: t.TempDir() + "/gone"})
	require.Equal(t, KindInvalid, KindOf(err), "a directory that cannot be entered is the caller's error")

	require.Equal(t, KindNotFound, KindOf(svc.Resize(t.Context(), "t99", 80, 24)))

	// Closing a terminal that is already gone is success with nothing done, so a
	// caller that saw the exit notice does not have to special-case the race.
	closed, err := svc.Close(t.Context(), "t99")
	require.NoError(t, err)
	require.False(t, closed)
}
