package main

import (
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/desktop"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline"
	"github.com/hay-kot/hive-desktop/internal/desktop/prompts"
)

// PromptsService exposes the paste-ready LLM prompts to the frontend: the
// "LLM prompts" settings section lists Catalog(), and context-scoped surfaces
// (a webhook node's transform prompt) call Render() with instance data.
//
// Prompt text lives in internal/desktop/prompts, not here and not in any Vue
// component — this is transport plus the one thing the frontend cannot know
// on its own, the real config paths on this machine.
type PromptsService struct {
	webhookPort int
	// webhookListener is nil when the listener is disabled or was never
	// started, in which case the configured port still describes the endpoint
	// a live run would serve.
	webhookListener *pipeline.WebhookListener
}

// NewPromptsService wires the Wails prompts binding.
func NewPromptsService(listener *pipeline.WebhookListener, webhookPort int) *PromptsService {
	return &PromptsService{webhookPort: webhookPort, webhookListener: listener}
}

// service builds a prompts.Service against the current environment. It is
// rebuilt per call rather than cached because the paths and the webhook state
// it embeds are user-editable while the app runs — a cached service would keep
// handing out a stale port after the listener rebinds.
func (s *PromptsService) service() (*prompts.Service, error) {
	env := prompts.Env{
		ConfigDir:    desktop.ConfigDir(),
		FlowsDir:     desktop.FlowsDir(),
		ActionsPath:  desktop.ActionsPath(),
		SettingsPath: desktop.SettingsPath(),
	}

	port := s.webhookPort
	if s.webhookListener != nil && s.webhookListener.Running() {
		port = s.webhookListener.Port()
	}
	if port > 0 {
		env.WebhookBaseURL = webhookBaseURL(port)
	}
	// A settings read failure must not take the prompts page down with it:
	// every other prompt is still correct, so fall back to reporting the
	// listener as enabled and let the webhook settings pane surface the error.
	if settings, err := desktop.LoadSettings(); err == nil {
		env.WebhookEnabled = settings.WebhookEnabledOrDefault()
	} else {
		env.WebhookEnabled = true
	}

	return prompts.New(env)
}

// Catalog returns every prompt that belongs in the settings listing, rendered
// against this install. input carries the frontend-owned bindable command
// catalog the keybindings prompt needs.
func (s *PromptsService) Catalog(input prompts.Input) ([]prompts.Prompt, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	return svc.Catalog(input), nil
}

// Render returns one prompt by id, including the context-scoped prompts
// Catalog omits.
func (s *PromptsService) Render(id string, input prompts.Input) (prompts.Prompt, error) {
	svc, err := s.service()
	if err != nil {
		return prompts.Prompt{}, err
	}
	prompt, err := svc.Render(id, input)
	if err != nil {
		return prompts.Prompt{}, fmt.Errorf("render prompt: %w", err)
	}
	return prompt, nil
}
