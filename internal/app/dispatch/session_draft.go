package dispatch

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/colonyops/hive/pkg/tmpl"
)

// CreateSessionRequest is a user-submitted New Session form. ItemID is the
// inbox item the form was drafted from, or 0 for a blank one; it is an id
// rather than a ref because the core resolves the item's identity itself and
// never takes it from a client.
type CreateSessionRequest struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	Agent      string `json:"agent,omitempty"`
	ItemID     int64  `json:"itemId,omitempty"`
}

// SessionDraft is a New Session form the app prefills: from an inbox item, or
// from a creation attempt that failed and is being handed back. Agent and
// ItemID are only meaningful for the second, which has to restore both because
// the form they came from is gone.
type SessionDraft struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	Agent      string `json:"agent,omitempty"`
	ItemID     int64  `json:"itemId,omitempty"`
	// nil on a draft that is not a retry, which is also what "no attempt is
	// waiting" looks like to a caller.
	Failure *SessionCreateFailure `json:"failure,omitempty"`
}

// SessionCreateFailure is why one session creation attempt failed.
type SessionCreateFailure struct {
	// Reason is the wrapped error, a chain like
	// "clone repository: git clone: exec git: exit status 1".
	Reason string `json:"reason"`
	// Step is the last thing creation reported: "Cloning repository...",
	// "Executing rules...".
	Step string `json:"step"`
	// Output is the tail of the attempt's progress output, hook output included.
	Output string `json:"output"`
	// CloneStrategy is "full" or "worktree".
	CloneStrategy string `json:"cloneStrategy"`
	// Destination is the checkout hive resolved for the attempt. A clone that
	// fails in a post-checkout hook leaves it complete on disk, and no session
	// record points at it.
	Destination string    `json:"destination"`
	At          time.Time `json:"at"`
}

// Activity-metadata keys for a retryable failed form, the persisted half of
// the retry. RetryKindSessionCreate marks the bag so the Activity view can
// offer the button without interpreting the rest of it; nothing outside this
// file knows these strings.
const (
	RetryMetadataKey       = "retry"
	RetryKindSessionCreate = "session-create"

	metaRepository  = "repository"
	metaName        = "name"
	metaPrompt      = "prompt"
	metaAgent       = "agent"
	metaItemID      = "itemId"
	metaStep        = "step"
	metaReason      = "reason"
	metaDestination = "destination"
)

// SessionDraftMetadata encodes a failed form onto an activity row. The
// progress tail is left out: an audit row is not the place for a hook's
// output, and the ERR line has it.
func SessionDraftMetadata(draft SessionDraft) map[string]string {
	meta := map[string]string{
		RetryMetadataKey: RetryKindSessionCreate,
		metaRepository:   draft.Repository,
		metaName:         draft.Name,
		metaPrompt:       draft.Prompt,
		metaAgent:        draft.Agent,
	}
	if draft.ItemID != 0 {
		meta[metaItemID] = strconv.FormatInt(draft.ItemID, 10)
	}
	if draft.Failure != nil {
		meta[metaStep] = draft.Failure.Step
		meta[metaReason] = draft.Failure.Reason
		meta[metaDestination] = draft.Failure.Destination
	}
	return meta
}

// SessionDraftFromMetadata decodes what SessionDraftMetadata wrote. It reports
// false for a bag that is not a session-create retry or that names no
// repository, so a forged or truncated row prefills nothing.
func SessionDraftFromMetadata(meta map[string]string) (SessionDraft, bool) {
	if meta[RetryMetadataKey] != RetryKindSessionCreate {
		return SessionDraft{}, false
	}
	draft := SessionDraft{
		Repository: meta[metaRepository],
		Name:       meta[metaName],
		Prompt:     meta[metaPrompt],
		Agent:      meta[metaAgent],
	}
	if draft.Repository == "" {
		return SessionDraft{}, false
	}
	draft.ItemID, _ = strconv.ParseInt(meta[metaItemID], 10, 64)
	if meta[metaReason] != "" || meta[metaStep] != "" {
		draft.Failure = &SessionCreateFailure{
			Reason:      meta[metaReason],
			Step:        meta[metaStep],
			Destination: meta[metaDestination],
		}
	}
	return draft, true
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

// SessionPromptData is the canonical item contract (ADR canonical-item-contract) the session
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
		Repository: draftRepository(data.Repo, data.URL),
		Name:       SlugifySessionName(data.Title),
		Prompt:     strings.TrimSpace(prompt),
	}, nil
}

// draftRepository turns an item's canonical repo (owner/name — never a clone
// URL) into a cloneable remote using the item URL's host. Cloning owner/name
// verbatim fails (git exit 128); the derived https URL also shares a
// configured repo's identity, so submitting reuses that checkout when one
// exists. A repo that is already a URL or scp remote passes through; anything
// we cannot turn into a valid remote yields "" so the form defaults instead.
func draftRepository(repo, itemURL string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" || strings.Contains(repo, "://") || strings.Contains(repo, "@") {
		return repo
	}
	if strings.Count(repo, "/") != 1 {
		return ""
	}
	u, err := url.Parse(itemURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return "https://" + u.Hostname() + "/" + repo + ".git"
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
