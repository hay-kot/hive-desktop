package agentws

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
)

// MCPWiring is how one agent receives the workspace's declared MCP set.
type MCPWiring struct {
	// File is written by the generator, relative to the workspace directory.
	File string
	// Render encodes the resolved servers in that file's format.
	Render func(servers map[string]mcpcatalog.Server) ([]byte, error)
	// Args name the file at launch. nil when the agent discovers it from the
	// working directory instead, as codex does.
	Args func(workspaceDir string) []string
	// Bounded reports whether this wiring confines the agent to exactly this
	// set. When false the agent also loads its own global servers, and the UI
	// says so — a posture is only meaningful against a tool set the user can
	// see.
	Bounded bool
}

// AgentLaunch is everything Hive knows about driving one agent CLI. It is
// data, not a branch: a new agent is an entry.
type AgentLaunch struct {
	// Autonomy maps a posture onto flags. A posture with no entry fails
	// closed — see ErrNoAutonomyMapping.
	Autonomy map[Autonomy][]string

	// MCP is how this agent receives the workspace's servers. A nil MCP means
	// no known project-scoped form at all: auto and full are withheld and the
	// workspace runs at ask.
	MCP *MCPWiring

	// SessionArgs renders the flags pinning a caller-minted session id. nil
	// means the agent mints its own id and Hive cannot address it. The caller
	// mints the uuid, not the table: a table that mints is non-deterministic
	// and cannot be goldened.
	SessionArgs func(id string) []string

	// Resume renders the resume invocation. nil means no known resume form:
	// the session relaunches fresh and the UI says the previous conversation
	// could not be resumed.
	Resume func(id string) []string

	// HasConversation reports whether the agent holds a persisted conversation
	// addressable by id; nil means Hive cannot tell and resume is attempted
	// unconditionally. It exists because an agent may accept a session id at
	// launch yet persist nothing until the first message — resuming such an id
	// dies in the pane, and the caller would rather relaunch fresh.
	HasConversation func(id string) bool
}

var (
	// ErrUnknownAgent reports an agent key with no launch entry.
	ErrUnknownAgent = errors.New("agentws: no launch mapping for agent")
	// ErrNoAutonomyMapping reports an (agent, posture) pair with no flags.
	ErrNoAutonomyMapping = errors.New("agentws: no autonomy mapping")
	// ErrPostureUnavailable reports a posture withheld from an agent with no
	// MCP wiring.
	ErrPostureUnavailable = errors.New("agentws: posture requires a bounded tool set")
	// ErrCommandNotASingleWord reports a hive agent profile whose command
	// carries flags.
	ErrCommandNotASingleWord = errors.New("agentws: agent command must be a single word")
)

// agentLaunches is the launch table, keyed by the agent key a workspace names
// in agent-workspace.yaml. The generator ranges it rather than special-casing
// each agent, so adding one here adds its generated MCP file with no edit to
// generate.go, and Resolve ranges it the same way for the launch line.
var agentLaunches = map[string]AgentLaunch{
	"claude": {
		Autonomy: map[Autonomy][]string{
			AutonomyAsk:  {},
			AutonomyAuto: {"--permission-mode", "acceptEdits"},
			AutonomyFull: {"--dangerously-skip-permissions"},
		},
		MCP: &MCPWiring{
			File:   ".mcp.json",
			Render: renderMCPJSON,
			Args: func(dir string) []string {
				return []string{"--strict-mcp-config", "--mcp-config", filepath.Join(dir, ".mcp.json")}
			},
			Bounded: true,
		},
		SessionArgs:     func(id string) []string { return []string{"--session-id", id} },
		Resume:          func(id string) []string { return []string{"--resume", id} },
		HasConversation: claudeConversationExists,
	},
	"codex": {
		Autonomy: map[Autonomy][]string{
			AutonomyAsk:  {},
			AutonomyAuto: {"--ask-for-approval", "on-request", "--sandbox", "workspace-write"},
			AutonomyFull: {"--dangerously-bypass-approvals-and-sandbox"},
		},
		MCP: &MCPWiring{
			File:    ".codex/config.toml",
			Render:  renderCodexTOML,
			Bounded: false,
		},
		// codex has no launch-time session-id flag and no resume-by-id form
		// (`codex resume` takes only an id or name the agent itself minted),
		// so SessionArgs and Resume both stay nil: a reopened codex session
		// relaunches fresh and says so.
	},
}

// SupportsResume reports whether agent has any known resume form at all.
// The service calls this before Resolve to decide ResumeAttempted: Resolve
// itself only reports whether ITS OWN inputs (the agent key, the posture)
// were unknown, not which optional per-agent capabilities are present, and
// ResumeAttempted has to be known before the launch line is built.
func SupportsResume(agent string) bool {
	launch, ok := agentLaunches[agent]
	return ok && launch.Resume != nil
}

// HasConversation reports whether agent is known to hold a persisted
// conversation under id — true when Hive cannot tell, so an uncertain caller
// resumes and lets the agent report its own state.
func HasConversation(agent, id string) bool {
	launch, ok := agentLaunches[agent]
	if !ok || launch.HasConversation == nil {
		return true
	}
	return launch.HasConversation(id)
}

// claudeConversationExists reports whether claude persisted a conversation
// under id. claude writes <config>/projects/<munged-cwd>/<session-id>.jsonl on
// the first message of a conversation — never at launch — with config being
// ~/.claude or $CLAUDE_CONFIG_DIR. The id is a uuid Hive minted, so matching
// the bare filename across every project directory is collision-safe and
// independent of claude's cwd-munging scheme.
//
// The check fails toward resuming: absence is only declared over a readable
// projects tree, because a wrong "absent" silently abandons a real
// conversation, while a wrong "present" merely reproduces the resume error
// the caller would have hit anyway.
func claudeConversationExists(id string) bool {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return true
		}
		root = filepath.Join(home, ".claude")
	}
	projects := filepath.Join(root, "projects")
	if info, err := os.Stat(projects); err != nil || !info.IsDir() {
		return true
	}
	matches, err := filepath.Glob(filepath.Join(projects, "*", id+".jsonl"))
	if err != nil {
		return true
	}
	return len(matches) > 0
}

// AutonomyFlags projects the launch table for display: per agent, the CLI
// flags each posture resolves to. A posture absent from an agent's map is one
// Resolve would refuse — no autonomy mapping, or withheld because the agent
// has no MCP wiring (ErrPostureUnavailable) — so a UI can disable exactly
// what the launch would fail closed on. The slices are copies: nothing
// reaches the table through the result.
func AutonomyFlags() map[string]map[Autonomy][]string {
	out := make(map[string]map[Autonomy][]string, len(agentLaunches))
	for agent, launch := range agentLaunches {
		postures := make(map[Autonomy][]string, len(launch.Autonomy))
		for posture, flags := range launch.Autonomy {
			if launch.MCP == nil && posture != AutonomyAsk {
				continue
			}
			postures[posture] = slices.Clone(flags)
		}
		out[agent] = postures
	}
	return out
}

// MCPBounded reports whether agent's MCP wiring confines it to exactly the
// workspace's declared servers. ok is false when the agent has no launch
// entry or no MCP wiring at all, which the caller (building a user-facing
// notice) treats the same as "nothing to bound."
func MCPBounded(agent string) (bounded, ok bool) {
	launch, exists := agentLaunches[agent]
	if !exists || launch.MCP == nil {
		return false, false
	}
	return launch.MCP.Bounded, true
}

// Resolve builds the finished login-shell command line for one launch, or
// reports which of the agent and the posture had no mapping. It returns the
// line rather than flags because the security-relevant step is interpolating
// paths into it — see shellQuote. w.Dir must be the absolute workspace
// directory: it is threaded straight into MCPWiring.Args, which needs a path
// that resolves regardless of the launched process's cwd.
func Resolve(command string, w Workspace, sessionID string, resume bool) (string, error) {
	return resolveAgainst(agentLaunches, command, w, sessionID, resume)
}

// resolveAgainst is Resolve's implementation, parameterized over the launch
// table so tests can exercise a fail-closed path (an agent with MCP == nil)
// that does not exist in the real, always-total agentLaunches.
func resolveAgainst(table map[string]AgentLaunch, command string, w Workspace, sessionID string, resume bool) (string, error) {
	if len(strings.Fields(command)) != 1 {
		return "", fmt.Errorf("%w: %q", ErrCommandNotASingleWord, command)
	}

	launch, ok := table[w.Agent]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownAgent, w.Agent)
	}

	autonomyFlags, ok := launch.Autonomy[w.Autonomy]
	if !ok {
		return "", fmt.Errorf("%w: agent %q, posture %q", ErrNoAutonomyMapping, w.Agent, w.Autonomy)
	}
	if launch.MCP == nil && w.Autonomy != AutonomyAsk {
		return "", fmt.Errorf("%w: agent %q, posture %q", ErrPostureUnavailable, w.Agent, w.Autonomy)
	}

	words := []string{command}
	words = append(words, autonomyFlags...)
	if launch.MCP != nil && launch.MCP.Args != nil {
		words = append(words, launch.MCP.Args(w.Dir)...)
	}
	switch {
	case resume && launch.Resume != nil:
		words = append(words, launch.Resume(sessionID)...)
	case !resume && launch.SessionArgs != nil:
		words = append(words, launch.SessionArgs(sessionID)...)
	}

	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = shellQuote(word)
	}
	// The line runs under $SHELL -l -c, and a login shell's profile is free to
	// cd somewhere else before -c executes; tmux's -c only sets the pane's
	// initial directory. The explicit cd is what guarantees the agent starts
	// in the workspace regardless of what the user's dotfiles do.
	return "cd " + shellQuote(w.Dir) + " && " + strings.Join(quoted, " "), nil
}

// shellQuote wraps s for a POSIX login shell: single quotes, with embedded
// single quotes closed and re-opened. ptyterm hands the line to $SHELL -l -c
// verbatim, so every interpolated path passes through here.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// mcpJSONServer is one entry under .mcp.json's "mcpServers" — claude's own
// format. A remote server carries "type" plus url/headers; a stdio server
// carries command/args/env and no "type" (claude infers stdio from the
// absence of one).
type mcpJSONServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// renderMCPJSON encodes servers as claude's .mcp.json. encoding/json sorts a
// map's string keys on encode, which is what gives both the server id order
// and the id-to-entry order their determinism — nothing here ranges a map
// straight into output.
func renderMCPJSON(servers map[string]mcpcatalog.Server) ([]byte, error) {
	out := struct {
		MCPServers map[string]mcpJSONServer `json:"mcpServers"`
	}{MCPServers: make(map[string]mcpJSONServer, len(servers))}

	for id, s := range servers {
		entry := mcpJSONServer{
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			URL:     s.URL,
			Headers: s.Headers,
		}
		if s.Transport == mcpcatalog.TransportHttp || s.Transport == mcpcatalog.TransportSse {
			entry.Type = string(s.Transport)
			entry.Command = ""
			entry.Args = nil
			entry.Env = nil
		}
		out.MCPServers[id] = entry
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("agentws: encode .mcp.json: %w", err)
	}
	return append(data, '\n'), nil
}

// renderCodexTOML hand-rolls codex's .codex/config.toml: one
// [mcp_servers.<id>] table per server, key-sorted by id, with basic-string
// escaping for backslashes, quotes and control characters. No TOML library
// exists in the module graph (verified — go.mod and go.sum carry none), and a
// full encoder for a subset this small would only trade explicit key
// ordering here for a marshaller's own habits.
func renderCodexTOML(servers map[string]mcpcatalog.Server) ([]byte, error) {
	ids := make([]string, 0, len(servers))
	for id := range servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var buf strings.Builder
	for i, id := range ids {
		if i > 0 {
			buf.WriteByte('\n')
		}
		s := servers[id]
		fmt.Fprintf(&buf, "[mcp_servers.%s]\n", tomlKey(id))
		if s.Command != "" {
			fmt.Fprintf(&buf, "command = %s\n", tomlString(s.Command))
		}
		if len(s.Args) > 0 {
			fmt.Fprintf(&buf, "args = %s\n", tomlStringArray(s.Args))
		}
		if len(s.Env) > 0 {
			fmt.Fprintf(&buf, "env = %s\n", tomlStringMap(s.Env))
		}
		if s.URL != "" {
			fmt.Fprintf(&buf, "url = %s\n", tomlString(s.URL))
		}
		if len(s.Headers) > 0 {
			fmt.Fprintf(&buf, "headers = %s\n", tomlStringMap(s.Headers))
		}
	}
	return []byte(buf.String()), nil
}

// tomlKey renders a TOML key, quoting it as a basic string unless every
// character is bare-key safe ([A-Za-z0-9_-]) — a server id or an env var name
// with any other character (a space, a dot) would otherwise produce invalid
// TOML or silently change the key's meaning (a bare "a.b" is two nested
// tables, not one key named "a.b").
func tomlKey(s string) string {
	if isBareKey(s) {
		return s
	}
	return tomlString(s)
}

func isBareKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// tomlString renders s as a TOML basic string: quoted, with backslashes,
// quotes and control characters escaped. This is the writer's one
// correctness-sensitive piece — see TestRenderCodexTOMLEscapesReservedStrings.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlStringArray renders a TOML array of strings. Order is preserved, never
// sorted — these are command-line arguments, and their order is semantic.
func tomlStringArray(vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = tomlString(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlStringMap renders a TOML inline table of string values, key-sorted —
// unlike an argument list, Env and Headers are Go maps with no order of their
// own, so ranging them straight into output would be nondeterministic.
func tomlStringMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = tomlKey(k) + " = " + tomlString(m[k])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}
