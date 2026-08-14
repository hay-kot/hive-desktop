package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/hivecore/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type oneSessionManagement struct {
	SessionManagement
	session session.Session
}

func (m oneSessionManagement) GetSession(context.Context, string) (session.Session, error) {
	return m.session, nil
}

// stubGit answers each read from a field, with a matching error field so a
// single failing read can be isolated.
type stubGit struct {
	branch      string
	branchErr   error
	clean       bool
	cleanErr    error
	unpushed    bool
	unpushedErr error
	additions   int
	deletions   int
	diffErr     error
}

func (g stubGit) Branch(context.Context, string) (string, error) { return g.branch, g.branchErr }
func (g stubGit) IsClean(context.Context, string) (bool, error)  { return g.clean, g.cleanErr }

func (g stubGit) HasUnpushedCommits(context.Context, string) (bool, error) {
	return g.unpushed, g.unpushedErr
}

func (g stubGit) DiffStats(context.Context, string) (int, int, error) {
	return g.additions, g.deletions, g.diffErr
}

func activeSession() session.Session {
	return session.Session{ID: "s1", Path: "/tmp/review-81", Remote: "git@github.com:acme/site.git", State: session.StateActive}
}

func TestSessionGitStatusReportsTheCheckoutAndItsGitHubCoordinates(t *testing.T) {
	t.Parallel()

	manager := NewHiveSessionManager(oneSessionManagement{session: activeSession()}, nil, stubGit{
		branch: "feat/bar", clean: false, unpushed: true, additions: 42, deletions: 7,
	}, 0)

	got, err := manager.SessionGitStatus(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, SessionGitStatus{
		Path: "/tmp/review-81", Branch: "feat/bar", Dirty: true, Unpushed: true,
		Additions: 42, Deletions: 7, Owner: "acme", Repo: "site", Resolved: true,
	}, got)
}

// The delete confirmation assumes dirty when git fails, because over-warning is
// the safe side of that decision. A status badge has no such safe side: an
// unread checkout must not render as one with uncommitted work.
func TestSessionGitStatusReportsAFailedReadInsteadOfAssumingDirty(t *testing.T) {
	t.Parallel()

	manager := NewHiveSessionManager(oneSessionManagement{session: activeSession()}, nil, stubGit{
		branch: "feat/bar", cleanErr: errors.New("git status: exit 128"),
	}, 0)

	got, err := manager.SessionGitStatus(t.Context(), "s1")
	require.NoError(t, err)
	assert.False(t, got.Dirty)
	assert.Equal(t, "git status: exit 128", got.Error)
	assert.True(t, got.Resolved, "the branch resolved, so the rest of the bar still has something to show")
}

// Branch is the read every other read needs a working checkout for, so its
// failure stands for the whole status rather than leaving a half-filled one.
func TestSessionGitStatusReportsNothingResolvedWhenBranchFails(t *testing.T) {
	t.Parallel()

	manager := NewHiveSessionManager(oneSessionManagement{session: activeSession()}, nil, stubGit{
		branchErr: errors.New("git branch: not a repository"),
	}, 0)

	got, err := manager.SessionGitStatus(t.Context(), "s1")
	require.NoError(t, err)
	assert.False(t, got.Resolved)
	assert.Empty(t, got.Branch)
	assert.Equal(t, "git branch: not a repository", got.Error)
}

// A recycled session has no checkout left, which is not a failure — there is
// simply nothing to read.
func TestSessionGitStatusIsEmptyForASessionWithNoCheckout(t *testing.T) {
	t.Parallel()

	recycled := activeSession()
	recycled.State = session.StateRecycled
	manager := NewHiveSessionManager(oneSessionManagement{session: recycled}, nil, stubGit{branch: "feat/bar"}, 0)

	got, err := manager.SessionGitStatus(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, SessionGitStatus{}, got)
}

// A non-GitHub remote still gets its git half; only the pull-request key is
// empty, which is what tells the caller not to ask.
func TestSessionGitStatusLeavesCoordinatesEmptyForANonGitHubRemote(t *testing.T) {
	t.Parallel()

	elsewhere := activeSession()
	elsewhere.Remote = "git@gitea.example.test:acme/site.git"
	manager := NewHiveSessionManager(oneSessionManagement{session: elsewhere}, nil, stubGit{branch: "feat/bar"}, 0)

	got, err := manager.SessionGitStatus(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "feat/bar", got.Branch)
	assert.Empty(t, got.Owner)
	assert.Empty(t, got.Repo)
}
