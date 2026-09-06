package mcpsrv

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// Scheduled chats on this server. The definitions live in each workspace's
// agent-workspace.yaml, so put_schedule and remove_schedule are manifest
// writes that touch one entry, and list_schedules joins the entries with the
// run state the app keeps beside them. Times are RFC 3339 in local time,
// unlike the canvas tools' unix milliseconds: cron is local time, and "Friday
// at 09:00" is what a model reasons about.
//
// Running a schedule outside its timetable spawns an agent CLI, which this
// surface never does (see the package comment); that stays the Chats area's
// "Run now".

type listWorkspacesOutput struct {
	Workspaces []workspaceSummary `json:"workspaces"`
}

type workspaceSummary struct {
	Dir      string `json:"dir"      jsonschema:"The directory name every schedule tool's workspace argument takes."`
	Name     string `json:"name"`
	Agent    string `json:"agent"`
	Autonomy string `json:"autonomy"`
	// Problem is why the manifest could not be read. The row still lists
	// with its last-good name, but it refuses schedule writes until the file
	// is fixed.
	Problem   string   `json:"problem,omitempty"`
	Schedules []string `json:"schedules"         jsonschema:"The ids of the workspace's schedules."`
}

type workspaceInput struct {
	Workspace string `json:"workspace" jsonschema:"The workspace directory name, as list_workspaces reports it. For the workspace this chat runs in, read this process's HIVE_AGENT_WORKSPACE environment variable."`
}

type listSchedulesOutput struct {
	Workspace string         `json:"workspace"`
	Schedules []scheduleView `json:"schedules"`
}

type scheduleView struct {
	ID        string           `json:"id"`
	Name      string           `json:"name,omitempty"`
	Cron      string           `json:"cron"`
	Prompt    string           `json:"prompt"`
	Disabled  bool             `json:"disabled"`
	OnMissed  string           `json:"onMissed"`
	NextRunAt string           `json:"nextRunAt,omitempty" jsonschema:"RFC 3339, local time. Absent when the schedule is disabled or its cron does not parse."`
	LastRun   *scheduleRunView `json:"lastRun,omitempty"   jsonschema:"The newest run whatever its outcome. Absent when the schedule has never run."`
}

type scheduleRunView struct {
	ID           int64  `json:"id"`
	ScheduleID   string `json:"scheduleId"`
	ScheduledFor string `json:"scheduledFor"      jsonschema:"RFC 3339, local time: the occurrence this run honored."`
	StartedAt    string `json:"startedAt"`
	Reason       string `json:"reason"            jsonschema:"due, catch_up, or manual."`
	Status       string `json:"status"            jsonschema:"launched, skipped, or failed."`
	Missed       int    `json:"missed"            jsonschema:"How many earlier occurrences were folded into this run."`
	Session      *int64 `json:"session,omitempty" jsonschema:"The chat the run launched. Absent when it launched none."`
	Error        string `json:"error,omitempty"`
}

type putScheduleInput struct {
	Workspace string `json:"workspace"          jsonschema:"The workspace directory name, as list_workspaces reports it. For the workspace this chat runs in, read this process's HIVE_AGENT_WORKSPACE environment variable."`
	ID        string `json:"id"                 jsonschema:"[a-z0-9-]+, unique in the workspace. An existing id is updated in place; a new one is appended."`
	Name      string `json:"name,omitempty"     jsonschema:"Shown in the UI and used to name the launched chat. Defaults to the id."`
	Cron      string `json:"cron"               jsonschema:"A 5-field cron expression, or @hourly, @daily, @weekly, @monthly, @every 1h. Local time."`
	Prompt    string `json:"prompt"             jsonschema:"A Go text/template rendered into the agent's opening message. Variables: .Now, .ScheduledFor, .LastRun (unset on the first run), .Reason, .Missed, .Schedule.ID, .Schedule.Name, .Schedule.Cron, .Workspace.Dir, .Workspace.Name; date \"2006-01-02\" .LastRun formats a time. Write the task itself: Hive frames it with what started the chat and how to end the session. Use preview_schedule to check it renders."`
	Disabled  bool   `json:"disabled,omitempty"`
	OnMissed  string `json:"onMissed,omitempty" jsonschema:"run (the default) folds occurrences missed while the app was closed into one catch-up launch; skip records them instead of launching."`
}

type scheduleIDInput struct {
	Workspace string `json:"workspace" jsonschema:"The workspace directory name, as list_workspaces reports it. For the workspace this chat runs in, read this process's HIVE_AGENT_WORKSPACE environment variable."`
	ID        string `json:"id"`
}

type removeScheduleOutput struct {
	Removed bool `json:"removed"`
}

type previewScheduleInput struct {
	Workspace string `json:"workspace" jsonschema:"The workspace directory name, as list_workspaces reports it. For the workspace this chat runs in, read this process's HIVE_AGENT_WORKSPACE environment variable."`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
}

type previewScheduleOutput struct {
	Next        []string `json:"next"                  jsonschema:"The next occurrences the cron produces, RFC 3339 local time. Empty when it does not parse."`
	Prompt      string   `json:"prompt"                jsonschema:"The template rendered against sample data. Empty when it does not render."`
	CronError   string   `json:"cronError,omitempty"`
	PromptError string   `json:"promptError,omitempty"`
}

type scheduleRunsInput struct {
	Workspace string `json:"workspace"       jsonschema:"The workspace directory name, as list_workspaces reports it. For the workspace this chat runs in, read this process's HIVE_AGENT_WORKSPACE environment variable."`
	ID        string `json:"id"`
	Limit     int    `json:"limit,omitempty" jsonschema:"How many newest runs to return. 0 takes the default of 50."`
}

type scheduleRunsOutput struct {
	Runs []scheduleRunView `json:"runs"`
}

func (ctrl *Controller) ListWorkspaces(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, listWorkspacesOutput, error) {
	views, err := ctrl.core.AgentWorkspaces.List(ctx)
	if err != nil {
		return nil, listWorkspacesOutput{}, ctrl.toolError(err)
	}
	out := listWorkspacesOutput{Workspaces: make([]workspaceSummary, 0, len(views))}
	for _, w := range views {
		ids := make([]string, 0, len(w.Schedules))
		for _, s := range w.Schedules {
			ids = append(ids, s.ID)
		}
		out.Workspaces = append(out.Workspaces, workspaceSummary{
			Dir: w.Dir, Name: w.Name, Agent: w.Agent, Autonomy: w.Autonomy, Problem: w.Problem, Schedules: ids,
		})
	}
	return nil, out, nil
}

func (ctrl *Controller) ListSchedules(ctx context.Context, _ *mcp.CallToolRequest, in workspaceInput) (*mcp.CallToolResult, listSchedulesOutput, error) {
	rows, err := ctrl.core.Schedules.List(ctx, in.Workspace)
	if err != nil {
		return nil, listSchedulesOutput{}, ctrl.toolError(err)
	}
	out := listSchedulesOutput{Workspace: in.Workspace, Schedules: make([]scheduleView, 0, len(rows))}
	for _, row := range rows {
		out.Schedules = append(out.Schedules, scheduleViewFrom(row))
	}
	return nil, out, nil
}

func (ctrl *Controller) PutSchedule(ctx context.Context, _ *mcp.CallToolRequest, in putScheduleInput) (*mcp.CallToolResult, scheduleView, error) {
	row, err := ctrl.core.AgentWorkspaces.PutSchedule(ctx, in.Workspace, app.ScheduleEdit{
		ID: in.ID, Name: in.Name, Cron: in.Cron, Prompt: in.Prompt, Disabled: in.Disabled, OnMissed: in.OnMissed,
	})
	if err != nil {
		return nil, scheduleView{}, ctrl.toolError(err)
	}
	return nil, scheduleViewFrom(row), nil
}

func (ctrl *Controller) RemoveSchedule(ctx context.Context, _ *mcp.CallToolRequest, in scheduleIDInput) (*mcp.CallToolResult, removeScheduleOutput, error) {
	if err := ctrl.core.AgentWorkspaces.RemoveSchedule(ctx, in.Workspace, in.ID); err != nil {
		return nil, removeScheduleOutput{}, ctrl.toolError(err)
	}
	return nil, removeScheduleOutput{Removed: true}, nil
}

func (ctrl *Controller) PreviewSchedule(ctx context.Context, _ *mcp.CallToolRequest, in previewScheduleInput) (*mcp.CallToolResult, previewScheduleOutput, error) {
	preview, err := ctrl.core.Schedules.Preview(ctx, app.PreviewRequest{Workspace: in.Workspace, Cron: in.Cron, Prompt: in.Prompt})
	if err != nil {
		return nil, previewScheduleOutput{}, ctrl.toolError(err)
	}
	out := previewScheduleOutput{
		Next: make([]string, 0, len(preview.Next)), Prompt: preview.Prompt,
		CronError: preview.CronError, PromptError: preview.PromptError,
	}
	for _, at := range preview.Next {
		out.Next = append(out.Next, localTime(at))
	}
	return nil, out, nil
}

func (ctrl *Controller) ScheduleRuns(ctx context.Context, _ *mcp.CallToolRequest, in scheduleRunsInput) (*mcp.CallToolResult, scheduleRunsOutput, error) {
	runs, err := ctrl.core.Schedules.Runs(ctx, in.Workspace, in.ID, in.Limit)
	if err != nil {
		return nil, scheduleRunsOutput{}, ctrl.toolError(err)
	}
	out := scheduleRunsOutput{Runs: make([]scheduleRunView, 0, len(runs))}
	for _, run := range runs {
		out.Runs = append(out.Runs, scheduleRunViewFrom(run))
	}
	return nil, out, nil
}

func scheduleViewFrom(row app.ScheduleView) scheduleView {
	view := scheduleView{
		ID: row.ID, Name: row.Name, Cron: row.Cron, Prompt: row.Prompt,
		Disabled: row.Disabled, OnMissed: row.OnMissed,
	}
	if row.NextRunAt != nil {
		view.NextRunAt = localTime(*row.NextRunAt)
	}
	if row.LastRun != nil {
		last := scheduleRunViewFrom(*row.LastRun)
		view.LastRun = &last
	}
	return view
}

func scheduleRunViewFrom(run app.RunView) scheduleRunView {
	return scheduleRunView{
		ID: run.ID, ScheduleID: run.ScheduleID,
		ScheduledFor: localTime(run.ScheduledFor), StartedAt: localTime(run.StartedAt),
		Reason: run.Reason, Status: run.Status, Missed: run.Missed,
		Session: run.SessionID, Error: run.Error,
	}
}

// localTime renders unix milliseconds the way the schedule tools answer with
// times: RFC 3339 in the machine's own zone, which is the zone cron runs in.
func localTime(unixMilli int64) string {
	return time.UnixMilli(unixMilli).Local().Format(time.RFC3339)
}
