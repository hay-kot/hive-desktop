package agentws

import (
	"fmt"
	"os/exec"
	"sort"

	"github.com/hay-kot/hive-desktop/internal/app/mcpcatalog"
)

// CatalogueEntry is one row of the merged MCP catalogue: a shipped registry
// entry, a user library entry, or a user entry that replaces a shipped one.
type CatalogueEntry struct {
	ID          string
	Title       string
	Description string
	Shipped     bool
	Stability   mcpcatalog.Stability
	// Shadows is the shipped id this user entry replaces, empty otherwise.
	Shadows string
	// Server is the resolved launch declaration. Nothing is enabled whose
	// command line the user cannot read first (spec §7.3).
	Server mcpcatalog.Server
	// Problem reports why this entry will not work — today, a stdio Command
	// that exec.LookPath cannot resolve. Showing a resolved command that does
	// not resolve is worse than showing nothing: the agent starts, the MCP
	// silently fails to connect, and the failure surfaces only inside the
	// agent's own /mcp output.
	Problem string
}

// Catalogue merges the shipped registry with the user library into one list,
// sorted by id. A user entry shadowing a shipped id wins and says so — the
// same rule the generator applies to .shared/ (D-D), stated once for the
// whole feature. It reads no store state beyond lib: a free function tests
// without standing up a store over a temp root.
//
// The LookPath check runs here, at catalogue-render time, never inside
// Generate — PATH is machine state and the generator must stay pure (spec
// §4.4).
func Catalogue(lib Library) []CatalogueEntry {
	shipped := mcpcatalog.All()

	entries := make(map[string]CatalogueEntry, len(shipped)+len(lib.Servers))
	for id, d := range shipped {
		entries[id] = CatalogueEntry{
			ID:          id,
			Title:       d.Title,
			Description: d.Description,
			Shipped:     true,
			Stability:   d.Stability,
			Server:      d.Server,
		}
	}

	for id, srv := range lib.Servers {
		entry := CatalogueEntry{
			ID:     id,
			Title:  srv.Title,
			Server: serverFromMCPServer(srv),
		}
		if d, ok := shipped[id]; ok {
			entry.Shadows = id
			entry.Stability = d.Stability
			entry.Description = d.Description
			if entry.Title == "" {
				entry.Title = d.Title
			}
		}
		entries[id] = entry
	}

	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]CatalogueEntry, 0, len(ids))
	for _, id := range ids {
		entry := entries[id]
		entry.Problem = problemFor(entry.Server)
		out = append(out, entry)
	}
	return out
}

func serverFromMCPServer(s MCPServer) mcpcatalog.Server {
	return mcpcatalog.Server{
		Transport: s.effectiveType(),
		Command:   s.Command,
		Args:      s.Args,
		Env:       s.Env,
		URL:       s.URL,
		Headers:   s.Headers,
	}
}

// problemFor reports why a resolved server will not work, or "" when it is
// fine. Only a stdio Command is checked: http/sse servers have nothing local
// to resolve.
func problemFor(s mcpcatalog.Server) string {
	if s.Transport != mcpcatalog.TransportStdio || s.Command == "" {
		return ""
	}
	if _, err := exec.LookPath(s.Command); err != nil {
		return fmt.Sprintf("command %q not found on PATH", s.Command)
	}
	return ""
}
