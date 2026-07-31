package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

func newPtyHarness(t *testing.T, manager *fakeSessionManager) *PtyTerminalsService {
	t.Helper()
	sessions := newSessionsService(&fakeSessionLauncher{}, manager, manager, &fakeSessionTmux{}, &fakeJobRunner{})
	pty := ptyterm.NewManager(ptyterm.ManagerOptions{Shell: []string{"/bin/sh"}})
	t.Cleanup(func() { _ = pty.Stop(t.Context()) })
	return newPtyTerminalsService(pty, sessions)
}

// Where a shell opens is the session domain's answer, not this service's: the
// checkout is what makes a terminal for a session mean anything.
func TestPtyTerminalsService_StartsInTheSessionCheckout(t *testing.T) {
	manager, detail := activeSession()
	manager.details["s1"] = dispatch.SessionDetail{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: detail.State,
		Path: t.TempDir(),
	}
	svc := newPtyHarness(t, manager)

	started, err := svc.Start(t.Context(), "review-81")
	require.NoError(t, err)
	require.True(t, started)

	again, err := svc.Start(t.Context(), "review-81")
	require.NoError(t, err)
	require.False(t, again, "a live session is left alone rather than respawned")

	windows, err := svc.Attach(t.Context(), "review-81", 100, 30)
	require.NoError(t, err)
	require.Len(t, windows, 1)
}

// A recycled session has no checkout left, so there is nowhere to open a shell.
// The tmux backend refuses the same case for the same reason.
func TestPtyTerminalsService_RefusesASessionWithNoCheckout(t *testing.T) {
	manager, detail := activeSession()
	manager.sessions[0].State = "recycled"
	manager.details["s1"] = dispatch.SessionDetail{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: "recycled", Path: t.TempDir(),
	}
	svc := newPtyHarness(t, manager)

	_, err := svc.Start(t.Context(), "review-81")
	require.Equal(t, KindConflict, KindOf(err))
}

func TestPtyTerminalsService_UnknownSlug(t *testing.T) {
	manager, _ := activeSession()
	svc := newPtyHarness(t, manager)

	_, err := svc.Start(t.Context(), "no-such-session")
	require.Equal(t, KindNotFound, KindOf(err))

	// Attaching never spawns, so an unstarted slug is a not-found the frontend
	// answers with the start panel rather than a silent spawn (ADR 0044).
	_, err = svc.Attach(t.Context(), "review-81", 80, 24)
	require.Equal(t, KindNotFound, KindOf(err))

	// Listing tolerates it: callers enumerate every session the app knows about.
	windows, err := svc.ListWindows(t.Context(), "review-81")
	require.NoError(t, err)
	require.Empty(t, windows)
}
