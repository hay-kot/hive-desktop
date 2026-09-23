// Package adoption reports anonymous daily installation activity.
package adoption

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	stateFileName  = "adoption.json"
	eventName      = "app_daily_active"
	dateLayout     = "2006-01-02"
	checkInterval  = time.Hour
	requestTimeout = 10 * time.Second
)

// Options configures anonymous adoption reporting. An empty project token or
// endpoint disables reporting without creating local state.
type Options struct {
	ProjectToken string
	Endpoint     string
	StateDir     string
	Version      string
	Channel      string
	Enabled      bool
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Reporter owns the daily reporting loop and its installation-local state.
type Reporter struct {
	configured   bool
	enabled      atomic.Bool
	captureURL   string
	projectToken string
	version      string
	channel      string
	state        *stateStore
	client       httpDoer
	logger       zerolog.Logger
	now          func() time.Time
	interval     time.Duration

	startOnce     sync.Once
	stopOnce      sync.Once
	cancel        context.CancelFunc
	done          chan struct{}
	wake          chan struct{}
	reportMu      sync.Mutex
	requestMu     sync.Mutex
	requestCancel context.CancelFunc
}

// New returns a reporter. Reporting is disabled when either build-time value
// is absent.
func New(opts Options, logger zerolog.Logger) (*Reporter, error) {
	reporter := &Reporter{
		logger:   logger,
		now:      time.Now,
		interval: checkInterval,
		done:     make(chan struct{}),
		wake:     make(chan struct{}, 1),
	}
	if opts.ProjectToken == "" || opts.Endpoint == "" {
		return reporter, nil
	}
	captureURL, err := captureEndpoint(opts.Endpoint)
	if err != nil {
		return reporter, err
	}
	if opts.StateDir == "" {
		return reporter, errors.New("adoption state directory is required")
	}

	reporter.configured = true
	reporter.enabled.Store(opts.Enabled)
	reporter.captureURL = captureURL
	reporter.projectToken = opts.ProjectToken
	reporter.version = opts.Version
	reporter.channel = opts.Channel
	reporter.state = &stateStore{path: filepath.Join(opts.StateDir, stateFileName)}
	reporter.client = &http.Client{Timeout: requestTimeout}
	return reporter, nil
}

// Configured reports whether this build carries a PostHog destination.
func (r *Reporter) Configured() bool { return r.configured }

// SetEnabled applies the user's current analytics preference. Disabling also
// cancels a capture already waiting on the network.
func (r *Reporter) SetEnabled(enabled bool) {
	r.enabled.Store(enabled)
	if !enabled {
		r.requestMu.Lock()
		if r.requestCancel != nil {
			r.requestCancel()
		}
		r.requestMu.Unlock()
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Start begins reporting. The first attempt runs immediately, then failed or
// newly due days are checked hourly.
func (r *Reporter) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		if !r.configured {
			close(r.done)
			return
		}
		runCtx, cancel := context.WithCancel(ctx)
		r.cancel = cancel
		go r.run(runCtx)
	})
}

// Stop cancels an in-flight request and waits for the reporting loop to exit.
func (r *Reporter) Stop(ctx context.Context) error {
	r.stopOnce.Do(func() {
		r.startOnce.Do(func() { close(r.done) })
		if r.cancel != nil {
			r.cancel()
		}
	})

	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Reporter) run(ctx context.Context) {
	defer close(r.done)
	r.report(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.report(ctx)
		case <-r.wake:
			r.report(ctx)
		}
	}
}

func (r *Reporter) report(ctx context.Context) {
	if err := r.reportIfDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Debug().Err(err).Msg("anonymous adoption ping failed")
	}
}

func (r *Reporter) reportIfDue(ctx context.Context) error {
	r.reportMu.Lock()
	defer r.reportMu.Unlock()

	requestCtx, finish, ok := r.beginRequest(ctx)
	if !ok {
		return nil
	}
	defer finish()

	state, err := r.state.loadOrCreate()
	if err != nil {
		return err
	}
	now := r.now().UTC()
	today := now.Format(dateLayout)
	if state.LastActiveDate == today {
		return nil
	}

	payload := capturePayload{
		APIKey:     r.projectToken,
		Event:      eventName,
		DistinctID: state.InstallationID,
		Timestamp:  now.Format(time.RFC3339),
		Properties: captureProperties{
			ProcessPersonProfile: false,
			DisableGeoIP:         true,
			Version:              r.version,
			Channel:              r.channel,
			OS:                   runtime.GOOS,
			Arch:                 runtime.GOARCH,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode adoption event: %w", err)
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, r.captureURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create adoption request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "hive-desktop/"+r.version)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send adoption event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("send adoption event: HTTP %d", resp.StatusCode)
	}

	state.LastActiveDate = today
	if err := r.state.save(state); err != nil {
		return fmt.Errorf("record adoption event: %w", err)
	}
	return nil
}

func (r *Reporter) beginRequest(ctx context.Context) (context.Context, func(), bool) {
	r.requestMu.Lock()
	defer r.requestMu.Unlock()
	if !r.enabled.Load() {
		return nil, nil, false
	}
	requestCtx, cancel := context.WithCancel(ctx)
	r.requestCancel = cancel
	return requestCtx, func() {
		r.requestMu.Lock()
		r.requestCancel = nil
		r.requestMu.Unlock()
		cancel()
	}, true
}

type capturePayload struct {
	APIKey     string            `json:"api_key"`
	Event      string            `json:"event"`
	DistinctID string            `json:"distinct_id"`
	Timestamp  string            `json:"timestamp"`
	Properties captureProperties `json:"properties"`
}

type captureProperties struct {
	ProcessPersonProfile bool   `json:"$process_person_profile"`
	DisableGeoIP         bool   `json:"$geoip_disable"`
	Version              string `json:"app_version,omitempty"`
	Channel              string `json:"release_channel,omitempty"`
	OS                   string `json:"os"`
	Arch                 string `json:"arch"`
}

func captureEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse adoption endpoint: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return "", errors.New("adoption endpoint must be an https origin")
	}
	parsed.Path = "/i/v0/e/"
	return parsed.String(), nil
}

type stateFile struct {
	InstallationID string `json:"installation_id"`
	LastActiveDate string `json:"last_active_date,omitempty"`
}

type stateStore struct {
	path string
	mu   sync.Mutex
}

func (s *stateStore) loadOrCreate() (stateFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err == nil {
		var state stateFile
		if json.Unmarshal(raw, &state) == nil {
			if _, parseErr := uuid.Parse(state.InstallationID); parseErr == nil {
				return state, nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return stateFile{}, fmt.Errorf("read adoption state: %w", err)
	}

	state := stateFile{InstallationID: uuid.NewString()}
	if err := s.saveLocked(state); err != nil {
		return stateFile{}, err
	}
	return state, nil
}

func (s *stateStore) save(state stateFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(state)
}

func (s *stateStore) saveLocked(state stateFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create adoption state directory: %w", err)
	}
	contents, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode adoption state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), stateFileName+".*")
	if err != nil {
		return fmt.Errorf("stage adoption state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(append(contents, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write adoption state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write adoption state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace adoption state: %w", err)
	}
	return nil
}
