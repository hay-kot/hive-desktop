package httpapi

import (
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// The schedules surface is the workspace manifest's schedules: list joined
// with the run state the app keeps beside it. It sits under the same prefix as
// the rest of the agent control plane because a schedule launches an agent
// CLI, which is arbitrary command execution (ADR terminal-transport).

// agentScheduleView is one schedule row. nextRunAt and lastRun are nullable on
// the wire: a disabled schedule has no next run, and one that has never fired
// has no last one.
type agentScheduleView struct {
	Workspace string                `json:"workspace"`
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Cron      string                `json:"cron"`
	Prompt    string                `json:"prompt"`
	Disabled  bool                  `json:"disabled"`
	OnMissed  string                `json:"onMissed"`
	NextRunAt *int64                `json:"nextRunAt"`
	LastRun   *agentScheduleRunView `json:"lastRun"`
}

// agentScheduleRunView is one execution. Timestamps are unix milliseconds;
// sessionId is null when the run launched no chat.
type agentScheduleRunView struct {
	ID           int64  `json:"id"`
	Workspace    string `json:"workspace"`
	ScheduleID   string `json:"scheduleId"`
	ScheduleName string `json:"scheduleName"`
	ScheduledFor int64  `json:"scheduledFor"`
	StartedAt    int64  `json:"startedAt"`
	Reason       string `json:"reason"`
	Status       string `json:"status"`
	Missed       int    `json:"missed"`
	SessionID    *int64 `json:"sessionId"`
	Prompt       string `json:"prompt"`
	Error        string `json:"error"`
}

func toAgentScheduleView(s app.ScheduleView) agentScheduleView {
	view := agentScheduleView{
		Workspace: s.Workspace, ID: s.ID, Name: s.Name, Cron: s.Cron, Prompt: s.Prompt,
		Disabled: s.Disabled, OnMissed: s.OnMissed, NextRunAt: s.NextRunAt,
	}
	if s.LastRun != nil {
		last := toAgentScheduleRunView(*s.LastRun)
		view.LastRun = &last
	}
	return view
}

func toAgentScheduleRunView(r app.RunView) agentScheduleRunView {
	return agentScheduleRunView{
		ID: r.ID, Workspace: r.Workspace, ScheduleID: r.ScheduleID, ScheduleName: r.ScheduleName,
		ScheduledFor: r.ScheduledFor, StartedAt: r.StartedAt, Reason: r.Reason, Status: r.Status,
		Missed: r.Missed, SessionID: r.SessionID, Prompt: r.Prompt, Error: r.Error,
	}
}

type agentSchedulesRequest struct {
	Workspace string `json:"workspace"`
}

func (b agentSchedulesRequest) Validate() error {
	return criterio.Run("workspace", b.Workspace, criterio.Required)
}

type agentSchedulesResponse struct {
	Schedules []agentScheduleView `json:"schedules"`
}

// AgentSchedules lists a workspace's schedules in manifest order.
func (ctrl *Controller) AgentSchedules(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSchedulesRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	schedules, err := ctrl.core.Schedules.List(r.Context(), body.Workspace)
	if err != nil {
		return err
	}
	views := make([]agentScheduleView, 0, len(schedules))
	for _, s := range schedules {
		views = append(views, toAgentScheduleView(s))
	}
	return server.JSON(w, http.StatusOK, agentSchedulesResponse{Schedules: views})
}

type agentScheduleSaveRequest struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Disabled  bool   `json:"disabled"`
	OnMissed  string `json:"onMissed"`
}

// Validate covers only the fields whose absence makes the request meaningless.
// The shape of the id, the cron expression and the prompt template are the
// core's to judge, so their failures come back as one classified error with
// the reason in it rather than as a field list assembled twice.
func (b agentScheduleSaveRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("workspace", b.Workspace, criterio.Required),
		criterio.Run("id", b.ID, criterio.Required),
		criterio.Run("cron", b.Cron, criterio.Required),
		criterio.Run("prompt", b.Prompt, criterio.Required),
	)
}

type agentScheduleResponse struct {
	Schedule agentScheduleView `json:"schedule"`
}

// AgentScheduleSave upserts one schedule into its workspace manifest.
func (ctrl *Controller) AgentScheduleSave(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentScheduleSaveRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	saved, err := ctrl.core.Schedules.Save(r.Context(), app.ScheduleEdit{
		Workspace: body.Workspace, ID: body.ID, Name: body.Name, Cron: body.Cron,
		Prompt: body.Prompt, Disabled: body.Disabled, OnMissed: body.OnMissed,
	})
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, agentScheduleResponse{Schedule: toAgentScheduleView(saved)})
}

type agentScheduleIDRequest struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id"`
}

func (b agentScheduleIDRequest) Validate() error {
	return criterio.ValidateStruct(
		criterio.Run("workspace", b.Workspace, criterio.Required),
		criterio.Run("id", b.ID, criterio.Required),
	)
}

type agentScheduleDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

// AgentScheduleDelete removes one schedule from its manifest.
func (ctrl *Controller) AgentScheduleDelete(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentScheduleIDRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	if err := ctrl.core.Schedules.Delete(r.Context(), body.Workspace, body.ID); err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, agentScheduleDeleteResponse{Deleted: true})
}

type agentScheduleRunResponse struct {
	Run agentScheduleRunView `json:"run"`
}

// AgentScheduleRun fires one schedule now, outside its timetable.
func (ctrl *Controller) AgentScheduleRun(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentScheduleIDRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	run, err := ctrl.core.Schedules.RunNow(r.Context(), body.Workspace, body.ID)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, agentScheduleRunResponse{Run: toAgentScheduleRunView(run)})
}

type agentScheduleRunsRequest struct {
	Workspace string `json:"workspace"`
	// ID empty spans every schedule in the workspace; Limit of 0 takes the
	// core's default.
	ID    string `json:"id"`
	Limit int    `json:"limit"`
}

func (b agentScheduleRunsRequest) Validate() error {
	return criterio.Run("workspace", b.Workspace, criterio.Required)
}

type agentScheduleRunsResponse struct {
	Runs []agentScheduleRunView `json:"runs"`
}

// AgentScheduleRuns lists run history, newest first.
func (ctrl *Controller) AgentScheduleRuns(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentScheduleRunsRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	runs, err := ctrl.core.Schedules.Runs(r.Context(), body.Workspace, body.ID, body.Limit)
	if err != nil {
		return err
	}
	views := make([]agentScheduleRunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, toAgentScheduleRunView(run))
	}
	return server.JSON(w, http.StatusOK, agentScheduleRunsResponse{Runs: views})
}

type agentSchedulePreviewRequest struct {
	Workspace string `json:"workspace"`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
}

// Validate requires only the workspace: the editor previews a half-typed cron
// and prompt on every keystroke, and their errors are the answer rather than a
// reason to refuse the call.
func (b agentSchedulePreviewRequest) Validate() error {
	return criterio.Run("workspace", b.Workspace, criterio.Required)
}

type agentSchedulePreviewResponse struct {
	Next        []int64 `json:"next"`
	Prompt      string  `json:"prompt"`
	CronError   string  `json:"cronError"`
	PromptError string  `json:"promptError"`
}

// AgentSchedulePreview dry-runs an unsaved edit.
func (ctrl *Controller) AgentSchedulePreview(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentSchedulePreviewRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	preview, err := ctrl.core.Schedules.Preview(r.Context(), app.PreviewRequest{
		Workspace: body.Workspace, Cron: body.Cron, Prompt: body.Prompt,
	})
	if err != nil {
		return err
	}
	next := preview.Next
	if next == nil {
		next = []int64{}
	}
	return server.JSON(w, http.StatusOK, agentSchedulePreviewResponse{
		Next: next, Prompt: preview.Prompt, CronError: preview.CronError, PromptError: preview.PromptError,
	})
}
