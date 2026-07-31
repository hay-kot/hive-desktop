package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
)

func newPopupHarness(t *testing.T, manager *fakeSessionManager) *PopupTerminalsService {
	t.Helper()
	return newPopupHarnessWithCatalog(t, manager, nil)
}

func newPopupHarnessWithCatalog(t *testing.T, manager *fakeSessionManager, catalog *actions.ActionStore) *PopupTerminalsService {
	t.Helper()
	sessions := newSessionsService(&fakeSessionLauncher{}, manager, manager, &fakeSessionTmux{}, &fakeJobRunner{}, nil, nil, nil)
	pty := ptyterm.NewManager(ptyterm.ManagerOptions{Shell: []string{"/bin/sh"}})
	t.Cleanup(func() { _ = pty.Stop(t.Context()) })
	return newPopupTerminalsService(pty, sessions, catalog)
}

// popupCatalog writes an actions.yml and returns a store over it, so the
// launcher tests below go through the real parser rather than a hand-built
// Action the loader would have rejected.
func popupCatalog(t *testing.T, yaml string) *actions.ActionStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "actions.yml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	store := actions.NewActionStore(path)
	require.NoError(t, store.Reload())
	return store
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

const launcherCatalogYAML = `version: 1
actions:
  - id: run-tests
    label: Run tests
    type: shell
    targets: [session]
    command_template: 'mise run test'
launchers:
  - id: lazygit
    label: lazygit
    icon: git-branch
    command: lazygit
  - id: dotfiles
    label: Edit dotfiles
    cwd: "~"
    command: $EDITOR .
`

// A launcher without a cwd follows the session, which is what makes one
// shortcut mean "lazygit here" wherever you are.
func TestPopupTerminalsService_LauncherFollowsTheSessionCheckout(t *testing.T) {
	manager, detail := activeSession()
	checkout := t.TempDir()
	manager.details["s1"] = dispatch.SessionDetail{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: detail.State, Path: checkout,
	}
	svc := newPopupHarnessWithCatalog(t, manager, popupCatalog(t, launcherCatalogYAML))

	term, err := svc.Open(t.Context(), OpenPopupTerminal{Launcher: "lazygit", SessionSlug: "review-81"})
	require.NoError(t, err)
	require.Equal(t, checkout, term.Dir)
	require.Equal(t, "lazygit", term.Command)
}

// A configured cwd pins the launcher, and beats the session the caller was
// looking at when they pressed the key.
func TestPopupTerminalsService_LauncherCwdWinsOverTheSession(t *testing.T) {
	manager, detail := activeSession()
	manager.details["s1"] = dispatch.SessionDetail{
		ID: detail.ID, Name: detail.Name, Slug: detail.Slug, Repo: detail.Repo, State: detail.State, Path: t.TempDir(),
	}
	svc := newPopupHarnessWithCatalog(t, manager, popupCatalog(t, launcherCatalogYAML))

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	term, err := svc.Open(t.Context(), OpenPopupTerminal{Launcher: "dotfiles", SessionSlug: "review-81"})
	require.NoError(t, err)
	require.Equal(t, home, term.Dir, "a leading ~ is expanded: chdir takes a path, not a shell word")
	require.Equal(t, "$EDITOR .", term.Command)
}

func TestPopupTerminalsService_ListsLaunchersInCatalogOrder(t *testing.T) {
	manager, _ := activeSession()
	svc := newPopupHarnessWithCatalog(t, manager, popupCatalog(t, launcherCatalogYAML))

	launchers, err := svc.Launchers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []PopupLauncher{
		{ID: "lazygit", Label: "lazygit", Icon: "git-branch"},
		{ID: "dotfiles", Label: "Edit dotfiles"},
	}, launchers, "the launchers list, in file order; the actions beside it are not launchers")
}

func TestPopupTerminalsService_RefusesALaunchThatIsNotOne(t *testing.T) {
	manager, _ := activeSession()
	svc := newPopupHarnessWithCatalog(t, manager, popupCatalog(t, launcherCatalogYAML))

	_, err := svc.Open(t.Context(), OpenPopupTerminal{Launcher: "nope"})
	require.Equal(t, KindNotFound, KindOf(err))

	// An action id is not a launcher id: the lists are separate namespaces, so
	// naming an action here finds nothing rather than opening one.
	_, err = svc.Open(t.Context(), OpenPopupTerminal{Launcher: "run-tests"})
	require.Equal(t, KindNotFound, KindOf(err))

	// What a launcher runs is the catalog's answer, so a caller cannot send an
	// id and a command line and have both honoured.
	_, err = svc.Open(t.Context(), OpenPopupTerminal{Launcher: "lazygit", Command: "rm -rf /"})
	require.Equal(t, KindInvalid, KindOf(err))
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
