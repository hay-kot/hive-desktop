package agentws

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
)

// TestRenderCodexTOMLEscapesReservedStrings pins the hand-rolled writer's
// escaping byte-for-byte: a command carrying a quote and a backslash, an arg
// carrying a newline, and an env value carrying a backslash and a quote.
// There is no TOML library backing this, so this is this feature's own
// correctness to prove.
func TestRenderCodexTOMLEscapesReservedStrings(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpcatalog.Server{
		"demo": {
			Transport: mcpcatalog.TransportStdio,
			Command:   "npx \"pkg\" \\bin",
			Args:      []string{"line1\nline2"},
			Env:       map[string]string{"TOKEN": "a\\b\"c"},
		},
	}

	got, err := renderCodexTOML(servers)
	require.NoError(t, err)

	want := "[mcp_servers.demo]\ncommand = \"npx \\\"pkg\\\" \\\\bin\"\nargs = [\"line1\\nline2\"]\nenv = { TOKEN = \"a\\\\b\\\"c\" }\n"
	assert.Equal(t, want, string(got))
}

func TestRenderCodexTOMLSortsServersByID(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpcatalog.Server{
		"zulu":  {Transport: mcpcatalog.TransportStdio, Command: "true"},
		"alpha": {Transport: mcpcatalog.TransportStdio, Command: "true"},
	}

	got, err := renderCodexTOML(servers)
	require.NoError(t, err)

	assert.Less(t, strings.Index(string(got), "mcp_servers.alpha"), strings.Index(string(got), "mcp_servers.zulu"))
}

func TestRenderCodexTOMLQuotesAReservedTableKey(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpcatalog.Server{
		"has space": {Transport: mcpcatalog.TransportStdio, Command: "true"},
	}

	got, err := renderCodexTOML(servers)
	require.NoError(t, err)
	assert.Equal(t, "[mcp_servers.\"has space\"]\ncommand = \"true\"\n", string(got))
}

func TestRenderMCPJSONIsKeySortedAndTypesRemoteServers(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpcatalog.Server{
		"zulu-remote": {Transport: mcpcatalog.TransportHttp, URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer xyz"}},
		"alpha-local": {Transport: mcpcatalog.TransportStdio, Command: "npx", Args: []string{"-y", "@playwright/mcp@latest"}},
	}

	got, err := renderMCPJSON(servers)
	require.NoError(t, err)
	assert.Less(t, strings.Index(string(got), `"alpha-local"`), strings.Index(string(got), `"zulu-remote"`))

	var decoded struct {
		MCPServers map[string]mcpJSONServer `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(got, &decoded))
	require.Len(t, decoded.MCPServers, 2)

	local := decoded.MCPServers["alpha-local"]
	assert.Empty(t, local.Type)
	assert.Equal(t, "npx", local.Command)
	assert.Equal(t, []string{"-y", "@playwright/mcp@latest"}, local.Args)

	remote := decoded.MCPServers["zulu-remote"]
	assert.Equal(t, "http", remote.Type)
	assert.Equal(t, "https://example.com/mcp", remote.URL)
	assert.Empty(t, remote.Command)
}

func TestRenderMCPJSONEmptyServersIsAnEmptyObject(t *testing.T) {
	t.Parallel()

	got, err := renderMCPJSON(map[string]mcpcatalog.Server{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"mcpServers":{}}`, string(got))
}

// TestAutonomyMappingIsTotal is the cross-product AutonomyNames() ×
// agentLaunches, not the table iterated against itself. "Every pair in the
// table resolves" would still pass if a fourth posture compiled with no
// entries added anywhere — exactly the silent flag-drop the whole authority
// decision exists to prevent (spec §13).
func TestAutonomyMappingIsTotal(t *testing.T) {
	t.Parallel()

	for agent, launch := range agentLaunches {
		for _, posture := range AutonomyNames() {
			_, ok := launch.Autonomy[Autonomy(posture)]
			assert.True(t, ok, "agent %q has no mapping for posture %q", agent, posture)
		}
	}

	_, err := Resolve("bogus-cmd", Workspace{Agent: "no-such-agent", Autonomy: AutonomyAsk, Dir: "/tmp"}, "sess", false)
	require.ErrorIs(t, err, ErrUnknownAgent)

	_, err = Resolve("claude", Workspace{Agent: "claude", Autonomy: Autonomy("bogus"), Dir: "/tmp"}, "sess", false)
	require.ErrorIs(t, err, ErrNoAutonomyMapping)
}

func TestAutonomyFlagsProjectsTheLaunchTable(t *testing.T) {
	t.Parallel()

	flags := AutonomyFlags()
	require.Contains(t, flags, "claude")
	require.Contains(t, flags, "codex")
	assert.Empty(t, flags["claude"][AutonomyAsk])
	assert.Equal(t, []string{"--permission-mode", "acceptEdits"}, flags["claude"][AutonomyAuto])
	assert.Equal(t, []string{"--dangerously-skip-permissions"}, flags["claude"][AutonomyFull])
	assert.Equal(t, []string{"--dangerously-bypass-approvals-and-sandbox"}, flags["codex"][AutonomyFull])

	// The projection must mirror Resolve's own withholding: a posture the
	// launch refuses is absent, never shown as launchable.
	for agent, postures := range flags {
		for posture := range postures {
			_, err := Resolve(agent, Workspace{Agent: agent, Autonomy: posture, Dir: "/abs/demo"}, "sess", false)
			assert.NoError(t, err, "agent %q posture %q is projected but refused", agent, posture)
		}
	}
}

func TestResolvePassesStrictMCPConfigForClaude(t *testing.T) {
	t.Parallel()

	w := Workspace{Agent: "claude", Autonomy: AutonomyAsk, Dir: "/abs/demo"}
	line, err := Resolve("claude", w, "sess-1", false)
	require.NoError(t, err)

	assert.Contains(t, line, "--strict-mcp-config")
	assert.Contains(t, line, shellQuote(filepath.Join("/abs/demo", ".mcp.json")))
	assert.Contains(t, line, "--session-id")
	assert.Contains(t, line, shellQuote("sess-1"))
}

// TestPosturesWithheldWithoutAnyMCPWiring resolves against a table entry
// with MCP == nil — no real agent has one today, so this exercises
// resolveAgainst directly rather than reaching for a fictitious global entry.
func TestPosturesWithheldWithoutAnyMCPWiring(t *testing.T) {
	t.Parallel()

	table := map[string]AgentLaunch{
		"no-mcp": {
			Autonomy: map[Autonomy][]string{
				AutonomyAsk:  {},
				AutonomyAuto: {"--auto"},
				AutonomyFull: {"--full"},
			},
		},
	}

	w := Workspace{Agent: "no-mcp", Autonomy: AutonomyAsk, Dir: "/abs/demo"}
	_, err := resolveAgainst(table, "agent-bin", w, "sess", false)
	require.NoError(t, err, "ask is available with no MCP wiring at all")

	for _, posture := range []Autonomy{AutonomyAuto, AutonomyFull} {
		w.Autonomy = posture
		_, err := resolveAgainst(table, "agent-bin", w, "sess", false)
		require.ErrorIs(t, err, ErrPostureUnavailable, "posture %q", posture)
	}
}

// TestUnboundedWiringSurfacesTheGlobalServerSet: codex's MCP wiring exists
// but does not bound the server set (Bounded: false, the D-B tradeoff), so
// unlike TestPosturesWithheldWithoutAnyMCPWiring every posture must still
// resolve — Bounded is what tells the service to surface the notice, not a
// second withholding rule.
func TestUnboundedWiringSurfacesTheGlobalServerSet(t *testing.T) {
	t.Parallel()

	bounded, ok := MCPBounded("codex")
	require.True(t, ok, "codex has known MCP wiring")
	assert.False(t, bounded, "codex's wiring does not bound the server set")

	for _, posture := range AutonomyNames() {
		w := Workspace{Agent: "codex", Autonomy: Autonomy(posture), Dir: "/abs/demo"}
		_, err := Resolve("codex", w, "sess", false)
		require.NoError(t, err, "posture %s", posture)
	}
}

// TestLaunchLineQuotesShellMetacharacters actually executes the finished
// line through a real shell for each dangerous workspace directory, rather
// than asserting shellQuote's own escaping in isolation: the property that
// matters is that the string never takes effect as shell syntax, and running
// it is the only way to prove that.
func TestLaunchLineQuotesShellMetacharacters(t *testing.T) {
	t.Parallel()

	dangerous := []string{
		"has space",
		"semi;colon",
		"$(subshell)",
		"back`tick`",
		"single'quote",
		"new\nline",
	}

	for _, d := range dangerous {
		t.Run(d, func(t *testing.T) {
			t.Parallel()

			// The line opens with cd into the workspace, so the dangerous name
			// must be a real directory for the rest of it to execute at all.
			dir := filepath.Join(t.TempDir(), d)
			require.NoError(t, os.Mkdir(dir, 0o700))

			w := Workspace{Agent: "claude", Autonomy: AutonomyAsk, Dir: dir}
			line, err := Resolve("echo", w, "sess", false)
			require.NoError(t, err)

			out, err := exec.Command("sh", "-c", line).CombinedOutput()
			require.NoError(t, err, string(out))
			assert.Contains(t, string(out), d)
		})
	}
}

// TestLaunchLineStartsInTheWorkspaceDirectory proves the cd survives a shell
// whose startup moved elsewhere — the failure mode that motivated it: tmux's
// -c sets the pane's initial directory, but the login shell's profile runs
// before -c's command and may cd away.
func TestLaunchLineStartsInTheWorkspaceDirectory(t *testing.T) {
	t.Parallel()

	// A minimal table entry so the line is nothing but `cd … && pwd` — a real
	// agent's flag set would be arguments pwd rejects.
	table := map[string]AgentLaunch{"probe": {Autonomy: map[Autonomy][]string{AutonomyAsk: {}}}}
	dir := t.TempDir()
	w := Workspace{Agent: "probe", Autonomy: AutonomyAsk, Dir: dir}
	line, err := resolveAgainst(table, "pwd", w, "sess", false)
	require.NoError(t, err)

	cmd := exec.Command("sh", "-c", line)
	cmd.Dir = os.TempDir() // stand-in for a profile that moved the shell away
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), filepath.Base(dir))
}

func TestRejectsAMultiWordAgentCommand(t *testing.T) {
	t.Parallel()

	w := Workspace{Agent: "claude", Autonomy: AutonomyAsk, Dir: "/tmp"}
	_, err := Resolve("claude --dangerously-skip-permissions", w, "sess", false)
	require.ErrorIs(t, err, ErrCommandNotASingleWord)
}

// TestClaudeConversationExistence pins the probe's fail-toward-resume
// polarity: absence is declared only over a readable projects tree, because a
// wrong "absent" abandons a real conversation while a wrong "present" merely
// reproduces the resume error the probe exists to avoid.
func TestClaudeConversationExistence(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	const id = "3f2c8a54-1111-4222-8333-444455556666"

	assert.True(t, HasConversation("claude", id),
		"no projects tree cannot confirm absence — resume and let claude report its own state")

	projectDir := filepath.Join(cfg, "projects", "-some-workspace")
	require.NoError(t, os.MkdirAll(projectDir, 0o700))
	assert.False(t, HasConversation("claude", id),
		"a readable projects tree without the file is a conversation that never persisted")

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, id+".jsonl"), []byte("{}\n"), 0o600))
	assert.True(t, HasConversation("claude", id))
}

func TestHasConversationIsTrueWithoutAProbe(t *testing.T) {
	t.Parallel()

	assert.True(t, HasConversation("codex", "any-id"), "no probe means resume decides for itself")
	assert.True(t, HasConversation("mystery-agent", "any-id"))
}
