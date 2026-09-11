// Package configstate defines configuration reconciliation contracts shared by domains.
package configstate

import "time"

// Source identifies a configuration authority.
type Source string

const (
	Settings        Source = "settings"
	Actions         Source = "actions"
	Flows           Source = "flows"
	AgentWorkspaces Source = "agent_workspaces"
)

// Trigger identifies why reconciliation evaluated configuration.
type Trigger string

const (
	Startup    Trigger = "startup"
	Filesystem Trigger = "filesystem"
	Scan       Trigger = "scan"
	Overflow   Trigger = "overflow"
	AppWrite   Trigger = "app_write"
)

// Revision is a deterministic digest of configuration state.
type Revision string

// Outcome describes the result of one domain evaluation.
type Outcome string

const (
	Applied  Outcome = "applied"
	Partial  Outcome = "partial"
	Rejected Outcome = "rejected"
	Noop     Outcome = "noop"
	Error    Outcome = "error"
)

// CandidateState describes a candidate configuration value.
type CandidateState string

const (
	Valid      CandidateState = "valid"
	Invalid    CandidateState = "invalid"
	Missing    CandidateState = "missing"
	Unreadable CandidateState = "unreadable"
)

// ApplyState describes whether a candidate is in service.
type ApplyState string

const (
	Pending       ApplyState = "pending"
	Active        ApplyState = "active"
	LastGood      ApplyState = "last_good"
	Inactive      ApplyState = "inactive"
	NotApplicable ApplyState = "not_applicable"
)

// ActiveScope identifies what an active revision acknowledges.
type ActiveScope string

const (
	Catalog      ActiveScope = "catalog"
	FlowRuntime  ActiveScope = "flow_runtime"
	CoreSettings ActiveScope = "core_settings"
	None         ActiveScope = "none"
)

// Health describes whether a domain has usable configuration.
type Health string

const (
	Healthy  Health = "healthy"
	Degraded Health = "degraded"
	Failed   Health = "failed"
)

// Stage identifies the reconciliation step that produced a diagnostic.
type Stage string

const (
	Read     Stage = "read"
	Migrate  Stage = "migrate"
	Decode   Stage = "decode"
	Validate Stage = "validate"
	Apply    Stage = "apply"
)

// Reason categorizes a reconciliation diagnostic without exposing input text.
type Reason string

const (
	SourceUnavailable    Reason = "source_unavailable"
	UnsupportedVersion   Reason = "unsupported_version"
	InvalidDocument      Reason = "invalid_document"
	InvalidConfiguration Reason = "invalid_configuration"
	MissingDependency    Reason = "missing_dependency"
	RuntimeFailure       Reason = "runtime_failure"
)

// Diagnostic is safe to publish in reconciliation status and telemetry.
type Diagnostic struct {
	Stage  Stage
	Reason Reason
	Line   int
	Column int
}

// EntryStatus reports the state of one domain entry.
type EntryStatus struct {
	Kind              string
	ID                string
	CandidateRevision Revision
	LoadedRevision    Revision
	ActiveRevision    Revision
	CandidateState    CandidateState
	ApplyState        ApplyState
	Diagnostic        *Diagnostic
}

// DomainStatus reports one domain's accepted and active configuration state.
type DomainStatus struct {
	Source                Source
	CandidateRevision     Revision
	LoadedRevision        Revision
	ActiveRevision        Revision
	ActiveScope           ActiveScope
	LastEvaluatedAt       time.Time
	LastSuccessAt         time.Time
	Outcome               Outcome
	Health                Health
	PendingApply          bool
	Diagnostic            *Diagnostic
	RestartRequiredFields []string
	Entries               []EntryStatus
}

// Request supplies detection and dependency state for a domain evaluation.
type Request struct {
	Trigger             Trigger
	ObservedRevision    Revision
	DependencyRevisions map[Source]Revision
}

// Result describes the effect of a domain evaluation.
type Result struct {
	Status             DomainStatus
	Changed            bool
	EffectiveChanged   bool
	DuplicateAttempt   bool
	ChangedEntries     int
	InvalidEntries     int
	RemovedEntries     int
	ApplyFailedEntries int
}

// RevisionPart contributes one logical value to an aggregate revision.
type RevisionPart struct {
	Key      string
	State    CandidateState
	Revision Revision
}
