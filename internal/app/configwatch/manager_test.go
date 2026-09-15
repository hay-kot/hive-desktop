package configwatch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
)

type testAuthority struct{ topology Topology }

func (a testAuthority) Topology(context.Context) (Topology, error) { return a.topology, nil }
func (a testAuthority) Match(Change) Match                         { return Match{Dirty: true} }

type testWatcher struct {
	events    chan fsnotify.Event
	errors    chan error
	closeOnce sync.Once
}

func newTestWatcher() *testWatcher {
	return &testWatcher{events: make(chan fsnotify.Event, 8), errors: make(chan error, 8)}
}
func (w *testWatcher) Add(string) error    { return nil }
func (w *testWatcher) Remove(string) error { return nil }
func (w *testWatcher) Close() error {
	w.closeOnce.Do(func() { close(w.events); close(w.errors) })
	return nil
}
func (w *testWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *testWatcher) Errors() <-chan error          { return w.errors }

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Options{Debounce: time.Millisecond, ScanInterval: time.Hour})
	require.NoError(t, err)
	return m
}

func fixedRevision(data []byte) configstate.Revision {
	return configstate.AggregateRevision([]configstate.RevisionPart{{Key: "config", State: configstate.Valid, Revision: configstate.BytesRevision(data)}})
}

func nextBatch(t *testing.T, m *Manager) Batch {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	batch, err := m.Next(ctx)
	require.NoError(t, err)
	return batch
}

func nextBatchForRevision(t *testing.T, m *Manager, revision configstate.Revision) Batch {
	t.Helper()
	for range 3 {
		batch := nextBatch(t, m)
		if len(batch.Dirty) == 1 && batch.Dirty[0].ObservedRevision == revision {
			return batch
		}
		m.Ack(t.Context(), batch)
	}
	t.Fatal("configuration scan did not publish the expected revision")
	return Batch{}
}

func TestManagerLogsDetectionModeTransitions(t *testing.T) {
	var logs bytes.Buffer
	m, err := New(Options{Logger: zerolog.New(&logs)})
	require.NoError(t, err)
	state := &sourceState{scanFailures: 2}

	m.mu.Lock()
	m.recordModeLocked(t.Context(), configstate.Flows, state)
	m.recordModeLocked(t.Context(), configstate.Flows, state)
	state.lastObserved = time.Now()
	state.scanFailures = 0
	m.recordModeLocked(t.Context(), configstate.Flows, state)
	m.recordModeLocked(t.Context(), configstate.Flows, state)
	state.watchComplete = true
	m.recordModeLocked(t.Context(), configstate.Flows, state)
	m.recordModeLocked(t.Context(), configstate.Flows, state)
	m.mu.Unlock()

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 3)
	require.JSONEq(t, `{"level":"error","source":"flows","detection_mode":"unavailable","consecutive_scan_failures":2,"message":"configuration detection mode changed"}`, lines[0])
	require.JSONEq(t, `{"level":"warn","source":"flows","detection_mode":"poll","consecutive_scan_failures":0,"message":"configuration detection mode changed"}`, lines[1])
	require.JSONEq(t, `{"level":"info","source":"flows","detection_mode":"notify","consecutive_scan_failures":0,"message":"configuration detection mode changed"}`, lines[2])
}

func TestManagerDirtyDuringReconcile(t *testing.T) {
	m := newTestManager(t)
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: testAuthority{}}))
	_, err := m.Mark(t.Context(), configstate.Actions, configstate.Filesystem)
	require.NoError(t, err)
	first := nextBatch(t, m)
	_, err = m.Mark(t.Context(), configstate.Actions, configstate.Filesystem)
	require.NoError(t, err)
	m.Ack(t.Context(), first)
	second := nextBatch(t, m)
	require.Greater(t, second.Dirty[0].Generation, first.Dirty[0].Generation)
	m.Ack(t.Context(), second)
	require.NoError(t, m.Stop(t.Context()))
}

type namedTestAuthority struct{ path string }

func (a namedTestAuthority) Topology(context.Context) (Topology, error) { return Topology{}, nil }
func (a namedTestAuthority) Match(change Change) Match {
	return Match{Dirty: filepath.Clean(change.Path) == a.path}
}

func TestManagerSlowScan(t *testing.T) {
	m := newTestManager(t)
	watch := newTestWatcher()
	m.newWatcher = func() (watcher, error) { return watch, nil }
	actions := namedTestAuthority{path: filepath.Join(t.TempDir(), "actions.yml")}
	flows := namedTestAuthority{path: filepath.Join(t.TempDir(), "flow.yaml")}
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: actions}))
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Flows, Authority: flows}))

	blocked := make(chan struct{})
	actionsStarted := make(chan struct{})
	flowsInitial := make(chan struct{})
	flowEventScan := make(chan struct{})
	var actionOnce sync.Once
	var flowScans atomic.Int64
	m.scan = func(ctx context.Context, authority Authority, _ topologySynchronizer) (scanResult, error) {
		switch authority {
		case actions:
			actionOnce.Do(func() {
				close(actionsStarted)
				select {
				case <-blocked:
				case <-ctx.Done():
				}
			})
		case flows:
			if flowScans.Add(1) == 1 {
				close(flowsInitial)
			} else {
				close(flowEventScan)
			}
		}
		return scanResult{revision: configstate.BytesRevision([]byte("ok"))}, nil
	}
	require.NoError(t, m.Start(t.Context()))
	<-actionsStarted
	<-flowsInitial
	batch := nextBatch(t, m)
	m.Ack(t.Context(), batch)

	watch.events <- fsnotify.Event{Name: flows.path, Op: fsnotify.Write}
	select {
	case <-flowEventScan:
	case <-time.After(time.Second):
		t.Fatal("watcher event did not schedule the unblocked source scan")
	}
	close(blocked)
	require.NoError(t, m.Stop(t.Context()))
}

func TestManagerStopCancelsActiveScan(t *testing.T) {
	m := newTestManager(t)
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: testAuthority{}}))
	started := make(chan struct{})
	canceled := make(chan struct{})
	m.scan = func(ctx context.Context, _ Authority, _ topologySynchronizer) (scanResult, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return scanResult{}, ctx.Err()
	}
	require.NoError(t, m.Start(t.Context()))
	<-started
	require.NoError(t, m.Stop(t.Context()))
	<-canceled
}

func TestManagerLifecycle(t *testing.T) {
	m := newTestManager(t)
	require.NoError(t, m.Stop(t.Context()))
	require.ErrorIs(t, m.Start(t.Context()), ErrStopped)
	_, err := m.Mark(t.Context(), configstate.Actions, configstate.Scan)
	require.ErrorIs(t, err, ErrStopped)
	_, err = m.Next(t.Context())
	require.ErrorIs(t, err, ErrStopped)
	m.Ack(t.Context(), Batch{})
	require.NoError(t, m.Stop(t.Context()))

	m = newTestManager(t)
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: testAuthority{}}))
	require.NoError(t, m.Start(t.Context()))
	require.NoError(t, m.Start(t.Context()))
	require.ErrorIs(t, m.Register(t.Context(), Registration{Source: configstate.Flows, Authority: testAuthority{}}), ErrAlreadyStarted)
	initial := nextBatch(t, m)
	m.Ack(t.Context(), initial)

	waiting := make(chan error, 1)
	go func() {
		_, err := m.Next(t.Context())
		waiting <- err
	}()
	stopped := make(chan error, 2)
	go func() { stopped <- m.Stop(t.Context()) }()
	go func() { stopped <- m.Stop(t.Context()) }()
	for range 2 {
		require.NoError(t, <-stopped)
	}
	require.ErrorIs(t, <-waiting, ErrStopped)
	m.Ack(t.Context(), Batch{Dirty: []Dirty{{Source: configstate.Actions, Generation: 1}}})
	_, err = m.Mark(t.Context(), configstate.Actions, configstate.Scan)
	require.ErrorIs(t, err, ErrStopped)
}

func TestManagerOverflowRecovery(t *testing.T) {
	m := newTestManager(t)
	timers := newManualTimers()
	m.newRetryTimer = timers.new
	first, second := newTestWatcher(), newTestWatcher()
	var calls, scans atomic.Int64
	watcherRecreated := make(chan struct{})
	m.newWatcher = func() (watcher, error) {
		call := calls.Add(1)
		if call == 1 {
			return first, nil
		}
		close(watcherRecreated)
		return second, nil
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	for _, source := range []configstate.Source{configstate.Actions, configstate.Flows} {
		require.NoError(t, m.Register(t.Context(), Registration{Source: source, Authority: fixedAuthority{dir: dir, path: path}}))
	}
	originalScan := m.scan
	var recovering atomic.Bool
	refreshed := make(chan struct{})
	var refreshedOnce sync.Once
	m.scan = func(ctx context.Context, authority Authority, synchronize topologySynchronizer) (scanResult, error) {
		scans.Add(1)
		result, err := originalScan(ctx, authority, synchronize)
		if recovering.Load() {
			refreshedOnce.Do(func() { close(refreshed) })
		}
		return result, err
	}
	require.NoError(t, m.Start(t.Context()))
	initial := nextBatch(t, m)
	m.Ack(t.Context(), initial)
	initialScans := scans.Load()
	recovering.Store(true)
	first.errors <- fsnotify.ErrEventOverflow
	overflow := nextBatch(t, m)
	require.Len(t, overflow.Dirty, 2)
	for _, dirty := range overflow.Dirty {
		require.Equal(t, configstate.Overflow, dirty.Trigger)
	}
	m.Ack(t.Context(), overflow)
	timers.next(t).fire()
	select {
	case <-watcherRecreated:
	case <-time.After(time.Second):
		t.Fatal("overflow did not recreate the watcher")
	}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("overflow did not refresh source topology")
	}
	require.Greater(t, scans.Load(), initialScans)
	require.NoError(t, m.Stop(t.Context()))
}

func TestManagerScanFailureRetainsTopologyAndRevision(t *testing.T) {
	m := newTestManager(t)
	watch := newTestWatcher()
	m.newWatcher = func() (watcher, error) { return watch, nil }
	firstDir := t.TempDir()
	firstPath := filepath.Join(firstDir, "config.yml")
	require.NoError(t, os.WriteFile(firstPath, []byte("one"), 0o600))
	secondDir := t.TempDir()
	secondPath := filepath.Join(secondDir, "config.yml")
	require.NoError(t, os.WriteFile(secondPath, []byte("two"), 0o600))
	authority := &changingAuthority{topology: Topology{Directories: []string{firstDir}, Files: []AuthorityFile{{Key: "config", Path: firstPath}}}}
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: authority}))

	originalOpen := openFile
	var failRead atomic.Bool
	failed := make(chan struct{})
	openFile = func(path string) (readFile, error) {
		if failRead.Load() && path == secondPath {
			select {
			case failed <- struct{}{}:
			default:
			}
			return nil, errors.New("injected read failure")
		}
		return originalOpen(path)
	}
	t.Cleanup(func() { openFile = originalOpen })

	require.NoError(t, m.Start(t.Context()))
	initial := nextBatch(t, m)
	require.Equal(t, fixedRevision([]byte("one")), initial.Dirty[0].ObservedRevision)
	m.Ack(t.Context(), initial)

	authority.set(Topology{Directories: []string{secondDir}, Files: []AuthorityFile{{Key: "config", Path: secondPath}}})
	failRead.Store(true)
	_, err := m.Mark(t.Context(), configstate.Actions, configstate.Filesystem)
	require.NoError(t, err)
	m.scheduleScan(t.Context(), configstate.Actions, configstate.Scan, false)
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("injected content read did not run")
	}
	timeout := time.After(time.Second)
	var status Status
	for {
		status = m.Status(t.Context())[0]
		if status.ConsecutiveScanFailures == 1 {
			break
		}
		select {
		case <-timeout:
			t.Fatal("injected scan failure did not complete")
		default:
			runtime.Gosched()
		}
	}
	require.Equal(t, fixedRevision([]byte("one")), status.ObservedRevision)
	require.Equal(t, 1, status.ConsecutiveScanFailures)
	m.mu.Lock()
	committedFirst := m.sources[configstate.Actions].directories[firstDir]
	committedSecond := m.sources[configstate.Actions].directories[secondDir]
	m.mu.Unlock()
	require.True(t, committedFirst, "failed scan must retain the last complete topology")
	require.False(t, committedSecond, "failed scan must not commit candidate topology")
	m.watchMu.Lock()
	candidateWatched := m.watched[secondDir]
	m.watchMu.Unlock()
	require.True(t, candidateWatched, "candidate watch must remain after a failed scan")
	require.NoError(t, m.Stop(t.Context()))
}

type changingAuthority struct {
	mu       sync.Mutex
	topology Topology
}

func (a *changingAuthority) Topology(context.Context) (Topology, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.topology, nil
}

func (a *changingAuthority) Match(Change) Match { return Match{Dirty: true} }

func (a *changingAuthority) set(topology Topology) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.topology = topology
}

func TestManagerWatcherConstructionFailureDoesNotStopPolling(t *testing.T) {
	m := newTestManager(t)
	m.newWatcher = func() (watcher, error) { return nil, errors.New("unavailable") }
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: testAuthority{}}))
	require.NoError(t, m.Start(t.Context()))
	batch := nextBatch(t, m)
	require.Equal(t, configstate.Startup, batch.Dirty[0].Trigger)
	m.Ack(t.Context(), batch)
	status := m.Status(t.Context())
	require.Equal(t, "poll", status[0].Mode)
	require.NoError(t, m.Stop(t.Context()))
}

type fixedAuthority struct{ dir, path string }

func (a fixedAuthority) Topology(context.Context) (Topology, error) {
	return Topology{Directories: []string{a.dir}, Files: []AuthorityFile{{Key: "config", Path: a.path}}}, nil
}

func (a fixedAuthority) Match(change Change) Match {
	if change.Path == a.path {
		return Match{Dirty: true}
	}
	if change.Path == a.dir && (change.Operation == Rename || change.Operation == Remove) {
		return Match{Dirty: true, RefreshWatches: true}
	}
	return Match{}
}

func TestManagerAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	m := newTestManager(t)
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: fixedAuthority{dir: dir, path: path}}))
	require.NoError(t, m.Start(t.Context()))
	initial := nextBatch(t, m)
	require.Equal(t, fixedRevision([]byte("one")), initial.Dirty[0].ObservedRevision)
	m.Ack(t.Context(), initial)

	before, err := os.Stat(path)
	require.NoError(t, err)
	tmp := path + ".new"
	require.NoError(t, os.WriteFile(tmp, []byte("two"), 0o600))
	require.NoError(t, os.Rename(tmp, path))
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.False(t, os.SameFile(before, after), "atomic replacement must replace the target inode")
	changed := nextBatchForRevision(t, m, fixedRevision([]byte("two")))
	require.Equal(t, fixedRevision([]byte("two")), changed.Dirty[0].ObservedRevision)
	m.Ack(t.Context(), changed)

	require.NoError(t, os.WriteFile(path, []byte("three"), 0o600))
	saved := nextBatchForRevision(t, m, fixedRevision([]byte("three")))
	require.Equal(t, fixedRevision([]byte("three")), saved.Dirty[0].ObservedRevision)
	m.Ack(t.Context(), saved)
	require.NoError(t, m.Stop(t.Context()))
}

func TestManagerSharedWatchInvalidation(t *testing.T) {
	m := newTestManager(t)
	watch := newTestWatcher()
	var adds int
	m.newWatcher = func() (watcher, error) { return watch, nil }
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	for _, source := range []configstate.Source{configstate.Actions, configstate.Flows} {
		require.NoError(t, m.Register(t.Context(), Registration{Source: source, Authority: fixedAuthority{dir: dir, path: path}}))
	}
	// Count registrations through a wrapper so a shared directory needs one Add.
	m.newWatcher = func() (watcher, error) { return countingWatcher{testWatcher: watch, adds: &adds}, nil }
	require.NoError(t, m.Start(t.Context()))
	initial := nextBatch(t, m)
	m.Ack(t.Context(), initial)
	require.Equal(t, 1, adds)
	watch.events <- fsnotify.Event{Name: dir, Op: fsnotify.Remove}
	batch := nextBatch(t, m)
	require.Len(t, batch.Dirty, 2)
	m.Ack(t.Context(), batch)
	require.NoError(t, m.Stop(t.Context()))
}

type countingWatcher struct {
	testWatcher *testWatcher
	adds        *int
}

func (w countingWatcher) Add(path string) error         { *w.adds++; return w.testWatcher.Add(path) }
func (w countingWatcher) Remove(path string) error      { return w.testWatcher.Remove(path) }
func (w countingWatcher) Close() error                  { return w.testWatcher.Close() }
func (w countingWatcher) Events() <-chan fsnotify.Event { return w.testWatcher.Events() }
func (w countingWatcher) Errors() <-chan error          { return w.testWatcher.Errors() }

func TestManagerMissingRootRetainsRevisionAndRestoresNotify(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	path := filepath.Join(root, "config.yml")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	m, err := New(Options{Debounce: time.Millisecond, ScanInterval: time.Hour})
	require.NoError(t, err)
	timers := newManualTimers()
	m.newRetryTimer = timers.new
	watch := newTestWatcher()
	m.newWatcher = func() (watcher, error) { return pathWatcher{testWatcher: watch}, nil }
	missingScan := make(chan struct{})
	restoredScan := make(chan struct{})
	var successfulScans atomic.Int64
	originalScan := m.scan
	m.scan = func(ctx context.Context, authority Authority, synchronize topologySynchronizer) (scanResult, error) {
		result, err := originalScan(ctx, authority, synchronize)
		if err != nil {
			select {
			case missingScan <- struct{}{}:
			default:
			}
		} else if successfulScans.Add(1) > 1 {
			select {
			case <-restoredScan:
			default:
				close(restoredScan)
			}
		}
		return result, err
	}
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Flows, Authority: fixedAuthority{dir: root, path: path}}))
	require.NoError(t, m.Start(t.Context()))
	initial := nextBatch(t, m)
	require.Equal(t, fixedRevision([]byte("one")), initial.Dirty[0].ObservedRevision)
	m.Ack(t.Context(), initial)

	moved := root + ".gone"
	require.NoError(t, os.Rename(root, moved))
	watch.events <- fsnotify.Event{Name: root, Op: fsnotify.Remove}
	select {
	case <-missingScan:
	case <-time.After(time.Second):
		t.Fatal("root removal did not produce an observation failure")
	}
	status := m.Status(t.Context())[0]
	require.Equal(t, "unavailable", status.Mode)
	require.Equal(t, fixedRevision([]byte("one")), status.ObservedRevision)

	require.NoError(t, os.Rename(moved, root))
	timers.next(t).fire()
	select {
	case <-restoredScan:
	case <-time.After(time.Second):
		t.Fatal("registration retry did not restore the root watch")
	}
	select {
	case <-timers.stopped:
	case <-time.After(time.Second):
		t.Fatal("restored registration did not complete")
	}
	require.Equal(t, "notify", m.Status(t.Context())[0].Mode)
	require.NoError(t, m.Stop(t.Context()))
}

type manualRetryTimer struct {
	events  chan time.Time
	delays  chan<- time.Duration
	stopped chan<- struct{}
}

func (t *manualRetryTimer) C() <-chan time.Time { return t.events }
func (t *manualRetryTimer) Stop() bool {
	t.stopped <- struct{}{}
	return true
}

func (t *manualRetryTimer) Reset(delay time.Duration) bool {
	t.delays <- delay
	return true
}
func (t *manualRetryTimer) fire() { t.events <- time.Now() }

type manualTimers struct {
	created chan *manualRetryTimer
	delays  chan time.Duration
	stopped chan struct{}
}

func newManualTimers() *manualTimers {
	return &manualTimers{created: make(chan *manualRetryTimer, 8), delays: make(chan time.Duration, 16), stopped: make(chan struct{}, 8)}
}

func (t *manualTimers) new(delay time.Duration) retryTimer {
	timer := &manualRetryTimer{events: make(chan time.Time, 1), delays: t.delays, stopped: t.stopped}
	t.delays <- delay
	t.created <- timer
	return timer
}

func (t *manualTimers) nextDelay(tb testing.TB) time.Duration {
	tb.Helper()
	select {
	case delay := <-t.delays:
		return delay
	case <-time.After(time.Second):
		tb.Fatal("retry delay was not scheduled")
		return 0
	}
}

func (t *manualTimers) next(tb testing.TB) *manualRetryTimer {
	tb.Helper()
	select {
	case timer := <-t.created:
		return timer
	case <-time.After(time.Second):
		tb.Fatal("retry timer was not scheduled")
		return nil
	}
}

func TestManagerStopCancelsPendingRetryTimer(t *testing.T) {
	m := newTestManager(t)
	timers := newManualTimers()
	m.newRetryTimer = timers.new
	m.newWatcher = func() (watcher, error) { return nil, errors.New("injected watcher failure") }
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: testAuthority{}}))
	require.NoError(t, m.Start(t.Context()))
	_ = timers.next(t)
	require.NoError(t, m.Stop(t.Context()))
	select {
	case <-timers.stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the pending watcher retry")
	}
}

func TestManagerStopStopsResetRetryTimer(t *testing.T) {
	m := newTestManager(t)
	timers := newManualTimers()
	m.newRetryTimer = timers.new
	watch := newTestWatcher()
	var attempts atomic.Int64
	m.newWatcher = func() (watcher, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("injected watcher failure")
		}
		return watch, nil
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: fixedAuthority{dir: dir, path: path}}))
	require.NoError(t, m.Start(t.Context()))
	timer := timers.next(t)
	initial := nextBatch(t, m)
	m.Ack(t.Context(), initial)
	timer.fire()
	select {
	case <-timers.stopped:
	case <-time.After(time.Second):
		t.Fatal("watch recovery did not reset the retry timer")
	}
	require.NoError(t, m.Stop(t.Context()))
	select {
	case <-timers.stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not stop the reset retry timer")
	}
}

func TestManagerRegistrationRetryBackoffAndRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	m := newTestManager(t)
	timers := newManualTimers()
	m.newRetryTimer = timers.new
	watch := newTestWatcher()
	var attempts atomic.Int64
	m.newWatcher = func() (watcher, error) {
		if attempts.Add(1) <= 7 {
			return nil, errors.New("watcher unavailable")
		}
		return watch, nil
	}
	require.NoError(t, m.Register(t.Context(), Registration{Source: configstate.Actions, Authority: fixedAuthority{dir: dir, path: path}}))
	require.NoError(t, m.Start(t.Context()))
	initial := nextBatch(t, m)
	m.Ack(t.Context(), initial)

	timer := timers.next(t)
	for index, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		require.Equal(t, delay, timers.nextDelay(t))
		if index == 6 {
			require.NoError(t, os.WriteFile(path, []byte("two"), 0o600))
		}
		timer.fire()
	}
	changed := nextBatchForRevision(t, m, fixedRevision([]byte("two")))
	m.Ack(t.Context(), changed)
	select {
	case <-timers.stopped:
	case <-time.After(time.Second):
		t.Fatal("successful registration did not reset the retry delay")
	}
	require.NoError(t, m.Stop(t.Context()))
}

type pathWatcher struct{ testWatcher *testWatcher }

func (w pathWatcher) Add(path string) error         { _, err := os.Stat(path); return err }
func (w pathWatcher) Remove(path string) error      { return nil }
func (w pathWatcher) Close() error                  { return w.testWatcher.Close() }
func (w pathWatcher) Events() <-chan fsnotify.Event { return w.testWatcher.Events() }
func (w pathWatcher) Errors() <-chan error          { return w.testWatcher.Errors() }
