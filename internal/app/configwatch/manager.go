// Package configwatch observes configuration authority files without parsing them.
package configwatch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
)

const (
	defaultDebounce     = 250 * time.Millisecond
	defaultScanInterval = 30 * time.Second
	maxWatchRetry       = 30 * time.Second
)

var (
	ErrStopped         = errors.New("configwatch: manager stopped")
	ErrAlreadyStarted  = errors.New("configwatch: manager already started")
	ErrDuplicateSource = errors.New("configwatch: duplicate source")
	ErrInvalidRegister = errors.New("configwatch: invalid registration")
)

type Operation uint8

const (
	Create Operation = iota + 1
	Write
	Rename
	Remove
)

type Change struct {
	Path      string
	Operation Operation
}

type Match struct {
	Dirty          bool
	RefreshWatches bool
}

type AuthorityFile struct {
	Key  string
	Path string
}

type Topology struct {
	Directories []string
	Files       []AuthorityFile
}

type Authority interface {
	Topology(context.Context) (Topology, error)
	Match(Change) Match
}

type Registration struct {
	Source    configstate.Source
	Authority Authority
}

type Options struct {
	Logger       zerolog.Logger
	Debounce     time.Duration
	ScanInterval time.Duration
}

type Dirty struct {
	Source           configstate.Source
	Trigger          configstate.Trigger
	Generation       uint64
	ObservedRevision configstate.Revision
}

type Batch struct {
	ID    uint64
	Dirty []Dirty
}

type Status struct {
	Source                  configstate.Source
	ObservedRevision        configstate.Revision
	LastObservedAt          time.Time
	Mode                    string
	LastOverflowAt          time.Time
	LastScanAt              time.Time
	ConsecutiveScanFailures int
	Diagnostic              *configstate.Diagnostic
}

type watcher interface {
	Add(string) error
	Remove(string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

type fsWatcher struct{ *fsnotify.Watcher }

func (w fsWatcher) Events() <-chan fsnotify.Event { return w.Watcher.Events }
func (w fsWatcher) Errors() <-chan error          { return w.Watcher.Errors }

type retryTimer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

type scanTicker interface {
	C() <-chan time.Time
	Stop()
}

type standardRetryTimer struct{ *time.Timer }

type standardScanTicker struct{ *time.Ticker }

func (t standardRetryTimer) C() <-chan time.Time { return t.Timer.C }
func (t standardScanTicker) C() <-chan time.Time { return t.Ticker.C }

type sourceState struct {
	registration         Registration
	directories          map[string]bool
	candidateDirectories map[string]bool
	watchComplete        bool

	observed       configstate.Revision
	lastObserved   time.Time
	lastScan       time.Time
	scanFailures   int
	diagnostic     *configstate.Diagnostic
	lastOverflow   time.Time
	lastLoggedMode string

	dirty      bool
	inFlight   bool
	generation uint64
	trigger    configstate.Trigger

	scanning    bool
	scanPending bool
}

// Manager owns shared filesystem detection. It never parses or applies configuration.
type Manager struct {
	logger       zerolog.Logger
	debounce     time.Duration
	scanInterval time.Duration

	mu         sync.Mutex
	topologyMu sync.Mutex
	sources    map[configstate.Source]*sourceState
	started    bool
	stopped    bool
	cancel     context.CancelFunc
	done       chan struct{}
	stopDone   chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	watchMu     sync.Mutex
	watch       watcher
	watched     map[string]bool
	watchAdding map[string]bool

	batchID    uint64
	wake       chan struct{}
	watchRetry chan struct{}

	newWatcher    func() (watcher, error)
	newRetryTimer func(time.Duration) retryTimer
	newScanTicker func(time.Duration) scanTicker
	scan          sourceScanner
}

func New(opts Options) (*Manager, error) {
	if opts.Debounce < 0 || opts.ScanInterval < 0 {
		return nil, fmt.Errorf("configwatch: durations must not be negative")
	}
	if opts.Debounce == 0 {
		opts.Debounce = defaultDebounce
	}
	if opts.ScanInterval == 0 {
		opts.ScanInterval = defaultScanInterval
	}
	return &Manager{
		logger: opts.Logger, debounce: opts.Debounce, scanInterval: opts.ScanInterval,
		sources: make(map[configstate.Source]*sourceState), wake: make(chan struct{}, 1), watchRetry: make(chan struct{}, 1),
		done: make(chan struct{}), stopDone: make(chan struct{}), watched: make(map[string]bool), watchAdding: make(map[string]bool),
		newRetryTimer: func(delay time.Duration) retryTimer { return standardRetryTimer{time.NewTimer(delay)} },
		newScanTicker: func(interval time.Duration) scanTicker { return standardScanTicker{time.NewTicker(interval)} },
		newWatcher: func() (watcher, error) {
			w, err := fsnotify.NewWatcher()
			if err != nil {
				return nil, err
			}
			return fsWatcher{w}, nil
		},
		scan: scanAuthority,
	}, nil
}

func (m *Manager) Register(_ context.Context, registration Registration) error {
	if registration.Source == "" || registration.Authority == nil {
		return ErrInvalidRegister
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return ErrStopped
	}
	if m.started {
		return ErrAlreadyStarted
	}
	if _, ok := m.sources[registration.Source]; ok {
		return ErrDuplicateSource
	}
	m.sources[registration.Source] = &sourceState{registration: registration, directories: make(map[string]bool)}
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return ErrStopped
	}
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wg.Add(1)
	m.mu.Unlock()
	go m.observe(runCtx)
	return nil
}

func (m *Manager) Mark(_ context.Context, source configstate.Source, trigger configstate.Trigger) (uint64, error) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return 0, ErrStopped
	}
	state, ok := m.sources[source]
	if !ok {
		m.mu.Unlock()
		return 0, fmt.Errorf("configwatch: unknown source %q", source)
	}
	state.generation++
	state.dirty = true
	state.trigger = strongerTrigger(state.trigger, trigger)
	generation := state.generation
	m.mu.Unlock()
	m.signal()
	return generation, nil
}

func (m *Manager) Next(ctx context.Context) (Batch, error) {
	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		m.mu.Lock()
		stopped := m.stopped
		m.mu.Unlock()
		if stopped {
			return Batch{}, ErrStopped
		}
		select {
		case <-ctx.Done():
			return Batch{}, ctx.Err()
		case <-m.done:
			return Batch{}, ErrStopped
		case <-m.wake:
			if timer == nil {
				timer = time.NewTimer(m.debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(m.debounce)
			}
		case <-timerC:
			timer = nil
			timerC = nil
			m.mu.Lock()
			batch := m.readyBatchLocked()
			m.mu.Unlock()
			if len(batch.Dirty) > 0 {
				return batch, nil
			}
		}
	}
}

func (m *Manager) Ack(_ context.Context, batch Batch) {
	m.mu.Lock()
	for _, dirty := range batch.Dirty {
		state, ok := m.sources[dirty.Source]
		if !ok {
			continue
		}
		state.inFlight = false
		if state.generation == dirty.Generation {
			state.dirty = false
			state.trigger = ""
		}
	}
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) Status(_ context.Context) []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.sources))
	for source, state := range m.sources {
		out = append(out, Status{
			Source: source, ObservedRevision: state.observed, LastObservedAt: state.lastObserved,
			Mode: sourceMode(state), LastOverflowAt: state.lastOverflow, LastScanAt: state.lastScan,
			ConsecutiveScanFailures: state.scanFailures, Diagnostic: copyDiagnostic(state.diagnostic),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

func (m *Manager) Stop(ctx context.Context) error {
	m.shutdown()
	select {
	case <-m.stopDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) shutdown() {
	m.stopOnce.Do(func() {
		m.topologyMu.Lock()
		m.mu.Lock()
		m.stopped = true
		for _, state := range m.sources {
			state.candidateDirectories = nil
		}
		desired := m.desiredDirectoriesLocked()
		if m.cancel != nil {
			m.cancel()
		}
		close(m.done)
		m.mu.Unlock()
		m.removeObsoleteWatches(desired)
		m.topologyMu.Unlock()
		go func() { m.wg.Wait(); close(m.stopDone) }()
	})
}

func (m *Manager) observe(ctx context.Context) {
	defer m.wg.Done()
	ticker := m.newScanTicker(m.scanInterval)
	defer ticker.Stop()
	var retry retryTimer
	var retryC <-chan time.Time
	retryDelay := time.Second
	initialScan := true

	scheduleRetry := func() {
		if retry == nil {
			retry = m.newRetryTimer(retryDelay)
		} else {
			retry.Reset(retryDelay)
		}
		retryC = retry.C()
		retryDelay = min(retryDelay*2, maxWatchRetry)
	}
	resetRetry := func() {
		retryDelay = time.Second
		if retry != nil {
			if !retry.Stop() {
				select {
				case <-retry.C():
				default:
				}
			}
			retryC = nil
		}
	}
	defer func() {
		if retry != nil {
			retry.Stop()
		}
	}()

	refresh := func() {
		if m.currentWatcher() == nil {
			candidate, err := m.newWatcher()
			if err != nil {
				recordWatchError(ctx, watchErrorRegistration)
				m.logger.Debug().Err(err).Msg("configuration watcher creation failed")
				m.setWatchCoverage(ctx)
				scheduleRetry()
			} else {
				m.installWatcher(ctx, candidate)
			}
		}
		trigger := configstate.Scan
		if initialScan {
			trigger = configstate.Startup
		}
		m.scheduleAllScans(ctx, trigger, initialScan)
		initialScan = false
	}
	refresh()
	for {
		w := m.currentWatcher()
		var events <-chan fsnotify.Event
		var errs <-chan error
		if w != nil {
			events, errs = w.Events(), w.Errors()
		}
		select {
		case <-ctx.Done():
			m.closeWatcher(ctx)
			m.shutdown()
			return
		case <-ticker.C():
			refresh()
		case <-retryC:
			retryC = nil
			refresh()
		case <-m.watchRetry:
			if !m.watchRegistrationComplete() {
				scheduleRetry()
			}
		case event, ok := <-events:
			if !ok {
				m.closeWatcher(ctx)
				scheduleRetry()
				continue
			}
			if m.handleEvent(ctx, event) {
				refresh()
			}
		case err, ok := <-errs:
			if !ok {
				m.closeWatcher(ctx)
				scheduleRetry()
				continue
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				recordWatchError(ctx, watchErrorOverflow)
				m.markAll(configstate.Overflow)
				m.scheduleAllScans(ctx, configstate.Scan, false)
			} else {
				recordWatchError(ctx, watchErrorEventStream)
			}
			m.logger.Debug().Err(err).Msg("configuration watcher stream failed")
			m.closeWatcher(ctx)
			scheduleRetry()
		}
		if m.watchRegistrationComplete() {
			resetRetry()
		}
	}
}

func (m *Manager) currentWatcher() watcher {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	return m.watch
}

func (m *Manager) installWatcher(ctx context.Context, w watcher) {
	m.watchMu.Lock()
	m.watch = w
	m.watched = make(map[string]bool)
	m.watchAdding = make(map[string]bool)
	m.watchMu.Unlock()
	m.setWatchCoverage(ctx)
}

func (m *Manager) closeWatcher(ctx context.Context) {
	m.watchMu.Lock()
	w := m.watch
	m.watch = nil
	m.watched = make(map[string]bool)
	m.watchAdding = make(map[string]bool)
	m.watchMu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	m.setWatchCoverage(ctx)
}

func (m *Manager) handleEvent(ctx context.Context, event fsnotify.Event) bool {
	path := filepath.Clean(event.Name)
	operation := fsOperation(event.Op)
	if operation == 0 {
		return false
	}
	if operation == Rename || operation == Remove {
		m.dropWatch(path)
	}
	refresh := false
	dirtySources := make([]configstate.Source, 0)
	m.mu.Lock()
	for source, state := range m.sources {
		match := state.registration.Authority.Match(Change{Path: path, Operation: operation})
		if state.directories[path] && (operation == Create || operation == Rename || operation == Remove) {
			match.Dirty, match.RefreshWatches = true, true
		}
		if match.Dirty {
			m.markLocked(source, configstate.Filesystem)
			dirtySources = append(dirtySources, source)
		}
		refresh = refresh || match.RefreshWatches
	}
	m.mu.Unlock()
	for _, source := range dirtySources {
		m.scheduleScan(ctx, source, configstate.Scan, false)
	}
	m.refreshWatchCoverage(ctx)
	m.signal()
	return refresh
}

func (m *Manager) dropWatch(path string) {
	m.watchMu.Lock()
	delete(m.watched, path)
	m.watchMu.Unlock()
}

func (m *Manager) addWatch(ctx context.Context, dir string) {
	m.watchMu.Lock()
	w := m.watch
	if w == nil || m.watched[dir] || m.watchAdding[dir] {
		m.watchMu.Unlock()
		return
	}
	m.watchAdding[dir] = true
	m.watchMu.Unlock()

	err := w.Add(dir)
	m.watchMu.Lock()
	delete(m.watchAdding, dir)
	active := m.watch == w
	if err == nil && active {
		m.watched[dir] = true
	}
	m.watchMu.Unlock()
	if err != nil && active {
		m.requestWatchRetry()
		recordWatchError(ctx, watchErrorRegistration)
		m.logger.Debug().Err(err).Str("directory", dir).Msg("configuration watch registration failed")
		return
	}
}

func (m *Manager) publishCandidateTopology(ctx context.Context, source configstate.Source, topology Topology) {
	m.replaceTopology(ctx, source, func(state *sourceState) {
		state.candidateDirectories = topologyDirectories(topology)
	})
}

func (m *Manager) commitTopology(ctx context.Context, source configstate.Source, topology Topology) {
	directories := topologyDirectories(topology)
	m.replaceTopology(ctx, source, func(state *sourceState) {
		state.directories = directories
		state.candidateDirectories = nil
	})
}

func (m *Manager) discardCandidateTopology(ctx context.Context, source configstate.Source) {
	m.topologyMu.Lock()
	defer m.topologyMu.Unlock()

	m.mu.Lock()
	state := m.sources[source]
	if state == nil || m.stopped {
		m.mu.Unlock()
		return
	}
	state.candidateDirectories = nil
	desired := m.desiredDirectoriesLocked()
	m.mu.Unlock()

	m.removeObsoleteWatches(desired)
	m.refreshWatchCoverage(ctx)
}

func (m *Manager) replaceTopology(ctx context.Context, source configstate.Source, replace func(*sourceState)) {
	m.topologyMu.Lock()
	defer m.topologyMu.Unlock()

	m.mu.Lock()
	state := m.sources[source]
	if state == nil || m.stopped {
		m.mu.Unlock()
		return
	}
	replace(state)
	desired := m.desiredDirectoriesLocked()
	m.mu.Unlock()

	m.removeObsoleteWatches(desired)
	for dir := range desired {
		m.addWatch(ctx, dir)
	}
	m.refreshWatchCoverage(ctx)
	if m.watchRegistrationComplete() {
		m.requestWatchRetry()
	}
}

func (m *Manager) desiredDirectoriesLocked() map[string]bool {
	desired := make(map[string]bool)
	for _, state := range m.sources {
		for dir := range state.directories {
			desired[dir] = true
		}
		for dir := range state.candidateDirectories {
			desired[dir] = true
		}
	}
	return desired
}

func topologyDirectories(topology Topology) map[string]bool {
	directories := make(map[string]bool, len(topology.Directories))
	for _, dir := range topology.Directories {
		directories[filepath.Clean(dir)] = true
	}
	return directories
}

func (m *Manager) removeObsoleteWatches(desired map[string]bool) {
	m.watchMu.Lock()
	w := m.watch
	obsolete := make([]string, 0)
	for dir := range m.watched {
		if !desired[dir] {
			delete(m.watched, dir)
			obsolete = append(obsolete, dir)
		}
	}
	m.watchMu.Unlock()
	if w == nil {
		return
	}
	for _, dir := range obsolete {
		_ = w.Remove(dir)
	}
}

func (m *Manager) refreshWatchCoverage(ctx context.Context) {
	m.watchMu.Lock()
	watched := make(map[string]bool, len(m.watched))
	for dir := range m.watched {
		watched[dir] = true
	}
	hasWatcher := m.watch != nil
	m.watchMu.Unlock()
	m.mu.Lock()
	for source, state := range m.sources {
		state.watchComplete = hasWatcher && len(state.directories) > 0
		for dir := range state.directories {
			state.watchComplete = state.watchComplete && watched[dir]
		}
		m.recordModeLocked(ctx, source, state)
	}
	m.mu.Unlock()
}

func (m *Manager) setWatchCoverage(ctx context.Context) {
	m.mu.Lock()
	for source, state := range m.sources {
		state.watchComplete = false
		m.recordModeLocked(ctx, source, state)
	}
	m.mu.Unlock()
}

func (m *Manager) requestWatchRetry() {
	select {
	case m.watchRetry <- struct{}{}:
	default:
	}
}

func (m *Manager) watchRegistrationComplete() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.sources {
		if !state.watchComplete {
			return false
		}
	}
	return len(m.sources) > 0
}

func (m *Manager) scheduleAllScans(ctx context.Context, trigger configstate.Trigger, startup bool) {
	m.mu.Lock()
	sources := make([]configstate.Source, 0, len(m.sources))
	for source := range m.sources {
		sources = append(sources, source)
	}
	m.mu.Unlock()
	for _, source := range sources {
		m.scheduleScan(ctx, source, trigger, startup)
	}
}

func (m *Manager) scheduleScan(ctx context.Context, source configstate.Source, trigger configstate.Trigger, forceDirty bool) {
	m.mu.Lock()
	state := m.sources[source]
	if state == nil || m.stopped {
		m.mu.Unlock()
		return
	}
	if state.scanning {
		state.scanPending = true
		m.mu.Unlock()
		return
	}
	state.scanning = true
	authority := state.registration.Authority
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		// Candidate directories must be watched before reading files, but only a
		// complete scan may replace the source's committed topology.
		result, err := m.scan(ctx, authority, func(topology Topology) {
			m.publishCandidateTopology(ctx, source, topology)
		})
		now := time.Now()
		m.mu.Lock()
		state := m.sources[source]
		if state == nil {
			m.mu.Unlock()
			return
		}
		state.scanning = false
		state.lastScan = now
		changed := false
		if err != nil {
			state.scanFailures++
			state.diagnostic = &configstate.Diagnostic{Stage: configstate.Read, Reason: configstate.SourceUnavailable}
			recordWatchError(ctx, watchErrorScan)
		} else {
			changed = state.observed != result.revision
			state.observed = result.revision
			state.lastObserved = now
			state.scanFailures = 0
			state.diagnostic = nil
		}
		pending := state.scanPending
		state.scanPending = false
		if (forceDirty || (err == nil && changed)) && !m.stopped {
			m.markLocked(source, trigger)
		}
		m.mu.Unlock()
		if err == nil {
			m.commitTopology(ctx, source, result.topology)
		} else {
			m.discardCandidateTopology(ctx, source)
			m.refreshWatchCoverage(ctx)
			if !m.watchRegistrationComplete() {
				m.requestWatchRetry()
			}
		}
		m.signal()
		if pending {
			m.scheduleScan(ctx, source, configstate.Scan, false)
		}
	}()
}

func (m *Manager) markAll(trigger configstate.Trigger) {
	m.mu.Lock()
	now := time.Now()
	for source, state := range m.sources {
		state.lastOverflow = now
		m.markLocked(source, trigger)
	}
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) markLocked(source configstate.Source, trigger configstate.Trigger) {
	state := m.sources[source]
	state.generation++
	state.dirty = true
	state.trigger = strongerTrigger(state.trigger, trigger)
}

func (m *Manager) readyBatchLocked() Batch {
	var dirty []Dirty
	for source, state := range m.sources {
		if !state.dirty || state.inFlight {
			continue
		}
		state.inFlight = true
		dirty = append(dirty, Dirty{Source: source, Trigger: state.trigger, Generation: state.generation, ObservedRevision: state.observed})
	}
	if len(dirty) == 0 {
		return Batch{}
	}
	sort.Slice(dirty, func(i, j int) bool { return dirty[i].Source < dirty[j].Source })
	m.batchID++
	return Batch{ID: m.batchID, Dirty: dirty}
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) recordModeLocked(ctx context.Context, source configstate.Source, state *sourceState) {
	mode := sourceMode(state)
	recordMode(ctx, source, mode)
	if state.lastLoggedMode == mode {
		return
	}
	if mode == "unavailable" && state.lastLoggedMode == "" && state.lastObserved.IsZero() && state.scanFailures == 0 {
		return
	}
	state.lastLoggedMode = mode
	level := zerolog.InfoLevel
	switch mode {
	case "poll":
		level = zerolog.WarnLevel
	case "unavailable":
		level = zerolog.ErrorLevel
	}
	m.logger.WithLevel(level).
		Ctx(ctx).
		Str("source", string(source)).
		Str("detection_mode", mode).
		Int("consecutive_scan_failures", state.scanFailures).
		Msg("configuration detection mode changed")
}

func sourceMode(state *sourceState) string {
	if state.watchComplete {
		return "notify"
	}
	if !state.lastObserved.IsZero() && state.scanFailures == 0 {
		return "poll"
	}
	return "unavailable"
}

func copyDiagnostic(diagnostic *configstate.Diagnostic) *configstate.Diagnostic {
	if diagnostic == nil {
		return nil
	}
	copy := *diagnostic
	return &copy
}

func strongerTrigger(previous, next configstate.Trigger) configstate.Trigger {
	rank := map[configstate.Trigger]int{configstate.Scan: 1, configstate.Filesystem: 2, configstate.Overflow: 3, configstate.Startup: 4, configstate.AppWrite: 5}
	if rank[next] >= rank[previous] {
		return next
	}
	return previous
}

func fsOperation(operation fsnotify.Op) Operation {
	switch {
	case operation.Has(fsnotify.Create):
		return Create
	case operation.Has(fsnotify.Write):
		return Write
	case operation.Has(fsnotify.Rename):
		return Rename
	case operation.Has(fsnotify.Remove):
		return Remove
	default:
		return 0
	}
}
