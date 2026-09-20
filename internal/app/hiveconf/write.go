package hiveconf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Edit is the set of Hive config fields the desktop owns: the workspaces the
// session launcher scans, the agent profiles it offers, and which of them is
// the default. Everything else the file says — comments, key order, hive
// settings this app has no opinion on — survives a write untouched.
//
// Profiles and Workspaces are each the whole set, not a delta: the editor
// holds every entry while it is open, so a write reconciles the file to
// exactly what it was given (the agentws/write.go pattern).
type Edit struct {
	DefaultAgent string
	Profiles     []Profile
	Workspaces   []string
}

// reservedAgentKeys are the keys inside hive's `agents:` mapping that are not
// a profile. Reconciling profiles must step over them, or a write would delete
// the user's agent_selector setting — or, for "<<", the YAML merge key that
// pulls their shared profile block in, which is not this app's to remove
// however unfamiliar the profiles it names are.
var reservedAgentKeys = map[string]bool{"default": true, "agent_selector": true, "<<": true}

// InvalidEditError is an edit the user can fix, as opposed to a disk that
// would not take the write. It is a distinct type so a caller classifies it
// with errors.As rather than by reading the message.
type InvalidEditError struct{ Reason string }

func (e InvalidEditError) Error() string { return e.Reason }

func invalid(format string, args ...any) error {
	return InvalidEditError{Reason: fmt.Sprintf(format, args...)}
}

// Validate rejects an edit that would produce a config hive refuses to load.
// The load is all-or-nothing — hive's validateAgents fails the whole file when
// agents.default names no profile — so a bad write does not degrade the app,
// it stops it starting. This runs before any file is touched.
func (e Edit) Validate() error {
	if len(e.Profiles) == 0 {
		return invalid("choose at least one agent")
	}
	seen := make(map[string]bool, len(e.Profiles))
	for _, p := range e.Profiles {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return invalid("an agent profile needs a name")
		}
		if seen[name] {
			return invalid("%q is listed twice", name)
		}
		seen[name] = true
	}
	if !seen[strings.TrimSpace(e.DefaultAgent)] {
		return invalid("the default agent must be one of the agents you chose")
	}
	if len(e.Workspaces) == 0 {
		return invalid("add at least one folder that holds your repositories")
	}
	for _, w := range e.Workspaces {
		if err := ValidateWorkspace(w); err != nil {
			return invalid("%s: %s", w, err)
		}
	}
	return nil
}

// Apply writes the edit to path, creating the file when it is absent and
// editing the existing document in place when it is not.
//
// The two paths differ because the files do: a file this app creates is
// authored for a reader who has never seen one, comments included, while a
// file the user already has is theirs — the write reaches in for the two keys
// it owns and leaves the rest byte-for-byte.
func Apply(path string, edit Edit) error {
	if err := edit.Validate(); err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("no Hive config path is available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create the Hive config directory: %w", err)
	}

	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return writeFileAtomic(path, []byte(render(edit)))
	case err != nil:
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	out, err := editDocument(raw, edit)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out)
}

// editDocument applies the edit to an existing document's node tree.
func editDocument(raw []byte, edit Edit) ([]byte, error) {
	doc := &yaml.Node{}
	if err := yaml.Unmarshal(raw, doc); err != nil {
		return nil, fmt.Errorf("parse the Hive config: %w", err)
	}
	// An empty file parses to a document with no content. Seed a mapping so
	// the first write into a file `hive` created but never filled behaves the
	// same as a write into a file with keys already in it.
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the Hive config is not a YAML mapping")
	}

	var workspaces yaml.Node
	if err := workspaces.Encode(edit.Workspaces); err != nil {
		return nil, fmt.Errorf("encode workspaces: %w", err)
	}
	setNode(root, "workspaces", &workspaces)
	// A file still on hive's deprecated spelling would otherwise keep a
	// repo_dirs list that now disagrees with the workspaces beside it. Hive
	// reads repo_dirs only when workspaces is empty, so the stale key is not
	// merely redundant — it is what a downgrade would read.
	removeKey(root, "repo_dirs")

	agents := nodeFor(root, "agents")
	if agents == nil || agents.Kind != yaml.MappingNode {
		agents = &yaml.Node{Kind: yaml.MappingNode}
		setNode(root, "agents", agents)
	}
	if err := reconcileAgents(agents, edit); err != nil {
		return nil, err
	}

	return encodeDoc(doc)
}

// reconcileAgents makes the agents mapping declare exactly the edit's
// profiles, preserving each surviving profile's own node so a key this build
// does not know about stays where the user put it.
func reconcileAgents(agents *yaml.Node, edit Edit) error {
	if err := setScalar(agents, "default", edit.DefaultAgent); err != nil {
		return err
	}
	keep := make(map[string]bool, len(edit.Profiles))
	for _, p := range edit.Profiles {
		keep[p.Name] = true
	}
	for i := 0; i+1 < len(agents.Content); {
		key := agents.Content[i].Value
		if reservedAgentKeys[key] || keep[key] {
			i += 2
			continue
		}
		agents.Content = append(agents.Content[:i], agents.Content[i+2:]...)
	}
	for _, p := range edit.Profiles {
		profile := nodeFor(agents, p.Name)
		if profile == nil || profile.Kind != yaml.MappingNode {
			profile = &yaml.Node{Kind: yaml.MappingNode}
			setNode(agents, p.Name, profile)
		}
		command := p.Command
		if command == "" {
			command = p.Name
		}
		if err := setScalar(profile, "command", command); err != nil {
			return err
		}
		// An empty flags list is written rather than dropped: it is the
		// visible statement that this agent runs with no extra arguments,
		// which is what someone turning skip-permissions back off expects to
		// see in the file.
		flags := p.Flags
		if flags == nil {
			flags = []string{}
		}
		var node yaml.Node
		if err := node.Encode(flags); err != nil {
			return fmt.Errorf("encode %s flags: %w", p.Name, err)
		}
		node.Style = yaml.FlowStyle
		setNode(profile, "flags", &node)
	}
	return nil
}

const header = `# Hive configuration
#
# The ` + "`hive`" + ` CLI and Hive Desktop share this file. Hive Desktop wrote the
# workspaces and agents below during setup; both can be changed there again,
# under Settings > Hive CLI, or edited here by hand.
#
# Everything else Hive supports — rules, tmux integration, keybindings, user
# commands — can be added to this file and is left alone by Hive Desktop.
# Reference: https://hive.colonyops.io/configuration
`

// render produces the file a first run writes. It is a template rather than an
// encoded node tree because the comments are the point: this is the first and
// often only time the user sees what the file is for.
func render(edit Edit) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n# Parent folders that hold your git repositories. Point these at the\n")
	b.WriteString("# folder your repos sit in, for example ~/code — not at a repository.\n")
	b.WriteString("workspaces:\n")
	for _, w := range edit.Workspaces {
		fmt.Fprintf(&b, "  - %s\n", scalar(w))
	}
	b.WriteString("\n# The agents Hive can start a session with.\n")
	b.WriteString("agents:\n")
	fmt.Fprintf(&b, "  default: %s\n", scalar(edit.DefaultAgent))
	for _, p := range edit.Profiles {
		command := p.Command
		if command == "" {
			command = p.Name
		}
		fmt.Fprintf(&b, "  %s:\n", scalar(p.Name))
		fmt.Fprintf(&b, "    command: %s\n", scalar(command))
		fmt.Fprintf(&b, "    flags: %s\n", flagSequence(p.Flags))
	}
	return b.String()
}

// scalar renders s as a YAML scalar, quoted when its content would otherwise
// change meaning. A path is ordinary text right up until it contains a colon.
func scalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return strings.TrimRight(string(out), "\n")
}

func flagSequence(flags []string) string {
	if len(flags) == 0 {
		return "[]"
	}
	node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, f := range flags {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: f})
	}
	out, err := yaml.Marshal(node)
	if err != nil {
		return "[]"
	}
	return strings.TrimRight(string(out), "\n")
}

// The node helpers below are the flow/yamldoc.go pattern, kept package-local
// for the same reason it keeps its own: document editing is self-contained,
// and a shared helper package would couple three unrelated file formats.

func nodeFor(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// setNode sets mapping[key] = value, appending the pair when key is absent.
// Replacing only the value node is what keeps the key's own head comment
// attached to it.
func setNode(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

func setScalar(mapping *yaml.Node, key string, value any) error {
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	setNode(mapping, key, &v)
	return nil
}

func removeKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

func encodeDoc(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode the Hive config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode the Hive config: %w", err)
	}
	return buf.Bytes(), nil
}

// writeFileAtomic writes via temp-file-then-rename so a crash mid-write never
// leaves a half-written config for the next launch to fail on. 0o644, not the
// 0o600 the desktop's own files use: this file is shared with the `hive` CLI
// and carries no secret.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // shared with the hive CLI, holds no secret
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
