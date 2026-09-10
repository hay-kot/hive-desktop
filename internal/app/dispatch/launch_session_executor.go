package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

// defaultPostHookTimeout is generous for a hook that hands the checkout to
// something else. Anything slower says so in post_hook_timeout.
const defaultPostHookTimeout = time.Minute

// LaunchSessionRequest is a rendered launch-session action, ready to hand to
// a SessionLauncher.
type LaunchSessionRequest struct {
	Name   string
	Prompt string
	Agent  string
	Repo   string
	// Origin is the inbox item the session is being created for, or a zero ref
	// for a session that has no item behind it.
	Origin models.ItemRef
}

// SessionLauncher spawns a hive session for a launch-session action.
type SessionLauncher interface {
	LaunchSession(ctx context.Context, req LaunchSessionRequest) (SessionExecutionOutcome, error)
}

// LaunchSessionExecutor renders a launch-session action's templates over
// the triggering msg and hands the result to a SessionLauncher.
type LaunchSessionExecutor struct {
	logger   zerolog.Logger
	launcher SessionLauncher
	env      ExecEnvironment
}

// NewLaunchSessionExecutor treats a nil launcher as unavailable, so an action
// fails rather than reporting a session it never created.
func NewLaunchSessionExecutor(logger zerolog.Logger, launcher SessionLauncher, env ExecEnvironment) *LaunchSessionExecutor {
	return &LaunchSessionExecutor{logger: logger, launcher: launcher, env: env}
}

func (e *LaunchSessionExecutor) Execute(ctx context.Context, action actions.Action, data OutputData, input ActionInvocationInput) (ExecutionResult, error) {
	cfg, ok := action.Config.(*actions.LaunchSessionConfig)
	if !ok {
		return ExecutionResult{}, fmt.Errorf("launch-session executor: action %q has config type %T", action.ID, action.Config)
	}
	if e.launcher == nil {
		return ExecutionResult{}, fmt.Errorf("launch-session executor: no session launcher configured")
	}

	renderer := tmpl.New(tmpl.Config{})

	prompt, err := renderer.Render(cfg.PromptTemplate, data)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("launch-session: prompt_template: %w", err)
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ExecutionResult{}, fmt.Errorf("launch-session: prompt_template rendered blank")
	}

	repo, err := RenderRepoTarget(action, data.Key, data.Raw, data.Inputs)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("launch-session: repo_template: %w", err)
	}

	name := SlugifySessionName(action.ID + "-" + data.Key)
	if err := ValidateSessionName(name); err != nil {
		return ExecutionResult{}, fmt.Errorf("launch-session: derived session name: %w", err)
	}

	agent := cfg.Agent
	if cfg.RepoTemplate == "" {
		if input.Session == nil {
			return ExecutionResult{}, fmt.Errorf("launch-session: repository, name, and agent input are required")
		}
		repo, name = strings.TrimSpace(input.Session.Repository), strings.TrimSpace(input.Session.Name)
		if repo == "" || name == "" {
			return ExecutionResult{}, fmt.Errorf("launch-session: repository and session name are required")
		}
		if err := ValidateSessionName(name); err != nil {
			return ExecutionResult{}, fmt.Errorf("launch-session: session name: %w", err)
		}
		if input.Session.Agent != "" {
			agent = input.Session.Agent
		}
	} else if repo == "" {
		return ExecutionResult{}, fmt.Errorf("launch-session: repo_template rendered blank")
	}
	if data.IsRerun {
		name = fmt.Sprintf("%s-rerun-%d", name, data.CommandID)
		if err := ValidateSessionName(name); err != nil {
			return ExecutionResult{}, fmt.Errorf("launch-session: rerun session name: %w", err)
		}
	}
	outcome, err := e.launcher.LaunchSession(ctx, LaunchSessionRequest{Name: name, Prompt: prompt, Agent: agent, Repo: repo, Origin: data.Origin})
	if err != nil {
		return ExecutionResult{Attempted: true}, err
	}
	return ExecutionResult{
		Attempted: true,
		Outcome:   &ExecutionOutcome{Session: &outcome},
		Log:       e.runPostHook(ctx, action, cfg, data, repo, outcome),
	}, nil
}

// A failure stays in the log and never becomes the action's error: the session
// already exists, so a retry would create a second one.
func (e *LaunchSessionExecutor) runPostHook(
	ctx context.Context,
	action actions.Action,
	cfg *actions.LaunchSessionConfig,
	data OutputData,
	repo string,
	outcome SessionExecutionOutcome,
) ExecutionLog {
	if strings.TrimSpace(cfg.PostHook) == "" {
		return ExecutionLog{}
	}
	logger := e.logger.With().Str("action_id", action.ID).Str("session_id", outcome.ID).Logger()
	failed := func(err error) ExecutionLog {
		logger.Warn().Err(err).Msg("launch-session: post hook failed")
		return ExecutionLog{Stderr: "post_hook: " + err.Error()}
	}
	if e.env == nil {
		return failed(errors.New("no execution environment configured"))
	}
	if outcome.Path == "" {
		return failed(errors.New("the launcher reported no checkout to run in"))
	}
	// Branch is absent: hive reports none for a fresh session, and a hook that
	// wants one is already a shell in the checkout.
	data.Session = &SessionTarget{ID: outcome.ID, Name: outcome.Name, Slug: outcome.Slug, Repo: repo, Path: outcome.Path}
	command, err := tmpl.New(tmpl.Config{}).Render(cfg.PostHook, data)
	if err != nil {
		return failed(err)
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return failed(errors.New("rendered blank"))
	}
	timeout := cfg.PostHookTimeout.Duration()
	if timeout == 0 {
		timeout = defaultPostHookTimeout
	}
	log, err := runShell(ctx, e.env, shellCommand{Command: command, Dir: outcome.Path, Timeout: timeout})
	if err != nil {
		logger.Warn().Err(err).Msg("launch-session: post hook failed")
		log.Stderr = strings.TrimRight("post_hook: "+err.Error()+"\n"+log.Stderr, "\n")
		return log
	}
	logger.Info().Msg("launch-session: post hook ran")
	return log
}
