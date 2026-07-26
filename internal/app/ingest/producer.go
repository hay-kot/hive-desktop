package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/rs/zerolog"
)

// Producer is the poll loop that turns configured source connectors into
// event_log rows. On each tick it resolves the current pull-mode instances,
// drains each one through Produce, and appends every emitted Msg to the log.
// After a tick appends at least one row, onAppended fires with the offset of
// the last row, so the core can wake the flow engine.
//
// Source deduplication: a connector re-emits every current item on every
// tick, even when nothing changed upstream (the GitHub fetch layer may itself
// be cache-hit, but the cached items are still emitted). Producer delegates
// to store.IngestObservation, which stores the last payload by (topic, key)
// in the database and atomically appends a changed event with its new head,
// so deduplication survives restarts and a failed append never suppresses a
// retry. Successful ticks also append a source snapshot event for downstream
// feed reconciliation.
//
// Nothing here branches on which connector it is holding. What a source
// supports beyond producing messages — its classifier, its absence confirmer,
// its batched prefetch — arrives already wired on the instance, because a
// capability discovered by type assertion fails silently as generic
// ingestion, and generic ingestion of a GitHub item is wrong rather than
// merely plain.
type Producer struct {
	db          Appender
	sources     Sources
	intervalMu  sync.Mutex
	interval    time.Duration
	intervalCh  chan time.Duration
	onAppended  func(nextOffset int64)
	logger      zerolog.Logger
	recorder    activity.Recorder
	pauseIngest time.Duration

	stopOnce sync.Once
	stop     chan struct{}
}

// SetRecorder attaches an activity recorder so refresh failures surface in the
// Activity view. Successful periodic refreshes are intentionally omitted to
// avoid flooding the activity log. Set once at wiring time, before Start.
func (pr *Producer) SetRecorder(r activity.Recorder) { pr.recorder = r }

// SetDebugPause injects the development-only post-hydration pause.
func (pr *Producer) SetDebugPause(duration time.Duration) { pr.pauseIngest = duration }

// NewProducer builds a Producer. interval <= 0 is rejected by the caller's
// choice of default (App passes feed.DefaultPollInterval); Producer itself
// has no opinion on the default so this package does not need to import feed
// just for a constant.
func NewProducer(db Appender, sources Sources, interval time.Duration, onAppended func(nextOffset int64), logger zerolog.Logger) *Producer {
	return &Producer{
		db:         db,
		sources:    sources,
		interval:   interval,
		intervalCh: make(chan time.Duration, 1),
		onAppended: onAppended,
		logger:     logger,
		stop:       make(chan struct{}),
	}
}

// Start runs the poll loop in a goroutine until Stop.
func (pr *Producer) Start(ctx context.Context) {
	pr.intervalMu.Lock()
	interval := pr.interval
	pr.intervalMu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pr.stop:
				return
			case interval := <-pr.intervalCh:
				ticker.Reset(interval)
			case <-ticker.C:
				pr.Tick(ctx)
			}
		}
	}()
}

// SetInterval changes the poll cadence at runtime. The running ticker resets
// to the new interval, with the next tick occurring one new interval from
// now. Values <= 0 are ignored. It is safe before Start and concurrent with
// the poll loop.
func (pr *Producer) SetInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}
	pr.intervalMu.Lock()
	pr.interval = interval
	select {
	case pr.intervalCh <- interval:
	default:
		// Keep only the latest pending update. The poll loop is the ticker's
		// sole owner, so it performs Reset itself.
		select {
		case <-pr.intervalCh:
		default:
		}
		select {
		case pr.intervalCh <- interval:
		default:
		}
	}
	pr.intervalMu.Unlock()
}

// Stop halts the poll loop. Idempotent.
func (pr *Producer) Stop() {
	pr.stopOnce.Do(func() { close(pr.stop) })
}

// Tick resolves the current sources and drains each one once, appending
// every emitted Msg to the log. It is exported so tests can drive a
// deterministic tick instead of waiting on the ticker. A source whose
// Produce call fails is logged and skipped — one source's fetch failure
// (e.g. an offline stretch) must not block the others.
func (pr *Producer) Tick(ctx context.Context) {
	instances := pr.sources.PullInstances()

	if err := pr.sources.Prefetch(ctx, instances); err != nil {
		pr.logger.Debug().Err(err).Msg("pipeline producer: source prefetch failed")
	}

	var (
		lastOffset int64
		appended   int
	)
	for _, instance := range instances {
		rows, err := pr.drain(ctx, instance)
		if err != nil {
			continue
		}
		if rows.appended > 0 {
			appended += rows.appended
			lastOffset = rows.lastOffset
		}
	}

	if appended > 0 && pr.onAppended != nil {
		pr.onAppended(lastOffset)
	}
}

// drained is what one source's tick was worth: how many rows it appended and
// the offset of the last one.
type drained struct {
	appended   int
	lastOffset int64
}

// drain runs one source's Produce, confirms whatever left its snapshot, and
// appends the topic's authoritative snapshot. The returned error means the
// source did not complete — its snapshot is not authoritative, so neither
// absence confirmation nor the snapshot append may run.
func (pr *Producer) drain(ctx context.Context, instance connector.Instance) (drained, error) {
	var out drained

	id := instance.Node.ID()
	topic := instance.Node.Topic()
	meta := instance.Metadata
	if meta.Policy == "" {
		meta.Policy = store.ResurfacePolicyStateChanges
	}
	// A connector that declared no classifier gets the generic one, which
	// records that something was observed or updated and nothing more.
	classifier := instance.Classifier
	if classifier == nil {
		classifier = genericClassifier{}
	}

	items := make([]store.SnapshotItem, 0)
	observed := make(map[string]struct{})
	err := instance.Pull.Produce(ctx, func(msg Msg) error {
		if msg.Topic != topic {
			return fmt.Errorf("source %q emitted topic %q, expected %q", id, msg.Topic, topic)
		}
		items = append(items, store.SnapshotItem{Key: msg.Key, Payload: msg.Payload})
		if msg.Key == "" {
			return nil
		}
		observed[msg.Key] = struct{}{}
		kind := meta.SourceKind
		if msg.SourceKind != "" {
			kind = msg.SourceKind
		}
		result, err := pr.db.IngestObservation(ctx, classifier, store.IngestObservationParams{ProfileID: meta.ProfileID, Topic: topic, Policy: meta.Policy, Current: observationFromMsg(msg, kind, meta.SourceScope)})
		if err != nil {
			return err
		}
		if result.Wrote {
			out.appended++
			out.lastOffset = result.Offset
		}
		return nil
	})
	if err != nil {
		pr.logger.Debug().Err(err).Str("source", id).Msg("pipeline producer: source fetch failed")
		pr.record(ctx, activity.RefreshFailed(id, err.Error()))
		return out, err
	}

	if instance.Absence != nil {
		pr.confirmAbsent(ctx, instance, meta, classifier, observed, &out)
	}

	offset, err := pr.db.AppendSnapshot(ctx, topic, meta.SourceKind, meta.SourceScope, items)
	if err != nil {
		pr.logger.Debug().Err(err).Str("source", id).Msg("pipeline producer: appending source snapshot failed")
		pr.record(ctx, activity.RefreshFailed(id, err.Error()))
		return out, err
	}
	out.appended++
	out.lastOffset = offset
	return out, nil
}

// confirmAbsent asks the connector what happened to each item that was in the
// source head but not in this tick's snapshot. Only connectors that declared
// CapConfirmAbsence get here; for the rest an item that stops appearing is
// left to the resurface policy.
func (pr *Producer) confirmAbsent(ctx context.Context, instance connector.Instance, meta connector.Metadata, classifier store.Classifier, observed map[string]struct{}, out *drained) {
	id := instance.Node.ID()
	topic := instance.Node.Topic()

	keys, err := pr.db.ListSourceHeadKeys(ctx, topic)
	if err != nil {
		pr.logger.Debug().Err(err).Str("source", id).Msg("pipeline producer: listing source head failed")
		return
	}
	for _, key := range keys {
		if _, present := observed[key]; present {
			continue
		}
		payload, err := pr.db.SourceHeadPayload(ctx, topic, key)
		if err != nil {
			pr.logger.Debug().Err(err).Str("source", id).Str("key", key).Msg("pipeline producer: reading source head failed")
			continue
		}
		// source_head persists the source payload, not presentation metadata.
		// Reconstruct the prior observation from that payload so an absence
		// confirmer that starts from prev retains the item's title, URL, and
		// upstream observation time when it returns a hydrated Current.
		prev := observationFromMsg(Msg{Key: key, Payload: payload}, meta.SourceKind, meta.SourceScope)
		verdict, err := instance.Absence.ConfirmAbsence(ctx, prev)
		debugPause(ctx, pr.pauseIngest)
		if err != nil {
			pr.logger.Debug().Err(err).Str("source", id).Str("key", key).Msg("pipeline producer: absence hydration failed")
			continue
		}
		if verdict.Current == nil {
			continue
		}
		result, err := pr.db.IngestObservation(ctx, classifier, store.IngestObservationParams{ProfileID: meta.ProfileID, Topic: topic, Policy: meta.Policy, Current: *verdict.Current})
		if err != nil {
			pr.logger.Debug().Err(err).Str("source", id).Str("key", key).Msg("pipeline producer: absence ingestion failed")
			continue
		}
		if result.Wrote {
			out.appended++
			out.lastOffset = result.Offset
		}
	}
}

func debugPause(ctx context.Context, duration time.Duration) {
	if duration <= 0 {
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// record forwards an activity event when a recorder is attached. Recording is
// best-effort: the recorder itself logs and swallows failures, and a nil
// recorder (no wiring) is a no-op.
func (pr *Producer) record(ctx context.Context, e activity.Event) {
	if pr.recorder != nil {
		pr.recorder.Record(ctx, e)
	}
}

// genericClassifier keeps non-GitHub/test sources ingestible while adapters
// supply richer semantics for real source kinds.
func observationFromMsg(msg Msg, sourceKind, sourceScope string) store.Observation {
	var wire struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		UpdatedAt int64  `json:"updatedAt"`
	}
	_ = json.Unmarshal(msg.Payload, &wire)
	if wire.Title == "" {
		wire.Title = msg.Key
	}
	if wire.UpdatedAt == 0 {
		wire.UpdatedAt = time.Now().UnixMilli()
	}
	return store.Observation{ExternalID: msg.Key, Title: wire.Title, URL: wire.URL, SourceKind: sourceKind, SourceScope: sourceScope, ObservedAt: wire.UpdatedAt, Payload: msg.Payload}
}

type genericClassifier struct{}

func (genericClassifier) Classify(previous *store.Observation, current store.Observation) store.Classification {
	if previous == nil {
		return store.Classification{Kind: "observed", Transition: store.TransitionNone, Attention: store.AttentionActivity, Lifecycle: store.LifecycleUnknown, Summary: current.Title}
	}
	return store.Classification{Kind: "updated", Transition: store.TransitionNone, Attention: store.AttentionTrivial, Lifecycle: store.LifecycleUnknown, Summary: current.Title}
}
