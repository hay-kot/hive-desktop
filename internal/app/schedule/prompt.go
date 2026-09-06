package schedule

import (
	"fmt"
	"strings"
	"text/template"
	"time"
)

// PromptData is what a prompt template can reach. It is a flat record on
// purpose: a template that reads through the app's own types would break every
// time one of them changed.
type PromptData struct {
	Schedule struct {
		ID, Name, Cron string
	}
	Workspace struct {
		Dir, Name string
	}
	Now          time.Time
	ScheduledFor time.Time
	Reason       string
	Missed       int
	// LastRun is the ScheduledFor of the previous launched run, nil on the
	// first. The date func takes it directly so a template does not have to
	// guard the nil itself.
	LastRun *time.Time
}

// promptFuncs is the template function set. It stays this small deliberately:
// every function here is a promise to keep working for prompts users have
// already written.
var promptFuncs = template.FuncMap{"date": formatDate}

// formatDate renders t with a Go layout. It takes any rather than time.Time
// because {{ date "2006-01-02" .LastRun }} passes a *time.Time, and
// text/template errors on dereferencing a nil one before the function is ever
// called. A nil or unrecognized value renders empty so a first run's template
// still executes.
func formatDate(layout string, value any) string {
	switch t := value.(type) {
	case time.Time:
		return t.Format(layout)
	case *time.Time:
		if t == nil {
			return ""
		}
		return t.Format(layout)
	default:
		return ""
	}
}

// RenderPrompt executes a schedule's prompt template.
func RenderPrompt(tmpl string, data PromptData) (string, error) {
	parsed, err := template.New("prompt").Funcs(promptFuncs).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("prompt template: %w", err)
	}
	var out strings.Builder
	if err := parsed.Execute(&out, data); err != nil {
		return "", fmt.Errorf("prompt template: %w", err)
	}
	return out.String(), nil
}

// PromptPreview is a template rendered twice: against the data as given, and
// as the first run sees it, with LastRun unset.
type PromptPreview struct {
	Prompt         string
	FirstRunPrompt string
}

// PreviewPrompt renders a template both ways. A template that only fails on
// the first run, one that calls a method on .LastRun, says so in its error:
// that is the run it would otherwise fail on for real.
func PreviewPrompt(tmpl string, data PromptData) (PromptPreview, error) {
	prompt, err := RenderPrompt(tmpl, data)
	if err != nil {
		return PromptPreview{}, err
	}
	data.LastRun = nil
	firstRun, err := RenderPrompt(tmpl, data)
	if err != nil {
		return PromptPreview{}, fmt.Errorf("%w (on the first run, with .LastRun unset)", err)
	}
	return PromptPreview{Prompt: prompt, FirstRunPrompt: firstRun}, nil
}

// ValidatePrompt reports whether a template parses and executes, with a
// previous run behind it and without one.
func ValidatePrompt(tmpl string) error {
	_, err := PreviewPrompt(tmpl, SamplePromptData())
	return err
}

// SamplePromptData is the stand-in a preview or a validation renders against.
func SamplePromptData() PromptData {
	now := time.Now()
	last := now.Add(-24 * time.Hour)
	data := PromptData{
		Now:          now,
		ScheduledFor: now,
		Reason:       string(ReasonDue),
		Missed:       0,
		LastRun:      &last,
	}
	data.Schedule.ID = "sample"
	data.Schedule.Name = "Sample schedule"
	data.Schedule.Cron = "0 9 * * 5"
	data.Workspace.Dir = "sample-workspace"
	data.Workspace.Name = "Sample workspace"
	return data
}
