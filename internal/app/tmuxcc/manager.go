package tmuxcc

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// minMajor/minMinor is the control-mode floor: 3.2 is where pause mode and
// subscriptions landed, and it is the version this client is written against.
const (
	minMajor = 3
	minMinor = 2
)

// ManagerOptions configures the client set.
type ManagerOptions struct {
	Metrics     MetricsSink
	Logger      zerolog.Logger
	BufferBytes int

	// Binary answers which tmux to exec, and is consulted on every failed
	// availability probe rather than once at startup, so installing tmux does not
	// need a relaunch. nil means $PATH.
	Binary func() (string, error)

	versionProbe func(context.Context, string) (string, error)
	newProcess   func(Options) process
}

// managedClient carries the generation its registration was made under, so a
// teardown that fires late cannot evict the client that replaced it.
type managedClient struct {
	gen    uint64
	client *Client
	cancel context.CancelFunc
}

// Manager owns one control-mode client per session slug.
type Manager struct {
	log         zerolog.Logger
	metrics     MetricsSink
	locate      func() (string, error)
	probe       func(context.Context, string) (string, error)
	newProcess  func(Options) process
	bufferBytes int

	// The app-lifetime context lives in this closure rather than in a field:
	// every client context descends from it, and Stop cancels the lot.
	derive    func() (context.Context, context.CancelFunc)
	cancelAll context.CancelFunc

	attachMu sync.Mutex

	mu      sync.Mutex
	clients map[string]*managedClient
	gen     uint64
	stopped bool
	probed  bool
	binary  string

	stopOnce sync.Once
}

// NewManager builds the client set. ctx is the app's lifetime context: every
// tmux client's goroutines descend from it.
func NewManager(ctx context.Context, opts ManagerOptions) *Manager {
	lifetime, cancel := context.WithCancel(ctx)

	m := &Manager{
		log:         opts.Logger,
		metrics:     opts.Metrics,
		locate:      opts.Binary,
		probe:       opts.versionProbe,
		newProcess:  opts.newProcess,
		bufferBytes: opts.BufferBytes,
		cancelAll:   cancel,
		derive:      func() (context.Context, context.CancelFunc) { return context.WithCancel(lifetime) },
		clients:     map[string]*managedClient{},
	}
	if m.metrics == nil {
		m.metrics = NopMetrics
	}
	if m.locate == nil {
		m.locate = func() (string, error) { return defaultBinary, nil }
	}
	if m.probe == nil {
		m.probe = tmuxVersion
	}
	return m
}

// Available reports nil when a usable tmux (>= 3.2) is present on a supported
// build and platform, else ErrUnavailable. A successful probe is cached along
// with the binary it ran; a failing one is retried, so installing tmux does not
// require a restart.
func (m *Manager) Available(ctx context.Context) error {
	if !platformSupported() {
		return ErrUnavailable
	}

	m.mu.Lock()
	probed := m.probed
	m.mu.Unlock()
	if probed {
		return nil
	}

	binary, err := m.locate()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	raw, err := m.probe(ctx, binary)
	if err != nil {
		return fmt.Errorf("%w: %s not usable: %w", ErrUnavailable, binary, err)
	}
	if !versionAtLeast(raw, minMajor, minMinor) {
		return fmt.Errorf("%w: %s is %s, older than %d.%d", ErrUnavailable, binary, strings.TrimSpace(raw), minMajor, minMinor)
	}

	m.mu.Lock()
	m.probed = true
	m.binary = binary
	m.mu.Unlock()
	m.log.Info().Str("tmux", binary).Str("version", strings.TrimSpace(raw)).Msg("tmux control mode available")
	return nil
}

// Attach opens, or returns the windows of, the client for slug. A second
// attach for a live slug reuses it; after a client exits its slug is free
// again and a re-attach starts fresh.
func (m *Manager) Attach(ctx context.Context, slug string, cols, rows int) ([]Window, error) {
	if err := m.Available(ctx); err != nil {
		return nil, err
	}
	if err := validateSize(cols, rows); err != nil {
		return nil, err
	}
	if slug == "" {
		return nil, fmt.Errorf("%w: empty slug", ErrNotAttached)
	}

	m.attachMu.Lock()
	defer m.attachMu.Unlock()

	if mc, ok := m.managed(slug); ok {
		if _, dead := mc.client.exited(); !dead {
			return mc.client.Windows(), nil
		}
		m.remove(slug, mc.gen)
	}

	m.mu.Lock()
	stopped := m.stopped
	binary := m.binary
	m.gen++
	gen := m.gen
	m.mu.Unlock()
	if stopped {
		return nil, ErrUnavailable
	}

	lifetime, cancel := m.derive()
	client, err := Attach(ctx, lifetime, Options{
		Slug:        slug,
		Cols:        cols,
		Rows:        rows,
		Binary:      binary,
		BufferBytes: m.bufferBytes,
		Metrics:     m.metrics,
		Logger:      m.log,
		OnExit:      func(slug, _ string) { m.remove(slug, gen) },
		newProcess:  m.newProcess,
	})
	if err != nil {
		cancel()
		return nil, err
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		_ = client.Close(ctx)
		cancel()
		return nil, ErrUnavailable
	}
	// Teardown can fire before Attach returns, and its OnExit then has nothing
	// to remove. Reading the flag under the lock the entry is stored under
	// leaves no gap: either it is already set and we never register, or OnExit
	// is still waiting on this lock and evicts what we just stored.
	if reason, dead := client.exited(); dead {
		m.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("%w: %s exited during attach: %s", ErrNotAttached, slug, reason)
	}
	m.clients[slug] = &managedClient{gen: gen, client: client, cancel: cancel}
	m.mu.Unlock()

	return client.Windows(), nil
}

func (m *Manager) Client(slug string) (*Client, bool) {
	mc, ok := m.managed(slug)
	if !ok {
		return nil, false
	}
	return mc.client, true
}

func (m *Manager) managed(slug string) (*managedClient, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mc, ok := m.clients[slug]
	return mc, ok
}

// Subscribe returns slug's event channel and unsubscribe func. There is one
// active subscriber per slug; a new call closes the previous channel. Events
// buffered before the call — first paint included — replay on the new one.
func (m *Manager) Subscribe(slug string) (<-chan Event, func(), error) {
	c, ok := m.Client(slug)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotAttached, slug)
	}
	ch, unsubscribe := c.Subscribe()
	return ch, unsubscribe, nil
}

// Detach closes slug's client. Unknown slugs are a no-op.
func (m *Manager) Detach(ctx context.Context, slug string) error {
	mc, ok := m.managed(slug)
	if !ok {
		return nil
	}
	err := mc.client.Close(ctx)
	m.remove(slug, mc.gen)
	return err
}

// Stop closes every client, joins their readers, and cancels the app-lifetime
// context. Idempotent.
func (m *Manager) Stop(ctx context.Context) error {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.stopped = true
		clients := make([]*managedClient, 0, len(m.clients))
		for _, mc := range m.clients {
			clients = append(clients, mc)
		}
		m.clients = map[string]*managedClient{}
		m.mu.Unlock()

		for _, mc := range clients {
			_ = mc.client.Close(ctx)
			mc.cancel()
		}
		m.cancelAll()
	})
	return nil
}

// remove evicts slug only if it still holds the registration gen identifies. A
// client that exits after its slug was re-attached must not take the new client
// with it.
func (m *Manager) remove(slug string, gen uint64) {
	m.mu.Lock()
	mc, ok := m.clients[slug]
	if !ok || mc.gen != gen {
		m.mu.Unlock()
		return
	}
	delete(m.clients, slug)
	m.mu.Unlock()
	mc.cancel()
}

func tmuxVersion(ctx context.Context, binary string) (string, error) {
	out, err := exec.CommandContext(ctx, binary, "-V").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// versionAtLeast reads the first major.minor in a `tmux -V` string, which may
// be decorated ("tmux 3.2a", "tmux next-3.4").
func versionAtLeast(raw string, major, minor int) bool {
	i := strings.IndexFunc(raw, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 {
		return false
	}
	rest := raw[i:]
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	gotMajor, err := strconv.Atoi(rest[:j])
	if err != nil {
		return false
	}
	gotMinor := 0
	if j < len(rest) && rest[j] == '.' {
		rest = rest[j+1:]
		k := 0
		for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
			k++
		}
		gotMinor, _ = strconv.Atoi(rest[:k])
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}
