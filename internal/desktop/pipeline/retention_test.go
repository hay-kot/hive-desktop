package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/rs/zerolog"
)

type retentionFlowLister struct {
	flows []flow.Flow
}

func (l retentionFlowLister) List() []flow.Flow { return l.flows }

type retentionStore struct {
	mu        sync.Mutex
	consumers [][]string
	calls     chan struct{}
}

func (s *retentionStore) Prune(_ context.Context, consumers []string, _ store.RetentionPolicy) (store.RetentionResult, error) {
	s.mu.Lock()
	s.consumers = append(s.consumers, append([]string(nil), consumers...))
	s.mu.Unlock()
	if s.calls != nil {
		select {
		case s.calls <- struct{}{}:
		default:
		}
	}
	return store.RetentionResult{}, nil
}

func TestMaintenanceTick_UsesOnlyEnabledFlowIDs(t *testing.T) {
	pruner := &retentionStore{}
	maintenance := NewMaintenance(
		pruner,
		retentionFlowLister{flows: []flow.Flow{{ID: "enabled", Enabled: true}, {ID: "disabled", Enabled: false}}},
		store.DefaultRetentionPolicy(),
		time.Hour,
		zerolog.Nop(),
	)

	maintenance.Tick(t.Context())

	pruner.mu.Lock()
	defer pruner.mu.Unlock()
	require.Len(t, pruner.consumers, 1)
	assert.Equal(t, []string{"enabled"}, pruner.consumers[0])
}

func TestMaintenanceStop_WaitsForScheduledLoop(t *testing.T) {
	pruner := &retentionStore{calls: make(chan struct{}, 1)}
	maintenance := NewMaintenance(
		pruner,
		retentionFlowLister{flows: []flow.Flow{{ID: "enabled", Enabled: true}}},
		store.DefaultRetentionPolicy(),
		time.Millisecond,
		zerolog.Nop(),
	)
	maintenance.Start()

	select {
	case <-pruner.calls:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not run on its scheduled interval")
	}
	maintenance.Stop()

	pruner.mu.Lock()
	calls := len(pruner.consumers)
	pruner.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	pruner.mu.Lock()
	defer pruner.mu.Unlock()
	assert.Len(t, pruner.consumers, calls, "Stop must prevent further database maintenance")
}
