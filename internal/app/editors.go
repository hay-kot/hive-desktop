package app

import (
	"context"
	"os/exec"
)

// EditorChoice is one editor the Settings selector offers: a CLI command, the
// display title it is shown as, and whether the command resolves on the
// subprocess PATH right now.
type EditorChoice struct {
	Command string
	Title   string
	Found   bool
}

// knownEditors is the catalogue behind the Settings selector — each one a CLI
// launcher that takes a directory argument and returns immediately. The
// setting stores the command, not an enum, so a YAML-authored command outside
// this list still works; it just labels itself.
var knownEditors = []EditorChoice{
	{Command: "zed", Title: "Zed"},
	{Command: "code", Title: "VS Code"},
	{Command: "cursor", Title: "Cursor"},
	{Command: "subl", Title: "Sublime Text"},
}

// editorTitle maps a configured command onto its display title, falling back
// to the command itself for one outside the known catalogue.
func editorTitle(command string) string {
	for _, e := range knownEditors {
		if e.Command == command {
			return e.Title
		}
	}
	return command
}

// detectEditors reports the known catalogue with each command resolved
// through lookPath — the resolver's, so the answer matches what a launch
// would actually find (ADR 0041), not the desktop process's own PATH.
func detectEditors(ctx context.Context, lookPath func(context.Context, string) (string, error)) []EditorChoice {
	if lookPath == nil {
		lookPath = func(_ context.Context, name string) (string, error) { return exec.LookPath(name) }
	}
	choices := make([]EditorChoice, len(knownEditors))
	for i, e := range knownEditors {
		_, err := lookPath(ctx, e.Command)
		e.Found = err == nil
		choices[i] = e
	}
	return choices
}
