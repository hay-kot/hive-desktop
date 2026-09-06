package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"
	"github.com/hay-kot/hive-desktop/internal/app/observe"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
)

// Producer is the poll loop that turns configured source connectors into
// event_log rows. On each tick it resolves the current pull-mode instances,
// drains each one through Produce, and appends every emitted Msg to the log.
// After a tick appends at least one row, onAppended fires with the offset of
// the last row, so the core can wake the flow engine.
//
// There is one ticker for every source, at settings.polling.interval. An
// instance that wants to run less often than that declares a MinInterval and
// the tick skips it until it is due — a floor quantized to the tick, not a
// second schedule. Not drained is not the same as drained empty: Produce is
// never called, so nothing about the source's tracked set changes.
//
// Source deduplication: a connector re-emits every current item on every
// tick, even when nothing changed upstream (the GitHub fetch layer may itself
// be cache-hit, but the cached items are still emitted). Producer delegates
// to InboxItemStore.IngestObservation, which stores the last payload by (topic, key)
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
	ingester    Ingester
	snapshots   SnapshotAppender
	heads       SourceHeads
	sources     Sources
	intervalMu  sync.Mutex
	interval    time.Duration
	intervalCh  chan time.Duration
	onAppended  func(nextOffset int64)
	logger      zerolog.Logger
	recorder    activity.Recorder
	pauseIngest time.Duration
	now         func() time.Time

	// scheduleMu guards the per-source state a tick keeps between ticks. A
	// manual refresh runs a tick on the caller's goroutine while the loop may
	// be running one of its own.
	scheduleMu  sync.Mutex
	lastRun     map[string]time.Time
	lastFailure map[string]time.Time

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
func NewProducer(ingester Ingester, snapshots SnapshotAppender, heads SourceHeads, sources Sources, interval time.Duration, onAppended func(nextOffset int64), logger zerolog.Logger) *Producer {
	return &Producer{
		ingester:    ingester,
		snapshots:   snapshots,
		heads:       heads,
		sources:     sources,
		interval:    interval,
		intervalCh:  make(chan time.Duration, 1),
		onAppended:  onAppended,
		logger:      logger,
		now:         time.Now,
		lastRun:     map[string]time.Time{},
		lastFailure: map[string]time.Time{},
		stop:        make(chan struct{}),
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

// TickSummary reports what one tick did, so a caller that forced the tick (a
// manual refresh) can tell "nothing changed" from "every source failed".
type TickSummary struct {
	Sources  int
	Appended int
	Failed   int
}

// Tick resolves the current sources and drains each one whose own cadence is
// due, appending every emitted Msg to the log. It is exported so tests can
// drive a deterministic tick instead of waiting on the ticker. A source whose
// Produce call fails is logged and skipped — one source's fetch failure
// (e.g. an offline stretch) must not block the others.
func (pr *Producer) Tick(ctx context.Context) TickSummary { return pr.tick(ctx, false) }

// Refresh drains every source now, ignoring the per-instance cadence floors: a
// user who asked for a refresh gets one, including from the source that only
// wanted to run hourly.
func (pr *Producer) Refresh(ctx context.Context) TickSummary { return pr.tick(ctx, true) }

func (pr *Producer) tick(ctx context.Context, forced bool) TickSummary {
	// A trigger, so a root span: everything below hangs off it, which is what
	// makes an otherwise orphan client span readable.
	ctx, span := tracer.Start(ctx, "ingest.tick", trace.WithAttributes(attribute.Bool(attrForced, forced)))
	defer span.End()

	instances := pr.sources.PullInstances()

	pr.prefetch(ctx, instances)

	pr.pruneSchedule(instances)

	summary := TickSummary{Sources: len(instances)}
	var lastOffset, drained int64
	for _, instance := range instances {
		if !forced && !pr.claimRun(instance) {
			continue
		}
		drained++
		rows, err := pr.drain(ctx, instance)
		if err != nil {
			summary.Failed++
			continue
		}
		if rows.appended > 0 {
			summary.Appended += rows.appended
			lastOffset = rows.lastOffset
		}
	}

	span.SetAttributes(
		attribute.Int(attrSources, summary.Sources),
		attribute.Int64(attrDrained, drained),
		attribute.Int(attrFailed, summary.Failed),
		attribute.Int(attrAppended, summary.Appended),
	)

	if summary.Appended > 0 && pr.onAppended != nil {
		pr.onAppended(lastOffset)
	}
	return summary
}

// A kind is overridable per message and so can be absent; a trailing space in
// a search key helps nobody.
func sourceSpanName(kind string) string {
	if kind == "" {
		return "ingest.source"
	}
	return "ingest.source " + kind
}

// One batched round trip per tick. It has a span because without one its time
// lands under ingest.tick as a bare HTTP call, and it can be most of the tick.
func (pr *Producer) prefetch(ctx context.Context, instances []connector.Instance) {
	ctx, span := tracer.Start(ctx, "ingest.prefetch", trace.WithAttributes(
		attribute.Int(attrSources, len(instances)),
	))
	defer span.End()

	if err := pr.sources.Prefetch(ctx, instances); err != nil {
		observe.RecordError(span, err)
		pr.logger.Debug().Ctx(ctx).Err(err).Msg("pipeline producer: source prefetch failed")
	}
}

// pruneSchedule drops the per-source state of sources that are no longer
// configured. Both maps are keyed by a flow-qualified node id, which the user
// renames freely, so without this an edited flow leaks an entry per rename for
// the life of the process. It also means a node that is deleted and recreated
// starts clean, which is what re-adding a source should do.
func (pr *Producer) pruneSchedule(instances []connector.Instance) {
	current := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		current[instance.Node.ID()] = struct{}{}
	}

	stale := func(id string, _ time.Time) bool {
		_, present := current[id]
		return !present
	}

	pr.scheduleMu.Lock()
	defer pr.scheduleMu.Unlock()
	maps.DeleteFunc(pr.lastRun, stale)
	maps.DeleteFunc(pr.lastFailure, stale)
}

// claimRun reports whether instance is due, recording the attempt when it is.
//
// The floor rate-limits *running* the source, not succeeding at it: a failed
// run still claims its slot, so a command asking to run hourly is not retried
// every tick because it is broken. Nothing here persists — a restart runs every
// source once, which is the behaviour a user expects from launching the app.
func (pr *Producer) claimRun(instance connector.Instance) bool {
	if instance.MinInterval <= 0 {
		return true
	}
	id := instance.Node.ID()
	now := pr.now()

	pr.scheduleMu.Lock()
	defer pr.scheduleMu.Unlock()
	if last, ok := pr.lastRun[id]; ok && now.Sub(last) < instance.MinInterval {
		return false
	}
	pr.lastRun[id] = now
	return true
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
func (pr *Producer) drain(ctx context.Context, instance connector.Instance) (out drained, err error) {
	id := instance.Node.ID()
	topic := instance.Node.Topic()
	meta := instance.Metadata
	if meta.Policy == "" {
		meta.Policy = models.ResurfacePolicyStateChanges
	}

	// Named by kind, which is bounded; the id rides as an attribute.
	ctx, span := tracer.Start(ctx, sourceSpanName(meta.SourceKind), trace.WithAttributes(
		attribute.String(attrSourceID, id),
		attribute.String(attrSourceKnd, meta.SourceKind),
		attribute.String(attrTopic, topic),
	))
	// A failed source is the question this span answers; the tick only counts it.
	defer func() {
		if err != nil {
			observe.RecordError(span, err)
		}
		span.SetAttributes(attribute.Int(attrAppended, out.appended))
		span.End()
	}()
	// A connector that declared no classifier gets the generic one, which
	// records that something was observed or updated and nothing more.
	classifier := instance.Classifier
	if classifier == nil {
		classifier = genericClassifier{}
	}

	items := make([]models.SnapshotItem, 0)
	observed := make(map[string]struct{})
	err = instance.Pull.Produce(ctx, func(msg Msg) error {
		if msg.Topic != topic {
			return fmt.Errorf("source %q emitted topic %q, expected %q", id, msg.Topic, topic)
		}
		items = append(items, models.SnapshotItem{Key: msg.Key, Payload: msg.Payload})
		if msg.Key == "" {
			return nil
		}
		observed[msg.Key] = struct{}{}
		kind := meta.SourceKind
		if msg.SourceKind != "" {
			kind = msg.SourceKind
		}
		result, err := pr.ingester.IngestObservation(ctx, classifier, stores.IngestObservationParams{ProfileID: meta.ProfileID, Topic: topic, Policy: meta.Policy, Current: observationFromMsg(msg, kind, meta.SourceScope)})
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
		pr.recordFailure(ctx, id, err)
		return out, err
	}

	if instance.Absence != nil {
		pr.confirmAbsent(ctx, instance, meta, classifier, observed, &out)
	}

	offset, err := pr.snapshots.AppendSnapshot(ctx, topic, meta.SourceKind, meta.SourceScope, items)
	if err != nil {
		pr.logger.Debug().Err(err).Str("source", id).Msg("pipeline producer: appending source snapshot failed")
		pr.recordFailure(ctx, id, err)
		return out, err
	}
	pr.clearFailure(id)
	out.appended++
	out.lastOffset = offset
	return out, nil
}

// confirmAbsent asks the connector what happened to each item that was in the
// source head but not in this tick's snapshot. Only connectors that declared
// CapConfirmAbsence get here; for the rest an item that stops appearing is
// left to the resurface policy.
func (pr *Producer) confirmAbsent(ctx context.Context, instance connector.Instance, meta connector.Metadata, classifier models.Classifier, observed map[string]struct{}, out *drained) {
	id := instance.Node.ID()
	topic := instance.Node.Topic()

	keys, err := pr.heads.ListActiveKeys(ctx, stores.SourceIdentity{Topic: topic, ProfileID: meta.ProfileID, SourceKind: meta.SourceKind, SourceScope: meta.SourceScope})
	if err != nil {
		pr.logger.Debug().Err(err).Str("source", id).Msg("pipeline producer: listing source head failed")
		return
	}

	prevs := make([]models.Observation, 0, len(keys))
	for _, key := range keys {
		if _, present := observed[key]; present {
			continue
		}
		payload, err := pr.heads.Payload(ctx, topic, key)
		if err != nil {
			pr.logger.Debug().Err(err).Str("source", id).Str("key", key).Msg("pipeline producer: reading source head failed")
			continue
		}
		// source_head persists the source payload, not presentation metadata.
		// Reconstruct the prior observation from that payload so an absence
		// confirmer that starts from prev retains the item's title, URL, and
		// upstream observation time when it returns a hydrated Current.
		prevs = append(prevs, observationFromMsg(Msg{Key: key, Payload: payload}, meta.SourceKind, meta.SourceScope))
	}
	if len(prevs) == 0 {
		return
	}

	verdicts, err := instance.Absence.ConfirmAbsence(ctx, prevs)
	debugPause(ctx, pr.pauseIngest)
	if err != nil {
		// A partial failure still resolves some verdicts; those are ingested
		// below rather than discarded.
		pr.logger.Debug().Err(err).Str("source", id).Msg("pipeline producer: absence confirmation failed")
	}
	for _, prev := range prevs {
		v, ok := verdicts[prev.ExternalID]
		if !ok || v.Current == nil {
			continue
		}
		result, err := pr.ingester.IngestObservation(ctx, classifier, stores.IngestObservationParams{ProfileID: meta.ProfileID, Topic: topic, Policy: meta.Policy, Current: *v.Current})
		if err != nil {
			pr.logger.Debug().Err(err).Str("source", id).Str("key", prev.ExternalID).Msg("pipeline producer: absence ingestion failed")
			continue
		}
		if result.Wrote {
			out.appended++
			out.lastOffset = result.Offset
		}
		// Evict after IngestObservation, regardless of Wrote: the dedup
		// short-circuit still leaves the head row in place, and deleting
		// before the ingest would be undone by its UpsertSourceHead.
		if v.Terminal {
			if err := pr.heads.Delete(ctx, topic, prev.ExternalID); err != nil {
				pr.logger.Debug().Err(err).Str("source", id).Str("key", prev.ExternalID).Msg("pipeline producer: evicting source head failed")
			}
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

// failureNoticeInterval is how often one source's continuing failure is
// re-announced in Activity. A source that stays broken is one condition, not
// one per tick: at the 60s floor, recording every failure would bury a day of
// real events under 1440 copies of the same line, which is how an audit log
// stops being read.
const failureNoticeInterval = time.Hour

// recordFailure announces a source's failure, at most once per
// failureNoticeInterval until it succeeds again. The first failure after a
// success always records — a transition is news; a continuation is not.
//
// Suppression is by source rather than by message: a failure reason routinely
// carries a timestamp or a request id from the tool that produced it, so
// comparing reasons would defeat itself on exactly the persistently broken
// source this exists for.
func (pr *Producer) recordFailure(ctx context.Context, id string, cause error) {
	now := pr.now()

	pr.scheduleMu.Lock()
	last, announced := pr.lastFailure[id]
	if announced && now.Sub(last) < failureNoticeInterval {
		pr.scheduleMu.Unlock()
		return
	}
	pr.lastFailure[id] = now
	pr.scheduleMu.Unlock()

	pr.record(ctx, activity.RefreshFailed(id, cause.Error()))
}

// clearFailure re-arms the notice for a source that completed a tick, so the
// next failure is recorded immediately rather than waiting out the interval.
func (pr *Producer) clearFailure(id string) {
	pr.scheduleMu.Lock()
	defer pr.scheduleMu.Unlock()
	delete(pr.lastFailure, id)
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
func observationFromMsg(msg Msg, sourceKind, sourceScope string) models.Observation {
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
	return models.Observation{ExternalID: msg.Key, Title: wire.Title, URL: wire.URL, SourceKind: sourceKind, SourceScope: sourceScope, ObservedAt: wire.UpdatedAt, Payload: msg.Payload}
}

type genericClassifier struct{}

func (genericClassifier) Classify(previous *models.Observation, current models.Observation) models.Classification {
	if previous == nil {
		return models.Classification{Kind: "observed", Transition: models.TransitionNone, Attention: models.AttentionActivity, Lifecycle: models.LifecycleUnknown, Summary: current.Title}
	}
	return models.Classification{Kind: "updated", Transition: models.TransitionNone, Attention: models.AttentionTrivial, Lifecycle: models.LifecycleUnknown, Summary: current.Title}
}
