package agentws

import (
	"encoding/json"
	"fmt"
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
	// Bounded reports whether this wiring confines the agent to exactly this
	// set. When false the agent also loads its own global servers, and the UI
	// says so — a posture is only meaningful against a tool set the user can
	// see.
	Bounded bool
}

// AgentLaunch is everything Hive knows about driving one agent CLI. It is
// data, not a branch: a new agent is an entry. This phase carries only the
// MCP half the generator needs; a later phase adds Autonomy, SessionArgs and
// Resume once there is something to launch.
type AgentLaunch struct {
	// MCP is how this agent receives the workspace's servers. A nil MCP means
	// no known project-scoped form at all.
	MCP *MCPWiring
}

// agentLaunches is the launch table, keyed by the agent key a workspace names
// in agent-workspace.yaml. The generator ranges it rather than special-casing
// each agent, so adding one here adds its generated MCP file with no edit to
// generate.go.
var agentLaunches = map[string]AgentLaunch{
	"claude": {
		MCP: &MCPWiring{
			File:    ".mcp.json",
			Render:  renderMCPJSON,
			Bounded: true,
		},
	},
	"codex": {
		MCP: &MCPWiring{
			File:    ".codex/config.toml",
			Render:  renderCodexTOML,
			Bounded: false,
		},
	},
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
