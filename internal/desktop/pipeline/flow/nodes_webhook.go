package flow

import (
	"fmt"
	"strings"
)

// Webhook path/secret bounds. Paths are URL segments under the local
// listener's /hooks/ prefix, so they stay slug-shaped; the secret is an
// opaque header value with no shape beyond printable-ASCII sanity.
const (
	maxWebhookPathLen   = 128
	maxWebhookSecretLen = 128
)

// WebhookSourceConfig is a webhook-source node: 0 inputs, 1 output. The node
// declares a path on the desktop's local webhook listener; JSON POSTed to
// /hooks/<path> is ingested as this node's observations and appended to the
// event log under the node's flow-qualified topic — the same delivery
// contract as github-source, minus the poll loop (delivery is push-driven).
// Several nodes (across flows) may declare the same path: each enabled one
// receives every request, so one sender can fan into multiple flows.
type WebhookSourceConfig struct {
	// Path is the endpoint under /hooks/, e.g. "ci-alerts" or "ci/deploys":
	// one or more slug segments separated by "/", no leading or trailing
	// slash.
	Path string `json:"path" yaml:"path"`
	// Secret, when set, requires senders to present the same value in the
	// X-Hive-Secret request header. Requests without it are rejected 401.
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
}

func (c *WebhookSourceConfig) Inputs() int  { return 0 }
func (c *WebhookSourceConfig) Outputs() int { return 1 }

// Validate is Refs-free: a webhook-source node's config is self-contained.
func (c *WebhookSourceConfig) Validate(Refs) error {
	if strings.TrimSpace(c.Path) == "" {
		return fmt.Errorf("webhook-source: path is required")
	}
	if len(c.Path) > maxWebhookPathLen {
		return fmt.Errorf("webhook-source: path exceeds %d characters", maxWebhookPathLen)
	}
	for _, segment := range strings.Split(c.Path, "/") {
		if !validWebhookPathSegment(segment) {
			return fmt.Errorf("webhook-source: invalid path %q (want lowercase slug segments like \"ci-alerts\" or \"ci/deploys\")", c.Path)
		}
	}
	if len(c.Secret) > maxWebhookSecretLen {
		return fmt.Errorf("webhook-source: secret exceeds %d characters", maxWebhookSecretLen)
	}
	for _, r := range c.Secret {
		if r < '!' || r > '~' {
			return fmt.Errorf("webhook-source: secret must be printable ASCII without spaces")
		}
	}
	return nil
}

// validWebhookPathSegment reports whether one "/"-separated path segment is a
// slug: a lowercase letter or digit followed by lowercase letters, digits,
// "-" or "_".
func validWebhookPathSegment(segment string) bool {
	if segment == "" {
		return false
	}
	for i, r := range segment {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '_'):
		default:
			return false
		}
	}
	return true
}
