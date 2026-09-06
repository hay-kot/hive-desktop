package stores

import (
	"context"
	"database/sql"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// Stores constructs every store over one database handle and offers the one
// transaction entry point a service is allowed to use. It does nothing else:
// no seeding, no migrations, no cross-store work. A write spanning two
// aggregates is a service opening Tx, not a method here.
type Stores struct {
	q *queries.DB

	ActivityEvents  *ActivityEventStore
	AgentSessions   *AgentSessionStore
	EventLog        *EventLogStore
	FeedClaims      *FeedClaimStore
	InboxItems      *InboxItemStore
	ItemSessions    *ItemSessionStore
	Jobs            *JobStore
	NodeKV          *NodeKVStore
	NodeRuns        *NodeRunStore
	OutputCommands  *OutputCommandStore
	SourceHeads     *SourceHeadStore
	WebhookCaptures *WebhookCaptureStore
}

// Options configures every store built by New. Both fields are optional.
type Options struct {
	// Now supplies write timestamps; defaults to time.Now. It replaces
	// activity.Options.Now and jobs.Options.Now, and the bare time.Now()
	// calls the hand-written store files made before this package existed.
	Now func() time.Time
	// Logger is where a store reports a recoverable anomaly it chose to skip
	// rather than fail on -- today only EventLogStore.Commit's keyless feed
	// output (3b).
	Logger zerolog.Logger
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// New builds every store over q. It is cheap enough to call as part of
// construction; nothing here touches the database.
func New(q *queries.DB, opts Options) *Stores {
	opts = opts.withDefaults()

	return &Stores{
		q: q,

		ActivityEvents:  NewActivityEventStore(q, opts),
		AgentSessions:   NewAgentSessionStore(q, opts),
		EventLog:        NewEventLogStore(q, opts),
		FeedClaims:      NewFeedClaimStore(q, opts),
		InboxItems:      NewInboxItemStore(q, opts),
		ItemSessions:    NewItemSessionStore(q, opts),
		Jobs:            NewJobStore(q, opts),
		NodeKV:          NewNodeKVStore(q, opts),
		NodeRuns:        NewNodeRunStore(q, opts),
		OutputCommands:  NewOutputCommandStore(q, opts),
		SourceHeads:     NewSourceHeadStore(q, opts),
		WebhookCaptures: NewWebhookCaptureStore(q, opts),
	}
}

// Tx opens a transaction and returns a context carrying it, so store calls
// made with that context join it. The caller owns Commit and Rollback. It
// exists so a service that needs a cross-aggregate transaction never imports
// the queries package.
func (s *Stores) Tx(ctx context.Context) (context.Context, *sql.Tx, error) {
	return queries.WithTransaction(ctx, s.q)
}
