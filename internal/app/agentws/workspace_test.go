package agentws

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func validWorkspace() Workspace {
	return Workspace{Version: 1, Name: "X", Agent: "claude", Autonomy: AutonomyAsk}
}

func TestWorkspaceValidate(t *testing.T) {
	t.Parallel()

	t.Run("ValidPasses", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validWorkspace().Validate())
	})

	t.Run("NameRequired", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.Name = ""
		require.Error(t, w.Validate())
	})

	t.Run("AgentRequired", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.Agent = ""
		require.Error(t, w.Validate())
	})

	t.Run("AutonomyEmptyIsValid", func(t *testing.T) {
		t.Parallel()
		// Validate no longer requires it: an omitted autonomy defaults to ask
		// at load (see TestAutonomyDefaultsToAsk in loader_test.go), and this
		// asserts Validate itself does not stand in the way of that.
		w := validWorkspace()
		w.Autonomy = ""
		require.NoError(t, w.Validate())
	})

	t.Run("AutonomyMustBeValid", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.Autonomy = Autonomy("yolo")
		require.Error(t, w.Validate())
	})

	t.Run("EveryAutonomyValueIsValid", func(t *testing.T) {
		t.Parallel()
		for _, name := range AutonomyNames() {
			w := validWorkspace()
			w.Autonomy = Autonomy(name)
			require.NoError(t, w.Validate(), "autonomy %q must validate", name)
		}
	})

	t.Run("DuplicateMCPRejected", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.MCPs = []string{"playwright", "playwright"}
		require.Error(t, w.Validate())
	})

	t.Run("DuplicateSkillRejected", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.Skills = []string{"hive-mcp", "hive-mcp"}
		require.Error(t, w.Validate())
	})

	t.Run("EmptyMCPEntryRejected", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.MCPs = []string{""}
		require.Error(t, w.Validate())
	})

	t.Run("EmptySkillEntryRejected", func(t *testing.T) {
		t.Parallel()
		w := validWorkspace()
		w.Skills = []string{""}
		require.Error(t, w.Validate())
	})
}
