package actions

import (
	"fmt"
	"strings"
)

// LaunchSessionConfig is a launch-session action: it spawns a hive session
// from a triggering msg. PromptTemplate/RepoTemplate are Go text/template
// strings rendered over the msg payload by the output worker (see
// internal/app/ingest's LaunchSessionExecutor) — this package only
// parses and validates the config, it never renders or executes it.
type LaunchSessionConfig struct {
	// PromptTemplate renders the new session's initial prompt.
	PromptTemplate string `yaml:"prompt_template"`
	// Agent optionally selects a non-default agent profile for the new
	// session (e.g. "claude", "aider").
	Agent string `yaml:"agent,omitempty"`
	// RepoTemplate optionally renders which repo the session is created
	// against; empty means the launcher's own default.
	RepoTemplate string `yaml:"repo_template,omitempty"`
	// PostHook optionally renders a shell command to run in the new session's
	// checkout once it exists — checking out a pull request and opening an
	// editor on it, say. It is rendered over the same data as the templates
	// above, with `.Session` bound to the session that was just created.
	PostHook string `yaml:"post_hook,omitempty"`
	// PostHookTimeout bounds how long the hook may run; zero means the
	// executor's own default.
	PostHookTimeout Duration `yaml:"post_hook_timeout,omitempty"`
}

func (c *LaunchSessionConfig) Validate() error {
	if strings.TrimSpace(c.PromptTemplate) == "" {
		return fmt.Errorf("launch-session: prompt_template is required")
	}
	if c.PostHookTimeout < 0 {
		return fmt.Errorf("launch-session: post_hook_timeout must be positive")
	}
	if strings.TrimSpace(c.PostHook) == "" && c.PostHookTimeout != 0 {
		return fmt.Errorf("launch-session: post_hook_timeout is set without post_hook")
	}
	return nil
}
