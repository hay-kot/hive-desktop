package dispatch

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colonyops/hive/pkg/tmpl"
)

// CreateSessionRequest is a user-submitted New Session form.
type CreateSessionRequest struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	Agent      string `json:"agent,omitempty"`
}

// SessionDraft is a New Session form prefilled from an inbox item.
type SessionDraft struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
}

// DefaultSessionPromptTemplate renders an inbox item into the starting prompt
// for a session created from it. Repo is omitted; it prefills the form's own
// repository field.
const DefaultSessionPromptTemplate = `{{ .Title }}
{{- if .URL }}

{{ .URL }}
{{- end }}
{{- if .Body }}

{{ .Body }}
{{- end }}`

// SessionPromptData is the canonical item contract (ADR 0008) the session
// prompt template renders over. Keep in sync with the frontend's
// canonicalPayload (lib/itemPresentation.ts).
type SessionPromptData struct {
	Title  string
	Kind   string
	Repo   string
	Num    int
	Author string
	Body   string
	URL    string
	Labels []string
	State  string
}

// RenderSessionDraft projects a persisted inbox item into a New Session draft.
func RenderSessionDraft(title, url string, payload []byte) (SessionDraft, error) {
	data := sessionPromptData(title, url, payload)

	prompt, err := tmpl.New(tmpl.Config{}).Render(DefaultSessionPromptTemplate, data)
	if err != nil {
		return SessionDraft{}, fmt.Errorf("render session prompt: %w", err)
	}

	return SessionDraft{
		Repository: data.Repo,
		Name:       SlugifySessionName(data.Title),
		Prompt:     strings.TrimSpace(prompt),
	}, nil
}

func sessionPromptData(title, url string, payload []byte) SessionPromptData {
	data := SessionPromptData{Title: strings.TrimSpace(title), URL: url}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return data
	}

	str := func(key string) string {
		var s string
		_ = json.Unmarshal(fields[key], &s)
		return s
	}
	data.Kind = str("kind")
	data.Repo = str("repo")
	data.Author = str("author")
	data.Body = str("body")
	data.State = str("state")
	if u := str("url"); u != "" {
		data.URL = u
	}
	_ = json.Unmarshal(fields["num"], &data.Num)
	_ = json.Unmarshal(fields["labels"], &data.Labels)
	return data
}
