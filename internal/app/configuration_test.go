package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func newConfigurationTestApp(t *testing.T, ctx context.Context) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv(settings.EnvDataDir, filepath.Join(root, "data"))
	t.Setenv("HIVE_CONFIG", filepath.Join(root, "hive.yaml"))
	t.Setenv(settings.EnvHiveDataDir, filepath.Join(root, "hive"))
	t.Setenv(settings.EnvConfigDir, filepath.Join(root, "config"))
	t.Setenv(settings.EnvMockMode, settings.MockFeed)

	core, err := New(ctx, Config{
		Settings: settings.DefaultSettings(),
		MockMode: settings.MockFeed,
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)
	//nolint:contextcheck // Close owns its bounded shutdown context.
	t.Cleanup(func() { _ = core.Close() })
	return core
}

func TestConfigurationUsesAppLifetimeAfterStartContextCancellation(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(t.Context())
	t.Cleanup(cancelApp)
	core := newConfigurationTestApp(t, appCtx)

	updates := make(chan events.ActionsUpdated, 4)
	cancelSubscription := events.Subscribe(t.Context(), core.Events, "test.configuration-lifetime", events.Buffer(4), func(_ context.Context, event events.ActionsUpdated) {
		updates <- event
	})
	t.Cleanup(cancelSubscription)

	startCtx, cancelStart := context.WithCancel(t.Context())
	require.NoError(t, core.Start(startCtx))
	select {
	case <-updates:
	case <-time.After(5 * time.Second):
		t.Fatal("startup configuration pass did not publish actions")
	}

	cancelStart()
	require.NoError(t, appCtx.Err())
	require.NoError(t, os.WriteFile(core.paths.ActionsPath, []byte("invalid: true\n"), 0o600))
	select {
	case <-updates:
	case <-time.After(5 * time.Second):
		t.Fatal("configuration stopped after Start context cancellation")
	}

	require.NoError(t, core.Close())
	select {
	case <-core.configuration.done:
	case <-time.After(time.Second):
		t.Fatal("configuration reconciliation loop did not stop")
	}
	manager, ok := core.configuration.detector.(*configwatch.Manager)
	require.True(t, ok)
	_, err := manager.Mark(t.Context(), configstate.Actions, configstate.AppWrite)
	require.ErrorIs(t, err, configwatch.ErrStopped)
	require.ErrorIs(t, core.Start(t.Context()), errAppClosed)
}

type fakeConfigurationDetector struct {
	batches chan configwatch.Batch
	acks    chan configwatch.Batch
	started chan struct{}
	stopped chan struct{}

	statusMu sync.RWMutex
	statuses []configwatch.Status

	startOnce sync.Once
	stopOnce  sync.Once
}

func newFakeConfigurationDetector(statuses []configwatch.Status) *fakeConfigurationDetector {
	return &fakeConfigurationDetector{
		batches:  make(chan configwatch.Batch, 8),
		acks:     make(chan configwatch.Batch, 8),
		started:  make(chan struct{}),
		stopped:  make(chan struct{}),
		statuses: append([]configwatch.Status(nil), statuses...),
	}
}

func (d *fakeConfigurationDetector) Start(context.Context) error {
	d.startOnce.Do(func() { close(d.started) })
	return nil
}

func (d *fakeConfigurationDetector) Next(ctx context.Context) (configwatch.Batch, error) {
	select {
	case <-ctx.Done():
		return configwatch.Batch{}, ctx.Err()
	case <-d.stopped:
		return configwatch.Batch{}, configwatch.ErrStopped
	default:
	}
	select {
	case batch := <-d.batches:
		return batch, nil
	case <-d.stopped:
		return configwatch.Batch{}, configwatch.ErrStopped
	case <-ctx.Done():
		return configwatch.Batch{}, ctx.Err()
	}
}

func (d *fakeConfigurationDetector) Ack(ctx context.Context, batch configwatch.Batch) {
	select {
	case d.acks <- batch:
	case <-d.stopped:
	case <-ctx.Done():
	}
}

func (d *fakeConfigurationDetector) Status(context.Context) []configwatch.Status {
	d.statusMu.RLock()
	defer d.statusMu.RUnlock()
	return append([]configwatch.Status(nil), d.statuses...)
}

func (d *fakeConfigurationDetector) setStatuses(statuses []configwatch.Status) {
	d.statusMu.Lock()
	defer d.statusMu.Unlock()
	d.statuses = append([]configwatch.Status(nil), statuses...)
}

func (d *fakeConfigurationDetector) Stop(context.Context) error {
	d.stopOnce.Do(func() { close(d.stopped) })
	return nil
}

func sendConfigurationBatch(t *testing.T, detector *fakeConfigurationDetector, batch configwatch.Batch) {
	t.Helper()
	select {
	case detector.batches <- batch:
	case <-detector.stopped:
		t.Fatal("configuration detector stopped before receiving a batch")
	case <-t.Context().Done():
		t.Fatal("test context stopped before configuration batch was sent")
	case <-time.After(time.Second):
		t.Fatal("configuration detector did not receive a batch")
	}
}

func TestConfigurationDetectionRecovery(t *testing.T) {
	type recoveryCase struct {
		name            string
		statuses        []configwatch.Status
		batches         []configwatch.Batch
		prepare         func(t *testing.T, core *App, detector *fakeConfigurationDetector) func()
		check           func(t *testing.T, core *App, detector *fakeConfigurationDetector, acks []configwatch.Batch)
		actionEvents    int
		workspaceEvents int
	}

	cases := []recoveryCase{
		{
			name:    "missed filesystem notification is recovered by a scan",
			batches: []configwatch.Batch{{ID: 1, Dirty: []configwatch.Dirty{{Source: configstate.Actions, Trigger: configstate.Scan, Generation: 11}}}},
			prepare: func(t *testing.T, core *App, _ *fakeConfigurationDetector) func() {
				require.NoError(t, os.WriteFile(core.paths.ActionsPath, []byte("version: 1\nactions: []\nlaunchers: []\n"), 0o600))
				return nil
			},
			check: func(t *testing.T, core *App, _ *fakeConfigurationDetector, acks []configwatch.Batch) {
				require.Empty(t, core.actionStore.List())
				require.Equal(t, configwatch.Batch{ID: 1, Dirty: []configwatch.Dirty{{Source: configstate.Actions, Trigger: configstate.Scan, Generation: 11}}}, acks[0])
			},
			actionEvents: 1,
		},
		{
			name:     "poll mode reloads after notification registration fails",
			statuses: []configwatch.Status{{Source: configstate.Actions, Mode: "poll"}},
			batches:  []configwatch.Batch{{ID: 2, Dirty: []configwatch.Dirty{{Source: configstate.Actions, Trigger: configstate.Scan, Generation: 12}}}},
			prepare: func(t *testing.T, core *App, _ *fakeConfigurationDetector) func() {
				require.NoError(t, os.WriteFile(core.paths.ActionsPath, []byte("version: 1\nactions: []\nlaunchers: []\n"), 0o600))
				return nil
			},
			check: func(t *testing.T, core *App, _ *fakeConfigurationDetector, acks []configwatch.Batch) {
				require.Equal(t, []configwatch.Status{{Source: configstate.Actions, Mode: "poll"}}, core.configuration.status(t.Context()))
				require.Equal(t, configwatch.Batch{ID: 2, Dirty: []configwatch.Dirty{{Source: configstate.Actions, Trigger: configstate.Scan, Generation: 12}}}, acks[0])
				require.Empty(t, core.actionStore.List())
			},
			actionEvents: 1,
		},
		{
			name:     "root removal retains state and restoration reloads it",
			statuses: []configwatch.Status{{Source: configstate.AgentWorkspaces, Mode: "unavailable"}},
			batches: []configwatch.Batch{
				{ID: 3, Dirty: []configwatch.Dirty{{Source: configstate.AgentWorkspaces, Trigger: configstate.Scan, Generation: 13}}},
				{ID: 4, Dirty: []configwatch.Dirty{{Source: configstate.AgentWorkspaces, Trigger: configstate.Scan, Generation: 14}}},
			},
			prepare: func(t *testing.T, core *App, detector *fakeConfigurationDetector) func() {
				root := core.paths.AgentWorkspacesDir
				retained := filepath.Join(root, "retained")
				require.NoError(t, os.Mkdir(retained, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(retained, "agent-workspace.yaml"), []byte("version: 1\n"), 0o600))
				require.NoError(t, core.agentWorkspaceStore.Reload())
				before := len(core.agentWorkspaceStore.Statuses())
				moved := root + ".gone"
				require.NoError(t, os.Rename(root, moved))
				require.Equal(t, []configwatch.Status{{Source: configstate.AgentWorkspaces, Mode: "unavailable"}}, core.configuration.status(t.Context()))
				return func() {
					require.Len(t, core.agentWorkspaceStore.Statuses(), before)
					require.NoError(t, os.Rename(moved, root))
					restored := filepath.Join(root, "restored")
					require.NoError(t, os.Mkdir(restored, 0o700))
					require.NoError(t, os.WriteFile(filepath.Join(restored, "agent-workspace.yaml"), []byte("version: 1\n"), 0o600))
					detector.setStatuses([]configwatch.Status{{Source: configstate.AgentWorkspaces, Mode: "notify"}})
				}
			},
			check: func(t *testing.T, core *App, _ *fakeConfigurationDetector, acks []configwatch.Batch) {
				require.Equal(t, []configwatch.Status{{Source: configstate.AgentWorkspaces, Mode: "notify"}}, core.configuration.status(t.Context()))
				require.Equal(t, []configwatch.Batch{
					{ID: 3, Dirty: []configwatch.Dirty{{Source: configstate.AgentWorkspaces, Trigger: configstate.Scan, Generation: 13}}},
					{ID: 4, Dirty: []configwatch.Dirty{{Source: configstate.AgentWorkspaces, Trigger: configstate.Scan, Generation: 14}}},
				}, acks)
				_, ok := core.agentWorkspaceStore.Status("restored")
				require.True(t, ok)
			},
			workspaceEvents: 2,
		},
		{
			name: "overflow reloads all sources in fixed order",
			batches: []configwatch.Batch{{ID: 5, Dirty: []configwatch.Dirty{
				{Source: configstate.AgentWorkspaces, Trigger: configstate.Overflow, Generation: 15},
				{Source: configstate.Flows, Trigger: configstate.Overflow, Generation: 16},
				{Source: configstate.Actions, Trigger: configstate.Overflow, Generation: 17},
				{Source: configstate.Settings, Trigger: configstate.Overflow, Generation: 18},
			}}},
			prepare: func(*testing.T, *App, *fakeConfigurationDetector) func() { return nil },
			check: func(t *testing.T, _ *App, _ *fakeConfigurationDetector, acks []configwatch.Batch) {
				require.Equal(t, configwatch.Batch{ID: 5, Dirty: []configwatch.Dirty{
					{Source: configstate.Settings, Trigger: configstate.Overflow, Generation: 18},
					{Source: configstate.Actions, Trigger: configstate.Overflow, Generation: 17},
					{Source: configstate.Flows, Trigger: configstate.Overflow, Generation: 16},
					{Source: configstate.AgentWorkspaces, Trigger: configstate.Overflow, Generation: 15},
				}}, acks[0])
			},
			actionEvents:    1,
			workspaceEvents: 1,
		},
		{
			name: "read and enumeration failures retain state and acknowledge generation",
			batches: []configwatch.Batch{{ID: 6, Dirty: []configwatch.Dirty{
				{Source: configstate.AgentWorkspaces, Trigger: configstate.Scan, Generation: 19},
				{Source: configstate.Flows, Trigger: configstate.Scan, Generation: 20},
				{Source: configstate.Actions, Trigger: configstate.Scan, Generation: 21},
			}}},
			prepare: func(t *testing.T, core *App, _ *fakeConfigurationDetector) func() {
				require.NoError(t, os.WriteFile(filepath.Join(core.paths.FlowsDir, "retained.yaml"), []byte("version: 1\nnodes:\n  - { id: src, type: sources.github, credential: github/octocat, kind: search, query: \\\"is:open\\\" }\n  - { id: sink, type: feed }\nwires:\n  - { from: src, to: sink }\n"), 0o600))
				require.NoError(t, core.flowStore.Reload())
				workspace := filepath.Join(core.paths.AgentWorkspacesDir, "retained")
				require.NoError(t, os.Mkdir(workspace, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(workspace, "agent-workspace.yaml"), []byte("version: 1\n"), 0o600))
				require.NoError(t, core.agentWorkspaceStore.Reload())
				actionsBefore := len(core.actionStore.List())
				flowsBefore := len(core.flowStore.List())
				workspacesBefore := len(core.agentWorkspaceStore.Statuses())
				require.NoError(t, os.Remove(core.paths.ActionsPath))
				require.NoError(t, os.Mkdir(core.paths.ActionsPath, 0o700))
				require.NoError(t, os.Rename(core.paths.FlowsDir, core.paths.FlowsDir+".gone"))
				require.NoError(t, os.Rename(core.paths.AgentWorkspacesDir, core.paths.AgentWorkspacesDir+".gone"))
				return func() {
					require.Len(t, core.actionStore.List(), actionsBefore)
					require.Len(t, core.flowStore.List(), flowsBefore)
					require.Len(t, core.agentWorkspaceStore.Statuses(), workspacesBefore)
				}
			},
			check: func(t *testing.T, core *App, _ *fakeConfigurationDetector, acks []configwatch.Batch) {
				require.Error(t, core.actionStore.Err())
				require.Equal(t, configwatch.Batch{ID: 6, Dirty: []configwatch.Dirty{
					{Source: configstate.Actions, Trigger: configstate.Scan, Generation: 21},
					{Source: configstate.Flows, Trigger: configstate.Scan, Generation: 20},
					{Source: configstate.AgentWorkspaces, Trigger: configstate.Scan, Generation: 19},
				}}, acks[0])
			},
			actionEvents:    1,
			workspaceEvents: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core := newConfigurationTestApp(t, t.Context())
			detector := newFakeConfigurationDetector(tc.statuses)
			core.configuration.detector = detector

			actionUpdates := make(chan events.ActionsUpdated, 4)
			workspaceUpdates := make(chan events.AgentWorkspacesUpdated, 4)
			cancelActions := events.Subscribe(t.Context(), core.Events, "test.configuration-actions", events.Buffer(4), func(_ context.Context, event events.ActionsUpdated) { actionUpdates <- event })
			cancelWorkspaces := events.Subscribe(t.Context(), core.Events, "test.configuration-workspaces", events.Buffer(4), func(_ context.Context, event events.AgentWorkspacesUpdated) { workspaceUpdates <- event })
			t.Cleanup(cancelActions)
			t.Cleanup(cancelWorkspaces)

			runCtx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			require.NoError(t, core.configuration.begin(runCtx, cancel))
			require.NoError(t, core.configuration.start(runCtx))
			select {
			case <-detector.started:
			case <-time.After(time.Second):
				t.Fatal("configuration detector did not start")
			}

			afterFirstAck := tc.prepare(t, core, detector)
			acks := make([]configwatch.Batch, 0, len(tc.batches))
			for index, batch := range tc.batches {
				sendConfigurationBatch(t, detector, batch)
				select {
				case ack := <-detector.acks:
					acks = append(acks, ack)
				case <-time.After(time.Second):
					t.Fatal("configuration batch was not acknowledged")
				}
				if index == 0 && afterFirstAck != nil {
					afterFirstAck()
				}
			}
			for range tc.actionEvents {
				select {
				case <-actionUpdates:
				case <-time.After(time.Second):
					t.Fatal("actions update was not published")
				}
			}
			for range tc.workspaceEvents {
				select {
				case <-workspaceUpdates:
				case <-time.After(time.Second):
					t.Fatal("agent workspace update was not published")
				}
			}
			tc.check(t, core, detector, acks)
		})
	}
}

func TestConfigurationReconcileOrdersDomainsBeforePublishing(t *testing.T) {
	core := newConfigurationTestApp(t, t.Context())
	configuration := core.configuration
	var logs bytes.Buffer
	configuration.logger = zerolog.New(&logs)

	var mu sync.Mutex
	sequence := make([]string, 0, 7)
	record := func(value string) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, value)
	}
	configuration.applySource = func(_ context.Context, source configstate.Source) error {
		record(string(source))
		if source == configstate.Actions {
			return errors.New("private config secret")
		}
		return nil
	}
	configuration.publish = func(context.Context, events.Event) { record("publish") }

	batch := configwatch.Batch{ID: 1, Dirty: []configwatch.Dirty{
		{Source: configstate.AgentWorkspaces, Generation: 1, Trigger: configstate.Filesystem, ObservedRevision: "workspaces-revision"},
		{Source: configstate.Flows, Generation: 1, Trigger: configstate.Filesystem, ObservedRevision: "flows-revision"},
		{Source: configstate.Actions, Generation: 1, Trigger: configstate.Filesystem, ObservedRevision: "actions-revision"},
		{Source: configstate.Settings, Generation: 1, Trigger: configstate.Filesystem, ObservedRevision: "settings-revision"},
	}}
	processed := configuration.reconcile(t.Context(), batch)

	require.Equal(t, []configstate.Source{
		configstate.Settings,
		configstate.Actions,
		configstate.Flows,
		configstate.AgentWorkspaces,
	}, []configstate.Source{processed.Dirty[0].Source, processed.Dirty[1].Source, processed.Dirty[2].Source, processed.Dirty[3].Source})
	require.Equal(t, []string{
		"settings",
		"actions",
		"flows",
		"agent_workspaces",
		"publish",
		"publish",
		"publish",
	}, sequence)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 4)
	require.NotContains(t, logs.String(), "private config secret")

	type reconcileLogEntry struct {
		BatchID          uint64    `json:"batch_id"`
		SourceIndex      int       `json:"source_index"`
		Source           string    `json:"source"`
		Trigger          string    `json:"trigger"`
		ObservedRevision string    `json:"observed_revision"`
		CompletedAt      time.Time `json:"completed_at"`
		Outcome          string    `json:"outcome"`
	}
	entries := make([]reconcileLogEntry, 0, len(lines))
	for _, line := range lines {
		var entry reconcileLogEntry
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}

	for index, source := range []string{"settings", "actions", "flows", "agent_workspaces"} {
		entry := entries[index]
		require.Equal(t, uint64(1), entry.BatchID)
		require.Equal(t, index, entry.SourceIndex)
		require.Equal(t, source, entry.Source)
		require.Equal(t, "filesystem", entry.Trigger)
		require.False(t, entry.CompletedAt.IsZero())
	}
	require.Equal(t, "applied", entries[0].Outcome)
	require.Equal(t, "error", entries[1].Outcome)
	require.Equal(t, "actions-revision", entries[1].ObservedRevision)
	require.Equal(t, "flows-revision", entries[2].ObservedRevision)
}
