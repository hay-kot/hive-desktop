package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/cmd/internal/devproxy"
)

// Control is the devserver's own API: everything that drives the simulation,
// as opposed to the proxy's GitHub-shaped surface. It is mounted under /_ctl/
// so it cannot collide with a GitHub path.
type Control struct {
	cfg    Config
	store  *Store
	cache  *Cache
	proxy  *Proxy
	pusher *Pusher
	logger zerolog.Logger

	mu      sync.Mutex
	running map[string]bool
}

func NewControl(cfg Config, store *Store, cache *Cache, proxy *Proxy, pusher *Pusher, logger zerolog.Logger) *Control {
	return &Control{
		cfg: cfg, store: store, cache: cache, proxy: proxy, pusher: pusher,
		logger: logger, running: make(map[string]bool),
	}
}

// Handler returns the control routes.
func (c *Control) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+devproxy.HealthPath, handleHealth)
	mux.HandleFunc("GET /_ctl/state", c.handleState)
	mux.HandleFunc("POST /_ctl/overlay", c.handleSetOverlay)
	mux.HandleFunc("POST /_ctl/overlay/clear", c.handleClearOverlay)
	mux.HandleFunc("POST /_ctl/overlays/clear", c.handleClearAll)
	mux.HandleFunc("POST /_ctl/action", c.handleAction)
	mux.HandleFunc("POST /_ctl/scenarios/{name}/run", c.handleRunScenario)
	mux.HandleFunc("POST /_ctl/webhooks/push", c.handlePush)
	mux.HandleFunc("POST /_ctl/cache/purge", c.handlePurge)
	return mux
}

// StateView is the dashboard's single read. One endpoint keeps the page a
// plain poll rather than a fan of requests that can disagree with each other.
type StateView struct {
	Upstream  string               `json:"upstream"`
	CacheTTL  string               `json:"cacheTTL"`
	Entries   int                  `json:"cacheEntries"`
	Stats     Stats                `json:"stats"`
	Items     []Item               `json:"items"`
	Overlays  map[string]Mutations `json:"overlays"`
	Scenarios []ScenarioView       `json:"scenarios"`
	Targets   []WebhookTarget      `json:"targets"`
	Payloads  []string             `json:"payloads"`
	Recent    []LogEntry           `json:"recent"`
	Pushes    []PushResult         `json:"pushes"`
	Actions   []ActionView         `json:"actions"`
}

// ScenarioView is one configured scenario and whether it is mid-run.
type ScenarioView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       int    `json:"steps"`
	Running     bool   `json:"running"`
}

// ActionView describes one available quick action, so the dashboard renders
// whatever the vocabulary contains rather than hardcoding a parallel list.
type ActionView struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

func (c *Control) handleState(w http.ResponseWriter, r *http.Request) {
	stats, recent := c.proxy.Snapshot()

	scenarios := make([]ScenarioView, 0, len(c.cfg.Scenarios))
	c.mu.Lock()
	for name, scenario := range c.cfg.Scenarios {
		scenarios = append(scenarios, ScenarioView{
			Name: name, Description: scenario.Description,
			Steps: len(scenario.Steps), Running: c.running[name],
		})
	}
	c.mu.Unlock()
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Name < scenarios[j].Name })

	actions := make([]ActionView, 0, len(actionOrder))
	for _, name := range actionOrder {
		actions = append(actions, ActionView{Name: name, Label: quickActions[name].label})
	}

	writeJSON(w, http.StatusOK, StateView{
		Upstream:  c.cfg.Upstream,
		CacheTTL:  c.cfg.Cache.TTL.String(),
		Entries:   c.cache.Entries(),
		Stats:     stats,
		Items:     c.store.Items(),
		Overlays:  c.store.Overlays(),
		Scenarios: scenarios,
		Targets:   c.pusher.Targets(),
		Payloads:  c.pusher.PayloadNames(),
		Recent:    recent,
		Pushes:    c.pusher.Recent(),
		Actions:   actions,
	})
}

// handleHealth answers the duplicate-launch and preflight probes. It reads no
// state, so it stays truthful about "a devserver owns this port" even if the
// overlay store or cache is busy.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, devproxy.Health{Devserver: true})
}

// overlayRequest is the body of POST /_ctl/overlay.
type overlayRequest struct {
	Repo string    `json:"repo"`
	Num  int       `json:"num"`
	Set  Mutations `json:"set"`
}

func (c *Control) handleSetOverlay(w http.ResponseWriter, r *http.Request) {
	var req overlayRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	match := Matcher{Repo: req.Repo, Num: req.Num}
	if !match.Valid() {
		writeError(w, http.StatusBadRequest, "repo must be \"owner/name\" and num must be positive")
		return
	}
	if err := validateMutations(req.Set); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	merged := c.store.Apply(match.Key(), req.Set)
	c.logger.Info().Str("item", match.Key()).Msg("overlay applied")
	writeJSON(w, http.StatusOK, map[string]any{"item": match.Key(), "overlay": merged})
}

func (c *Control) handleClearOverlay(w http.ResponseWriter, r *http.Request) {
	var req overlayRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	match := Matcher{Repo: req.Repo, Num: req.Num}
	if !match.Valid() {
		writeError(w, http.StatusBadRequest, "repo must be \"owner/name\" and num must be positive")
		return
	}
	c.store.Clear(match.Key())
	writeJSON(w, http.StatusOK, map[string]any{"item": match.Key(), "cleared": true})
}

func (c *Control) handleClearAll(w http.ResponseWriter, r *http.Request) {
	c.store.ClearAll()
	c.logger.Info().Msg("all overlays cleared")
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}

// ── Quick actions ────────────────────────────────────────────────────────────
//
// The vocabulary is deliberately GitHub's own, not an invented one. The
// desktop's sources.github reads exactly four things — state, updatedAt,
// labels, and a notification reason — so those are the only levers that exist.
// In particular there is no "approved" action: GitHub has no such notification
// reason, and the desktop never fetches review state. An approval reaches the
// app as activity on the item, which is what these reasons produce.

type quickAction struct {
	label string
	apply func() Mutations
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }

var quickActions = map[string]quickAction{
	"review-requested":   {"Review requested", func() Mutations { return Mutations{Reason: stringPtr("review_requested")} }},
	"approval-requested": {"Approval requested", func() Mutations { return Mutations{Reason: stringPtr("approval_requested")} }},
	"comment":            {"New comment", func() Mutations { return Mutations{Reason: stringPtr("comment")} }},
	"ci-activity":        {"CI status changed", func() Mutations { return Mutations{Reason: stringPtr("ci_activity")} }},
	"mention":            {"Mentioned", func() Mutations { return Mutations{Reason: stringPtr("mention")} }},
	"state-change":       {"State changed", func() Mutations { return Mutations{Reason: stringPtr("state_change")} }},
	// Terminal transitions also mark the item absent from search results,
	// because that is how GitHub behaves once it leaves an is:open query — and
	// it is the only way to exercise the desktop's ConfirmAbsence path.
	"merge":  {"Merge", func() Mutations { return Mutations{State: stringPtr("merged"), Absent: boolPtr(true)} }},
	"close":  {"Close", func() Mutations { return Mutations{State: stringPtr("closed"), Absent: boolPtr(true)} }},
	"reopen": {"Reopen", func() Mutations { return Mutations{State: stringPtr("open"), Absent: boolPtr(false)} }},
	"draft":  {"Mark draft", func() Mutations { return Mutations{Draft: boolPtr(true)} }},
	"ready":  {"Mark ready", func() Mutations { return Mutations{Draft: boolPtr(false)} }},
}

// actionOrder fixes the dashboard's button order; map iteration would shuffle
// them on every poll.
var actionOrder = []string{
	"review-requested", "approval-requested", "comment", "ci-activity", "mention",
	"state-change", "draft", "ready", "reopen", "close", "merge",
}

type actionRequest struct {
	Repo   string `json:"repo"`
	Num    int    `json:"num"`
	Action string `json:"action"`
}

func (c *Control) handleAction(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	match := Matcher{Repo: req.Repo, Num: req.Num}
	if !match.Valid() {
		writeError(w, http.StatusBadRequest, "repo must be \"owner/name\" and num must be positive")
		return
	}
	action, ok := quickActions[req.Action]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown action %q", req.Action))
		return
	}
	merged := c.store.Apply(match.Key(), action.apply())
	c.logger.Info().Str("item", match.Key()).Str("action", req.Action).Msg("action applied")
	writeJSON(w, http.StatusOK, map[string]any{"item": match.Key(), "overlay": merged})
}

// ── Scenarios ────────────────────────────────────────────────────────────────

func (c *Control) handleRunScenario(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	scenario, ok := c.cfg.Scenarios[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no scenario named %q", name))
		return
	}

	c.mu.Lock()
	if c.running[name] {
		c.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf("scenario %q is already running", name))
		return
	}
	c.running[name] = true
	c.mu.Unlock()

	// Scenarios have waits between steps, so the run outlives the request. It
	// gets a background context for the same reason: the response returns
	// immediately and cancelling it must not abort the scenario.
	go c.runScenario(context.Background(), name, scenario)
	writeJSON(w, http.StatusAccepted, map[string]any{"scenario": name, "steps": len(scenario.Steps)})
}

func (c *Control) runScenario(ctx context.Context, name string, scenario Scenario) {
	defer func() {
		c.mu.Lock()
		delete(c.running, name)
		c.mu.Unlock()
	}()

	c.logger.Info().Str("scenario", name).Int("steps", len(scenario.Steps)).Msg("scenario started")
	for i, step := range scenario.Steps {
		if !step.Set.Empty() {
			c.store.Apply(step.Match.Key(), step.Set)
			c.logger.Info().Str("scenario", name).Int("step", i).
				Str("item", step.Match.Key()).Msg("scenario step applied")
		}
		if step.Wait > 0 {
			select {
			case <-ctx.Done():
				c.logger.Warn().Str("scenario", name).Msg("scenario cancelled")
				return
			case <-time.After(step.Wait):
			}
		}
	}
	c.logger.Info().Str("scenario", name).Msg("scenario finished")
}

// ── Webhook pushes ───────────────────────────────────────────────────────────

type pushRequest struct {
	Target string `json:"target"`
	// Payload names a configured payload; Body supplies one inline. Exactly
	// one is required.
	Payload   string         `json:"payload,omitempty"`
	Body      map[string]any `json:"body,omitempty"`
	Overrides map[string]any `json:"overrides,omitempty"`
}

func (c *Control) handlePush(w http.ResponseWriter, r *http.Request) {
	var req pushRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if (req.Payload == "") == (req.Body == nil) {
		writeError(w, http.StatusBadRequest, "provide exactly one of payload or body")
		return
	}

	var (
		result PushResult
		err    error
	)
	if req.Payload != "" {
		result, err = c.pusher.PushNamed(r.Context(), req.Target, req.Payload, req.Overrides)
	} else {
		for key, value := range req.Overrides {
			req.Body[key] = value
		}
		result, err = c.pusher.Push(r.Context(), req.Target, "inline", req.Body)
	}
	if err != nil {
		// The delivery attempt itself is the useful output even when it
		// failed, so it is returned rather than collapsed into a bare error.
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "result": result})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (c *Control) handlePurge(w http.ResponseWriter, r *http.Request) {
	if err := c.cache.Purge(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.logger.Info().Msg("cache purged")
	writeJSON(w, http.StatusOK, map[string]any{"purged": true})
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// decodeJSON decodes a request body, writing a 400 and reporting false when it
// cannot. Unknown fields are rejected so a typo in a hand-written curl call
// fails loudly instead of silently doing nothing.
func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
