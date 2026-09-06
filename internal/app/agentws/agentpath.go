package agentws

import (
	"context"
	"fmt"
	"os/exec"
)

// AgentProblem reports why a workspace's declared agent will not start, or ""
// when it resolves. command is hive's configured command for that agent, empty
// when hive has no profile named by it.
//
// It is problemFor (catalogue.go) for the agent: field, and takes the same
// posture: resolve against the PATH a session launches with, not the launchd
// one a desktop process inherits, and report the failure as text the user can
// act on. The alternative is what a workspace seeded with agent: claude does
// on a machine with no claude -- the session starts, tmux ends it when the
// command exits 127, and the only account of why is a notice after the fact.
//
// The result is advisory and never gates a launch: a login shell's own
// startup files can put a command within reach that walking PATH cannot see,
// so a refusal here would break a setup that works.
func AgentProblem(ctx context.Context, agent, command string, lookPath func(context.Context, string) (string, error)) string {
	if command == "" {
		return fmt.Sprintf("agent %q is not configured; hive has no agent profile with that name", agent)
	}
	if lookPath == nil {
		lookPath = func(_ context.Context, name string) (string, error) { return exec.LookPath(name) }
	}
	if _, err := lookPath(ctx, command); err != nil {
		return fmt.Sprintf("agent %q: command %q not found on PATH; install the agent CLI, or point hive's agent profile at it", agent, command)
	}
	return ""
}
