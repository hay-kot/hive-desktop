package agentws

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
)

// This file is the manifest writer the Migration Notes reserved: it edits the
// parsed yaml.Node tree in place (the flow/yamldoc.go pattern) so a
// hand-authored agent-workspace.yaml keeps its comments, key order, and any
// keys this build does not know. A yaml.Marshal round-trip would destroy all
// three, which is why Workspace itself stays read-only. The node helpers are
// deliberately package-local, the same self-containment flow and actions keep.

// CreateWorkspace makes dir under root and writes its first manifest. The
// directory already existing is returned as-is so the caller can classify it
// (errors.Is(err, fs.ErrExist)).
func CreateWorkspace(root, dir, name, agent string, autonomy Autonomy) error {
	if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
		return err
	}
	return WriteManifest(root, dir, name, agent, autonomy)
}

// WriteManifest sets exactly name, agent, and autonomy in dir's
// agent-workspace.yaml, creating a fresh version-current document when the
// file does not exist and editing the existing document in place when it
// does — everything else the file says survives.
func WriteManifest(root, dir, name, agent string, autonomy Autonomy) error {
	path := filepath.Join(root, dir, manifestFileName)

	var doc, mapping *yaml.Node
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		mapping = &yaml.Node{Kind: yaml.MappingNode}
		doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}
		if err := setManifestValue(mapping, "version", configmigrate.AgentWorkspaceSet.Current); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("agent-workspace.yaml: %w", err)
	default:
		doc, mapping, err = parseManifestNode(raw)
		if err != nil {
			return err
		}
	}

	for _, field := range []struct {
		key   string
		value any
	}{
		{"name", name},
		{"agent", agent},
		{"autonomy", string(autonomy)},
	} {
		if err := setManifestValue(mapping, field.key, field.value); err != nil {
			return err
		}
	}

	out, err := encodeManifestDoc(doc)
	if err != nil {
		return err
	}
	return writeManifestAtomic(path, out)
}

func parseManifestNode(data []byte) (doc, mapping *yaml.Node, err error) {
	doc = &yaml.Node{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, nil, fmt.Errorf("agent-workspace.yaml: parse: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("agent-workspace.yaml: document is not a mapping")
	}
	return doc, doc.Content[0], nil
}

// setManifestValue encodes value and sets it at mapping[key], appending the
// pair when key is absent. Replacing only the value node is what keeps the
// key's own head comment attached.
func setManifestValue(mapping *yaml.Node, key string, value any) error {
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return fmt.Errorf("agent-workspace.yaml: encode %s: %w", key, err)
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = &v
			return nil
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &v)
	return nil
}

func encodeManifestDoc(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("agent-workspace.yaml: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("agent-workspace.yaml: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// writeManifestAtomic writes via temp-file-then-rename so a crash mid-write
// never leaves a half-written manifest for the watcher to load.
func writeManifestAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("agent-workspace.yaml: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("agent-workspace.yaml: replace: %w", err)
	}
	return nil
}
