package events

// The complete core event vocabulary. A payload carries what happened, not a
// hint that something did: an adapter that only needs a wake-up can throw the
// payload away, but one that needs the delta cannot invent it.
//
// window:focus and window:blur are deliberately absent. Window focus is
// GUI-only state and stays entirely inside the wailsui adapter.

// LogAppended reports that a producer tick or webhook delivery appended at
// least one row to the event log.
type LogAppended struct{ NextOffset int64 }

// InboxUpdated reports that the flow engine committed at least one run — feed
// membership, queued actions, or node-run metrics may all have changed.
//
// This is a distinct event from LogAppended, not a duplicate of it. The log
// growing says a source observed something; it says nothing about whether any
// flow routed it anywhere. A reader that refreshes on LogAppended reads before
// the engine has committed, and one that never sees this event misses a
// membership change a replay made with no new log rows at all.
type InboxUpdated struct{}

// ActivityAppended reports a new row in the user-facing activity log.
type ActivityAppended struct{ ID int64 }

// JobsUpdated reports a job lifecycle transition. JobID is the concrete
// demonstration of the rule: the wiring this replaces threw the id away at the
// emit boundary, so no consumer could act on which job changed.
type JobsUpdated struct {
	JobID  int64
	Status string
}

// FlowsUpdated reports that the flow set was reloaded. Reason names what
// caused it — an external edit, or the app's own save.
type FlowsUpdated struct{ Reason string }

// ActionsUpdated reports that the actions catalog was reloaded or mutated.
type ActionsUpdated struct{ Count int }

// ConnectionUpdated reports that one provider's stored credentials changed —
// connected, rotated, or disconnected. Provider names which ("github"), so a
// consumer can ignore a provider it does not use; the new state is not in the
// payload because the connector's own status is the authority on it.
type ConnectionUpdated struct{ Provider string }

// NotificationRaised reports that a flow's notify terminal fired. InApp
// carries the user's delivery choice; routing it to a banner or a toast is
// the adapter's decision.
type NotificationRaised struct {
	ProfileID string
	ItemID    int64
	Title     string
	Body      string
	Severity  string
	InApp     bool
}

// UpdateChecked reports the outcome of a self-update check.
type UpdateChecked struct {
	Available bool
	Version   string
	Notes     string
}

func (LogAppended) eventName() string        { return "log.appended" }
func (InboxUpdated) eventName() string       { return "inbox.updated" }
func (ActivityAppended) eventName() string   { return "activity.appended" }
func (JobsUpdated) eventName() string        { return "jobs.updated" }
func (FlowsUpdated) eventName() string       { return "flows.updated" }
func (ActionsUpdated) eventName() string     { return "actions.updated" }
func (ConnectionUpdated) eventName() string  { return "connection.updated" }
func (NotificationRaised) eventName() string { return "notification.raised" }
func (UpdateChecked) eventName() string      { return "update.checked" }
