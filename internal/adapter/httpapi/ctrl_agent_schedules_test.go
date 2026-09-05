package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// seededWorkspace is the workspace openAgentWorkspaces creates the first time
// a root is made, which is every harness run.
const seededWorkspace = "hive"

// A schedule is written and read back on the workspace view: workspaces/update
// saves it and answers with what it wrote, and workspaces/open lists it joined
// with its run state. There is no schedules list route of its own.
func TestAgentWorkspaceViewCarriesTheSchedulesTheEditorWrote(t *testing.T) {
	h := newAgentHarness(t)

	resp := h.post(t, AgentWorkspacesPathPrefix+"schedules/runs", "", agentScheduleRunsRequest{Workspace: seededWorkspace})
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the schedules surface rides the terminal bearer gate")

	saved := h.saveWorkspaceSchedules(t, agentWorkspaceScheduleEdit{
		ID: "weekly", Name: "Weekly summary", Cron: "0 9 * * 5", Prompt: "Summarize the week.",
	})
	require.Len(t, saved.Schedules, 1, "the workspace view answers with the schedules it just wrote")
	assert.Equal(t, "weekly", saved.Schedules[0].ID)
	assert.Equal(t, "run", saved.Schedules[0].OnMissed)
	require.NotNil(t, saved.Schedules[0].NextRunAt)
	assert.Nil(t, saved.Schedules[0].LastRun)

	opened := h.post(t, AgentWorkspacesPathPrefix+"workspaces/open", testToken, agentWorkspaceDirRequest{Dir: seededWorkspace})
	defer func() { _ = opened.Body.Close() }()
	require.Equal(t, http.StatusOK, opened.StatusCode)
	var open agentWorkspaceOpenResponse
	require.NoError(t, json.NewDecoder(opened.Body).Decode(&open))
	require.Len(t, open.Workspace.Schedules, 1)
	assert.Equal(t, "Weekly summary", open.Workspace.Schedules[0].Name)

	cleared := h.saveWorkspaceSchedules(t)
	require.NotNil(t, cleared.Schedules, "schedules is never null on the wire")
	assert.Empty(t, cleared.Schedules, "an omitted list deletes every entry")
}

// saveWorkspaceSchedules writes the seeded workspace's schedules through the
// editor's own route and returns the workspace view it answers with.
func (h *terminalHarness) saveWorkspaceSchedules(t *testing.T, schedules ...agentWorkspaceScheduleEdit) agentWorkspaceView {
	t.Helper()
	resp := h.post(t, AgentWorkspacesPathPrefix+"workspaces/update", testToken, agentWorkspaceEditRequest{
		Dir: seededWorkspace, Name: "Hive", Agent: "claude", Autonomy: "ask", Schedules: schedules,
	})
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var view agentWorkspaceView
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&view))
	return view
}

// The core classifies a bad cron and an unknown workspace differently, and
// this adapter is the one place each becomes a status.
func TestAgentWorkspaceUpdateMapsScheduleFailuresToStatuses(t *testing.T) {
	h := newAgentHarness(t)

	badCron := h.post(t, AgentWorkspacesPathPrefix+"workspaces/update", testToken, agentWorkspaceEditRequest{
		Dir: seededWorkspace, Name: "Hive", Agent: "claude", Autonomy: "ask",
		Schedules: []agentWorkspaceScheduleEdit{{ID: "weekly", Cron: "not a cron", Prompt: "hi"}},
	})
	defer func() { _ = badCron.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, badCron.StatusCode)
	var failure struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	require.NoError(t, json.NewDecoder(badCron.Body).Decode(&failure))
	assert.Equal(t, "invalid", failure.Kind)
	assert.Contains(t, failure.Message, "not a cron", "the reason reaches the editor, not just the status")

	unknown := h.post(t, AgentWorkspacesPathPrefix+"workspaces/update", testToken, agentWorkspaceEditRequest{
		Dir: "no-such-workspace", Name: "Hive", Agent: "claude", Autonomy: "ask",
		Schedules: []agentWorkspaceScheduleEdit{{ID: "weekly", Cron: "@daily", Prompt: "hi"}},
	})
	_ = unknown.Body.Close()
	assert.Equal(t, http.StatusNotFound, unknown.StatusCode)

	missingID := h.post(t, AgentWorkspacesPathPrefix+"workspaces/update", testToken, agentWorkspaceEditRequest{
		Dir: seededWorkspace, Name: "Hive", Agent: "claude", Autonomy: "ask",
		Schedules: []agentWorkspaceScheduleEdit{{Cron: "@daily", Prompt: "hi"}},
	})
	_ = missingID.Body.Close()
	assert.Equal(t, http.StatusBadRequest, missingID.StatusCode, "an absent id is the spec's own rule")
}

// The frontend sends limit 0 for "however many you think", so the default has
// to be applied somewhere; this pins where.
func TestAgentScheduleRunsAppliesTheDefaultLimit(t *testing.T) {
	h := newAgentHarness(t)

	const inserted = 55
	for i := range inserted {
		_, err := h.core.Store.InsertScheduleRun(t.Context(), store.ScheduleRunRecord{
			Workspace: seededWorkspace, ScheduleID: "weekly", ScheduleName: "Weekly summary",
			ScheduledFor: int64(i), StartedAt: int64(i),
			Reason: "due", Status: "launched", SessionID: int64(i + 1),
		})
		require.NoError(t, err)
	}

	resp := h.post(t, AgentWorkspacesPathPrefix+"schedules/runs", testToken, agentScheduleRunsRequest{
		Workspace: seededWorkspace, ID: "", Limit: 0,
	})
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body agentScheduleRunsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Runs, 50)
	assert.Equal(t, int64(inserted-1), body.Runs[0].StartedAt, "newest first")
	require.NotNil(t, body.Runs[0].SessionID)

	empty := h.post(t, AgentWorkspacesPathPrefix+"schedules/runs", testToken, agentScheduleRunsRequest{
		Workspace: "nothing-here",
	})
	defer func() { _ = empty.Body.Close() }()
	require.Equal(t, http.StatusOK, empty.StatusCode)
	require.NoError(t, json.NewDecoder(empty.Body).Decode(&body))
	require.NotNil(t, body.Runs, "runs is never null on the wire")
	assert.Empty(t, body.Runs)
}

func TestAgentSchedulePreviewReportsErrorsAsFields(t *testing.T) {
	h := newAgentHarness(t)

	resp := h.post(t, AgentWorkspacesPathPrefix+"schedules/preview", testToken, agentSchedulePreviewRequest{
		Workspace: seededWorkspace, Cron: "every friday", Prompt: "{{ .Nope }}",
	})
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "a half-typed edit is previewed, not refused")
	var preview agentSchedulePreviewResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&preview))
	require.NotNil(t, preview.Next, "next is never null on the wire")
	assert.Empty(t, preview.Next)
	assert.NotEmpty(t, preview.CronError)
	assert.NotEmpty(t, preview.PromptError)
}
