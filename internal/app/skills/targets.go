package skills

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

var bodyTemplates = template.Must(template.New("skills").Funcs(funcs()).ParseFS(templatesFS, "templates/*.tmpl"))

// pathAndBody is the pair of Go templates a target renders a file from: pathTmpl
// yields the file's location under the target directory, bodyTmpl its contents.
// Every current agent reads the cross-tool SKILL.md format, so all four share
// these; a future agent whose file shape diverges gets its own pair.
var (
	skillPathTmpl = template.Must(template.New("skillpath").Parse("{{.Name}}/SKILL.md"))
	skillBodyTmpl = bodyTemplates.Lookup("skill.tmpl")
)

// Target is one agent destination for an installed skill. DefaultDir is where its
// skills live when the user sets no override; a leading ~ is expanded at install
// time. The two templates make the file's path and contents data, not code, so
// adding a target is a registry entry — the "install using Go templates for any
// file property" the installer is built around.
type Target struct {
	ID         string
	Label      string
	DefaultDir string
	pathTmpl   *template.Template
	bodyTmpl   *template.Template
}

// targets is the registry, in presentation order. See docs/decisions/0033.
var targets = []Target{
	{ID: "claude", Label: "Claude Code", DefaultDir: "~/.claude/skills", pathTmpl: skillPathTmpl, bodyTmpl: skillBodyTmpl},
	{ID: "codex", Label: "OpenAI Codex", DefaultDir: "~/.codex/skills", pathTmpl: skillPathTmpl, bodyTmpl: skillBodyTmpl},
	{ID: "pi", Label: "Pi Coding Agent", DefaultDir: "~/.pi/agent/skills", pathTmpl: skillPathTmpl, bodyTmpl: skillBodyTmpl},
	{ID: "agents", Label: "Agent Skills (portable)", DefaultDir: "~/.agents/skills", pathTmpl: skillPathTmpl, bodyTmpl: skillBodyTmpl},
}

// Targets returns the registry as a copy so callers cannot mutate it.
func Targets() []Target { return append([]Target(nil), targets...) }

// TargetByID looks a target up by id. The second result is false for an id the
// running build does not know — an index entry written by a newer build, say.
func TargetByID(id string) (Target, bool) {
	for _, t := range targets {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// RelPath is where the skill's file lives under the target directory.
func (t Target) RelPath(s Skill) (string, error) {
	return execute(t.pathTmpl, s)
}

// Render produces the file contents for a skill on this target.
func (t Target) Render(s Skill) (string, error) {
	body, err := execute(t.bodyTmpl, s)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(body) + "\n", nil
}

func execute(tmpl *template.Template, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("skills: render template: %w", err)
	}
	return buf.String(), nil
}

func funcs() template.FuncMap {
	return template.FuncMap{
		// yaml renders a value as a safe single-line double-quoted YAML scalar,
		// so a description containing a colon or quote cannot break the SKILL.md
		// frontmatter it sits in.
		"yaml": yamlScalar,
	}
}

func yamlScalar(s string) string {
	s = strings.ReplaceAll(s, "\\", `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}
