// Package mcpcatalog is the shipped MCP server catalogue: the vocabulary one
// entry is declared in (Stability, Transport, Server, Descriptor) and the
// registry that holds every entry.
//
// It follows the source connector precedent (connector.Descriptor,
// internal/app/sources/connector) but carries no config factory: a shipped
// entry has no per-workspace configuration in M1, so there is nothing for a
// factory to construct. It is deliberately a leaf — a workspace names an MCP
// only by its Type string (agentws.Workspace.MCPs []string), so this package
// must never import agentws back.
package mcpcatalog

// ENUM(experimental, beta, stable)
type Stability string

// Transport is how a client reaches an MCP server. It is the discriminator
// .mcp.json carries.
//
// ENUM(stdio, http, sse)
type Transport string

// Server is one MCP server's resolved launch declaration — the exact thing
// the catalogue shows a user before anything is enabled (spec §7.3) and the
// exact thing the generator writes into .mcp.json.
type Server struct {
	Transport Transport
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Headers   map[string]string
}

// Descriptor declares one shipped MCP server type. Static data with no
// dependencies, which is what lets the registry hold it as package state.
type Descriptor struct {
	Type        string
	Title       string
	Description string
	Icon        string
	Stability   Stability
	// Server is the fixed launch declaration. A shipped entry carries no
	// configuration in M1 and therefore no factory: a workspace names an MCP
	// only as a string (Workspace.MCPs []string), so there is nothing a config
	// could ever be decoded from.
	Server Server
}
