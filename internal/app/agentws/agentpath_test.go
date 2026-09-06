package agentws

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentProblemFlagsACommandNotOnPATH(t *testing.T) {
	t.Parallel()

	// os.Args[0] is the compiled test binary: guaranteed to exist and be
	// executable without depending on PATH content.
	assert.Empty(t, AgentProblem(t.Context(), "claude", os.Args[0], nil))

	problem := AgentProblem(t.Context(), "claude", "definitely-not-a-real-binary-xyz", nil)
	assert.Contains(t, problem, "claude")
	assert.Contains(t, problem, "definitely-not-a-real-binary-xyz")
	assert.Contains(t, problem, "PATH")
}

// The same reason the catalogue asks the injected resolver (#266): a desktop
// launch inherits launchd's /usr/bin:/bin:/usr/sbin:/sbin, where no agent CLI
// a package manager installed can be found.
func TestAgentProblemResolvesAgainstTheInjectedLookPath(t *testing.T) {
	t.Parallel()

	lookPath := func(_ context.Context, name string) (string, error) {
		if name == "hive-test-claude" {
			return "/opt/homebrew/bin/hive-test-claude", nil
		}
		return "", exec.ErrNotFound
	}

	assert.Empty(t, AgentProblem(t.Context(), "claude", "hive-test-claude", lookPath))
	assert.NotEmpty(t, AgentProblem(t.Context(), "codex", "hive-test-codex", lookPath))
}

func TestAgentProblemReportsAnAgentHiveHasNoProfileFor(t *testing.T) {
	t.Parallel()

	problem := AgentProblem(t.Context(), "mystery-agent", "", nil)
	assert.Contains(t, problem, "mystery-agent")
	assert.Contains(t, problem, "not configured")
}
