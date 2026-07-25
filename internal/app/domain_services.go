package app

import (
	"context"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/auth"
	"github.com/hay-kot/hive-desktop/internal/app/jobs"
	"github.com/hay-kot/hive-desktop/internal/app/prompts"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
)

// The four services in this file are thin. They exist so App is the single
// entry point rather than a partial one: an adapter that has to reach past
// the facade for a job list will reach past it for something else next.

// ActionsService owns the editable actions catalog.
type ActionsService struct {
	catalog *actions.ActionStore
	wake    func()
}

func newActionsService(catalog *actions.ActionStore, wake func()) *ActionsService {
	if wake == nil {
		wake = func() {}
	}
	return &ActionsService{catalog: catalog, wake: wake}
}

// List returns the effective last-good catalog plus a current parse error, if
// a hand edit made the latest actions.yml invalid.
func (s *ActionsService) List(context.Context) actions.EditableCatalog {
	return s.catalog.ListEditable()
}

func (s *ActionsService) Get(_ context.Context, id string) (actions.EditableAction, error) {
	a, ok := s.catalog.GetEditable(id)
	if !ok {
		return actions.EditableAction{}, Errorf(KindNotFound, "action %q not found", id)
	}
	return a, nil
}

func (s *ActionsService) Create(_ context.Context, a actions.EditableAction) (actions.EditableAction, error) {
	out, err := s.catalog.Create(a)
	if err != nil {
		return out, Wrap(err, KindInvalid, "creating action %q", a.ID)
	}
	s.wake()
	return out, nil
}

func (s *ActionsService) Update(ctx context.Context, id string, a actions.EditableAction) (actions.EditableAction, error) {
	out, err := s.catalog.Update(ctx, id, a)
	if err != nil {
		return out, Wrap(err, KindInvalid, "updating action %q", id)
	}
	s.wake()
	return out, nil
}

// Reorder persists the catalog order. A stale list — a hand edit added or
// removed an action meanwhile — is rejected so the caller reloads, which is
// the caller's view having moved rather than a failure.
func (s *ActionsService) Reorder(_ context.Context, ids []string) error {
	if err := s.catalog.Reorder(ids); err != nil {
		return Wrap(err, KindConflict, "reordering the actions catalog")
	}
	s.wake()
	return nil
}

func (s *ActionsService) Delete(ctx context.Context, id string) error {
	if err := s.catalog.Delete(ctx, id); err != nil {
		return Wrap(err, KindConflict, "deleting action %q", id)
	}
	s.wake()
	return nil
}

// ActivityService owns the user-facing audit log.
type ActivityService struct{ store *activity.Store }

func newActivityService(store *activity.Store) *ActivityService {
	return &ActivityService{store: store}
}

// List returns up to limit events with id < before, newest first. Pass
// before <= 0 to start from the most recent event.
func (s *ActivityService) List(ctx context.Context, before int64, limit int) ([]activity.Event, error) {
	events, err := s.store.List(ctx, before, limit)
	return events, Wrap(err, KindInternal, "listing activity events")
}

// Append records an event and returns the stored row. It is the path for
// surfacing something only a caller knows about — a failed save, a deleted
// profile — and publishes the same wake-up as any backend recording.
func (s *ActivityService) Append(ctx context.Context, e activity.Event) (activity.Event, error) {
	stored, err := s.store.Append(ctx, e)
	return stored, Wrap(err, KindInternal, "recording an activity event")
}

// JobService owns live action-run jobs.
type JobService struct{ store *jobs.Store }

func newJobService(store *jobs.Store) *JobService { return &JobService{store: store} }

// List returns up to limit jobs with id < before, newest first.
func (s *JobService) List(ctx context.Context, before int64, limit int) ([]jobs.Job, error) {
	out, err := s.store.List(ctx, before, limit)
	return out, Wrap(err, KindInternal, "listing jobs")
}

// ListActive returns non-terminal jobs plus terminal jobs completed within
// the lingering window, so a just-finished run stays visible briefly.
func (s *JobService) ListActive(ctx context.Context) ([]jobs.Job, error) {
	out, err := s.store.ListActive(ctx, jobs.DefaultLingerWindow)
	return out, Wrap(err, KindInternal, "listing active jobs")
}

// AuthService wraps the auth backend with context threading.
type AuthService struct{ backend auth.Backend }

func newAuthService(backend auth.Backend) *AuthService { return &AuthService{backend: backend} }

func (s *AuthService) Status(ctx context.Context) auth.Status { return s.backend.Status(ctx) }

func (s *AuthService) StartDeviceFlow(ctx context.Context) (auth.DeviceFlowInfo, error) {
	info, err := s.backend.StartDeviceFlow(ctx)
	return info, Wrap(err, KindUnauthenticated, "starting the device flow")
}

func (s *AuthService) CancelDeviceFlow(context.Context) { s.backend.CancelDeviceFlow() }

func (s *AuthService) SetToken(ctx context.Context, token string) (auth.Status, error) {
	status, err := s.backend.SetToken(ctx, token)
	return status, Wrap(err, KindUnauthenticated, "accepting the token")
}

func (s *AuthService) SignOut(context.Context) error {
	return Wrap(s.backend.SignOut(), KindInternal, "signing out")
}

// PromptsService owns the paste-ready LLM prompts. Prompt text lives in
// app/prompts; this resolves the install-specific environment they render
// against.
type PromptsService struct {
	webhooks *WebhookService
}

func newPromptsService(webhooks *WebhookService) *PromptsService {
	return &PromptsService{webhooks: webhooks}
}

// service is rebuilt per call rather than cached: the paths and webhook state
// it embeds are user-editable while the app runs, and a cached service would
// keep handing out a stale port after the listener rebinds.
func (s *PromptsService) service(ctx context.Context) (*prompts.Service, error) {
	env := prompts.Env{
		ConfigDir:    settings.ConfigDir(),
		FlowsDir:     settings.FlowsDir(),
		ActionsPath:  settings.ActionsPath(),
		SettingsPath: settings.SettingsPath(),
	}
	if _, port := s.webhooks.Endpoint(ctx); port > 0 {
		env.WebhookBaseURL = WebhookBaseURL(port)
	}
	// A settings read failure must not take the prompts page down with it:
	// every other prompt is still correct, so fall back to reporting the
	// listener as enabled and let the webhook settings pane surface the error.
	if cfg, err := settings.LoadSettings(); err == nil {
		env.WebhookEnabled = cfg.WebhookEnabledOrDefault()
	} else {
		env.WebhookEnabled = true
	}
	svc, err := prompts.New(env)
	return svc, Wrap(err, KindInternal, "building the prompt catalog")
}

// Catalog returns every prompt that belongs in a settings listing, rendered
// against this install.
func (s *PromptsService) Catalog(ctx context.Context, input prompts.Input) ([]prompts.Prompt, error) {
	svc, err := s.service(ctx)
	if err != nil {
		return nil, err
	}
	return svc.Catalog(input), nil
}

// Render returns one prompt by id, including the context-scoped prompts
// Catalog omits.
func (s *PromptsService) Render(ctx context.Context, id string, input prompts.Input) (prompts.Prompt, error) {
	svc, err := s.service(ctx)
	if err != nil {
		return prompts.Prompt{}, err
	}
	prompt, err := svc.Render(id, input)
	if err != nil {
		return prompts.Prompt{}, Wrap(err, KindNotFound, "rendering prompt %q", id)
	}
	return prompt, nil
}

// WebhookBaseURL is the URL prefix sources.webhook node paths are served
// under. It lives here rather than in the adapter because the prompt text
// embeds it, and a prompt is core-owned.
func WebhookBaseURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + webhook.PathPrefix
}
