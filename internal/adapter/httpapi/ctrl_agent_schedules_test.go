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

func TestAgentSchedulesListAndSaveOverTheWire(t *testing.T) {
	h := newAgentHarness(t)

	resp := h.post(t, AgentWorkspacesPathPrefix+"schedules", "", agentSchedulesRequest{Workspace: seededWorkspace})
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the schedules surface rides the terminal bearer gate")

	resp = h.post(t, AgentWorkspacesPathPrefix+"schedules", testToken, agentSchedulesRequest{Workspace: seededWorkspace})
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var list agentSchedulesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	require.NotNil(t, list.Schedules, "schedules is never null on the wire")
	assert.Empty(t, list.Schedules)

	saveResp := h.post(t, AgentWorkspacesPathPrefix+"schedules/save", testToken, agentScheduleSaveRequest{
		Workspace: seededWorkspace, ID: "weekly", Name: "Weekly summary",
		Cron: "0 9 * * 5", Prompt: "Summarize the week.",
	})
	defer func() { _ = saveResp.Body.Close() }()
	require.Equal(t, http.StatusOK, saveResp.StatusCode)
	var saved agentScheduleResponse
	require.NoError(t, json.NewDecoder(saveResp.Body).Decode(&saved))
	assert.Equal(t, "weekly", saved.Schedule.ID)
	assert.Equal(t, "run", saved.Schedule.OnMissed)
	require.NotNil(t, saved.Schedule.NextRunAt)
	assert.Nil(t, saved.Schedule.LastRun)

	resp2 := h.post(t, AgentWorkspacesPathPrefix+"schedules", testToken, agentSchedulesRequest{Workspace: seededWorkspace})
	defer func() { _ = resp2.Body.Close() }()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&list))
	require.Len(t, list.Schedules, 1)
	assert.Equal(t, "Weekly summary", list.Schedules[0].Name)

	deleteResp := h.post(t, AgentWorkspacesPathPrefix+"schedules/delete", testToken, agentScheduleIDRequest{
		Workspace: seededWorkspace, ID: "weekly",
	})
	defer func() { _ = deleteResp.Body.Close() }()
	require.Equal(t, http.StatusOK, deleteResp.StatusCode)
	var deleted agentScheduleDeleteResponse
	require.NoError(t, json.NewDecoder(deleteResp.Body).Decode(&deleted))
	assert.True(t, deleted.Deleted)
}

// The core classifies a bad cron, an unknown workspace and a missing field
// differently, and this adapter is the one place each becomes a status.
func TestAgentScheduleSaveMapsFailuresToStatuses(t *testing.T) {
	h := newAgentHarness(t)

	badCron := h.post(t, AgentWorkspacesPathPrefix+"schedules/save", testToken, agentScheduleSaveRequest{
		Workspace: seededWorkspace, ID: "weekly", Cron: "not a cron", Prompt: "hi",
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

	unknown := h.post(t, AgentWorkspacesPathPrefix+"schedules/save", testToken, agentScheduleSaveRequest{
		Workspace: "no-such-workspace", ID: "weekly", Cron: "@daily", Prompt: "hi",
	})
	_ = unknown.Body.Close()
	assert.Equal(t, http.StatusNotFound, unknown.StatusCode)

	missing := h.post(t, AgentWorkspacesPathPrefix+"schedules/save", testToken, agentScheduleSaveRequest{
		Workspace: seededWorkspace, Cron: "@daily", Prompt: "hi",
	})
	_ = missing.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, missing.StatusCode, "an absent id is caught before the core")
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
