package agentws

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
)

// LoadWorkspace reads and validates one agent-workspace.yaml. Dir is set from
// path's parent directory name, so every successfully loaded Workspace knows
// which directory under the root it came from. A missing file returns an
// error wrapping os.ErrNotExist, which the store uses to distinguish "no
// manifest yet" (ignored) from a genuinely broken one (kept as an error).
func LoadWorkspace(path string) (Workspace, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Workspace{}, fmt.Errorf("read agent-workspace.yaml %s: %w", path, err)
	}
	data, _, err := configmigrate.AgentWorkspaceSet.Apply(raw)
	if err != nil {
		return Workspace{}, fmt.Errorf("agent-workspace.yaml %s: %w", path, err)
	}
	if data == nil {
		data = raw
	}
	w, err := parseWorkspace(data)
	if err != nil {
		return Workspace{}, fmt.Errorf("agent-workspace.yaml %s: %w", path, err)
	}
	w.Dir = filepath.Base(filepath.Dir(path))
	return w, nil
}

// parseWorkspace strictly decodes an agent-workspace.yaml document already in
// hand (rather than read from disk), checks version ==
// configmigrate.AgentWorkspaceSet.Current, and validates it. It is the
// non-migrating counterpart to LoadWorkspace, for bytes that never touched a
// file — an unsaved edit checked before it is written, say.
func parseWorkspace(data []byte) (Workspace, error) {
	var w Workspace
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&w); err != nil && !errors.Is(err, io.EOF) {
		return Workspace{}, fmt.Errorf("agent-workspace.yaml: %w", err)
	}

	if w.Version != configmigrate.AgentWorkspaceSet.Current {
		return Workspace{}, fmt.Errorf("agent-workspace.yaml: version must be %d, got %d", configmigrate.AgentWorkspaceSet.Current, w.Version)
	}
	// An omitted autonomy defaults to the least-trusting posture (hc-ou4o02zx
	// §4) now that the M2 approval indicator makes "ask" legible in the area,
	// rather than failing to load at all.
	if w.Autonomy == "" {
		w.Autonomy = AutonomyAsk
	}
	if err := w.Validate(); err != nil {
		return Workspace{}, err
	}
	return w, nil
}

// LoadLibrary reads and validates mcps.yaml at path.
func LoadLibrary(path string) (Library, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Library{}, fmt.Errorf("read mcps.yaml %s: %w", path, err)
	}
	data, _, err := configmigrate.MCPLibrarySet.Apply(raw)
	if err != nil {
		return Library{}, fmt.Errorf("mcps.yaml %s: %w", path, err)
	}
	if data == nil {
		data = raw
	}
	lib, err := parseLibrary(data)
	if err != nil {
		return Library{}, fmt.Errorf("mcps.yaml %s: %w", path, err)
	}
	return lib, nil
}

// parseLibrary strictly decodes an mcps.yaml document already in hand, checks
// version == configmigrate.MCPLibrarySet.Current, and validates it.
func parseLibrary(data []byte) (Library, error) {
	var l Library
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil && !errors.Is(err, io.EOF) {
		return Library{}, fmt.Errorf("mcps.yaml: %w", err)
	}

	if l.Version != configmigrate.MCPLibrarySet.Current {
		return Library{}, fmt.Errorf("mcps.yaml: version must be %d, got %d", configmigrate.MCPLibrarySet.Current, l.Version)
	}
	if err := l.Validate(); err != nil {
		return Library{}, err
	}
	return l, nil
}
