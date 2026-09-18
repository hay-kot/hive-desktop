package app

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/metric"

	"github.com/hay-kot/hive-desktop/internal/app/procstats"
)

// DevToolsService backs the in-app developer tools: whether they are reachable
// in this build, and what the install is costing the machine right now. The
// gate is a setting rather than a build tag so the numbers can be read on the
// artifact that ships, where a Vite dev build's are not representative
// (ADR developer-tools-are-reachable-in-a-shipped-build-behind-a-setting).
type DevToolsService struct {
	enabled bool
	sampler *procstats.Sampler
	// Telemetry is not a developer-tools surface, so this remains registered
	// when the panel itself is disabled or closed.
	metrics metric.Registration
}

func newDevToolsService(enabled bool) *DevToolsService {
	sampler := procstats.New(int32(os.Getpid()))
	return &DevToolsService{
		enabled: enabled,
		sampler: sampler,
		metrics: sampler.RegisterMetrics(),
	}
}

// Enabled reports whether this build was asked to expose the developer tools.
func (s *DevToolsService) Enabled() bool { return s.enabled }

// Stats samples the process tree and the Go runtime. CPU is differenced against
// the previous call, so a caller polling on an interval gets a rate and a
// one-off call gets the process's average since it started.
func (s *DevToolsService) Stats(ctx context.Context) (procstats.Stats, error) {
	return s.sampler.Sample(ctx)
}

func (s *DevToolsService) close() error {
	return s.metrics.Unregister()
}
