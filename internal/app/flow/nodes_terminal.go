package flow

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hay-kot/hive-desktop/internal/app/icons"
)

// feedDescriptionMaxLen caps a feed's hover description. It is generous enough
// for a sentence or two of context (useful for LLM-generated feeds) while
// keeping the persisted YAML and the sidebar tooltip bounded.
const feedDescriptionMaxLen = 500

// FeedConfig is a feed node: 1 input, 0 outputs (terminal). The node *is* the
// feed — its identity is the node id (flow-qualified as "<flowId>/<nodeId>"
// for membership claims). Icon and Description are purely cosmetic
// sidebar presentation: the icon shown in the tree and the tooltip surfaced
// on hover (handy context for LLM-generated feeds). Both are optional.
type FeedConfig struct {
	Icon        string `json:"icon,omitempty"        yaml:"icon,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Notify turns this feed into one that interrupts: a present block means
	// "tell me when something new lands here", and its absence means the feed
	// is read at the user's leisure like any other. Presence is the switch so
	// a quiet feed carries no notify keys at all — the same absent-means-
	// default idiom Icon and Description use.
	//
	// A notify *node* (see NotifyConfig) is the general form: it notifies for
	// whatever is routed to it and claims no feed membership. This is the
	// common case of the same idea — the feed you point at "things that need
	// my attention right now" — expressed on the feed itself rather than as a
	// second terminal alongside it. Both deliver through the same executor.
	Notify *NotifyConfig `json:"notify,omitempty" yaml:"notify,omitempty"`
}

func (c *FeedConfig) Inputs() int  { return 1 }
func (c *FeedConfig) Outputs() int { return 0 }

// NotifyDeclaration satisfies dispatch's notify-raiser capability: a feed
// only raises a notify output when it carries a Notify block, and — unlike a
// notify node — restricts delivery to genuinely new arrivals. A feed is a
// place items live, so "notify me about this feed" means the arrivals, not
// every later comment on something already sitting in it.
func (c *FeedConfig) NotifyDeclaration() (cfg *NotifyConfig, onlyWhenNew, ok bool) {
	if c.Notify == nil {
		return nil, false, false
	}
	return c.Notify, true, true
}

func (c *FeedConfig) Validate(Refs) error {
	if !icons.ValidFeed(c.Icon) {
		return fmt.Errorf("icon: %q is not a supported feed icon", c.Icon)
	}
	if utf8.RuneCountInString(c.Description) > feedDescriptionMaxLen {
		return fmt.Errorf("description: must be at most %d characters", feedDescriptionMaxLen)
	}
	if c.Notify != nil {
		if err := c.Notify.Validate(nil); err != nil {
			return fmt.Errorf("notify: %w", err)
		}
	}
	return nil
}

// ActionConfig is an action node: 1 input, 0 outputs (terminal). It enqueues
// an output_command against the referenced desktop actions.yml action id.
type ActionConfig struct {
	Action string `json:"action" yaml:"action"`
}

func (c *ActionConfig) Inputs() int  { return 1 }
func (c *ActionConfig) Outputs() int { return 0 }

func (c *ActionConfig) Validate(refs Refs) error {
	if c.Action == "" {
		return fmt.Errorf("action: action is required")
	}
	if !validSlug(c.Action) {
		return fmt.Errorf("action: action %q is not a valid id", c.Action)
	}
	if !refsResolveAction(refs, c.Action) {
		return fmt.Errorf("action: action %q: unresolved reference", c.Action)
	}
	if !refsActionHeadlessCapable(refs, c.Action) {
		return fmt.Errorf("action: action %q requires interactive session input and cannot run in a flow", c.Action)
	}
	return nil
}

// notifySeverities is the closed set a notify node may declare. It is the
// same vocabulary the app-level notification path already speaks
// (internal/adapter/wailsui's Input.Severity), so a flow node and a built-in
// notification map to the same native interruption level rather than
// inventing a second urgency scale.
var notifySeverities = map[string]bool{
	"info":    true,
	"success": true,
	"warning": true,
	"error":   true,
}

// NotifySeverityDefault is the severity a notify node uses when it declares
// none: an ordinary, non-interrupting banner.
const NotifySeverityDefault = "info"

// notifyTitleMaxLen and notifyBodyMaxLen cap the *templates*, not the
// rendered result — enough room for a sentence of context each while keeping
// the persisted YAML bounded. The executor separately bounds what a template
// actually renders to.
const (
	notifyTitleMaxLen = 200
	notifyBodyMaxLen  = 1000
)

// NotifyConfig is a notify node: 1 input, 0 outputs (terminal). Every
// arriving message enqueues an output_command that the notify executor
// delivers as a native OS notification, with Title/Body rendered as Go
// text/templates over the message (the same templating story as an action's
// prompt_template — see internal/app/actions/launch_session.go).
//
// The node cannot override the app's notification settings: the executor
// checks the global kill switch before every delivery, so "notifications
// off" always wins over any flow.
type NotifyConfig struct {
	// Title renders the notification's headline. Required — a native
	// notification without a title is rejected by the OS layer.
	Title string `json:"title" yaml:"title"`
	// Body renders the notification's message. Optional.
	Body string `json:"body,omitempty" yaml:"body,omitempty"`
	// Severity selects the native interruption level (see notifySeverities).
	// Empty means NotifySeverityDefault.
	Severity string `json:"severity,omitempty" yaml:"severity,omitempty"`
	// Sound is a pointer so an absent key ("use the default", which is sound
	// on) is distinguishable from an explicit `sound: false`. It can only
	// silence a notification — the global notification-sound setting still
	// wins when it is off. Resolve through SoundOrDefault.
	Sound *bool `json:"sound,omitempty" yaml:"sound,omitempty"`
}

func (c *NotifyConfig) Inputs() int  { return 1 }
func (c *NotifyConfig) Outputs() int { return 0 }

// NotifyDeclaration satisfies dispatch's notify-raiser capability: a notify
// node always has content to raise and never restricts delivery to new
// arrivals — it notifies for whatever is routed to it, unconditionally,
// which is the author's explicit choice in authoring the node at all.
func (c *NotifyConfig) NotifyDeclaration() (cfg *NotifyConfig, onlyWhenNew, ok bool) {
	return c, false, true
}

// SeverityOrDefault resolves Severity, defaulting to NotifySeverityDefault
// when the node declares none.
func (c *NotifyConfig) SeverityOrDefault() string {
	if c.Severity == "" {
		return NotifySeverityDefault
	}
	return c.Severity
}

// SoundOrDefault resolves Sound, defaulting to true (play the notification
// sound) when the key is absent.
func (c *NotifyConfig) SoundOrDefault() bool {
	if c.Sound == nil {
		return true
	}
	return *c.Sound
}

func (c *NotifyConfig) Validate(Refs) error {
	if strings.TrimSpace(c.Title) == "" {
		return fmt.Errorf("title: title is required")
	}
	if utf8.RuneCountInString(c.Title) > notifyTitleMaxLen {
		return fmt.Errorf("title: must be at most %d characters", notifyTitleMaxLen)
	}
	if utf8.RuneCountInString(c.Body) > notifyBodyMaxLen {
		return fmt.Errorf("body: must be at most %d characters", notifyBodyMaxLen)
	}
	if c.Severity != "" && !notifySeverities[c.Severity] {
		return fmt.Errorf("severity: %q is not a supported severity (info, success, warning, error)", c.Severity)
	}
	return nil
}
