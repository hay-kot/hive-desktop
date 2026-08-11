package wailsui

import (
	"context"
	"strings"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/procstats"
)

// DevToolsService exposes the developer-tools panel's backend: the gate that
// decides whether the panel is reachable at all, a sample of what the install
// costs the machine, and two calls that exist only to be timed from the
// frontend (ADR developer-tools-are-reachable-in-a-shipped-build-behind-a-setting).
type DevToolsService struct {
	devtools *app.DevToolsService
}

func NewDevToolsService(devtools *app.DevToolsService) *DevToolsService {
	return &DevToolsService{devtools: devtools}
}

// DevToolsInfo is what the frontend needs before it decides to render the pane.
type DevToolsInfo struct {
	Enabled bool `json:"enabled"`
}

// Info reports whether the developer tools are reachable in this build.
func (s *DevToolsService) Info() DevToolsInfo {
	return DevToolsInfo{Enabled: s.devtools.Enabled()}
}

// RuntimeStats is the frontend-facing sample: the desktop process, the tree
// below it, and the Go runtime's own accounting, with the tree totals
// pre-summed so every caller reads the same number.
type RuntimeStats struct {
	SampledAtUnixMs   int64               `json:"sampledAtUnixMs"`
	UptimeMs          int64               `json:"uptimeMs"`
	Process           procstats.Process   `json:"process"`
	Children          []procstats.Process `json:"children"`
	ChildrenTruncated bool                `json:"childrenTruncated"`
	TotalRSSBytes     uint64              `json:"totalRssBytes"`
	TotalCPUPercent   float64             `json:"totalCpuPercent"`
	Go                procstats.GoRuntime `json:"go"`
}

// Stats samples the process tree and the Go runtime. CPU is a rate against the
// previous call, so a panel polling on an interval reports what is happening
// now rather than an average since launch.
func (s *DevToolsService) Stats(ctx context.Context) (RuntimeStats, error) {
	sample, err := s.devtools.Stats(ctx)
	if err != nil {
		return RuntimeStats{}, err
	}
	return RuntimeStats{
		SampledAtUnixMs:   sample.SampledAt.UnixMilli(),
		UptimeMs:          sample.UptimeMs,
		Process:           sample.Process,
		Children:          sample.Children,
		ChildrenTruncated: sample.ChildrenTruncated,
		TotalRSSBytes:     sample.TotalRSSBytes(),
		TotalCPUPercent:   sample.TotalCPUPercent(),
		Go:                sample.Go,
	}, nil
}

// Ping returns the service's clock in unix milliseconds. It does nothing else
// on purpose: timed from the frontend it prices one empty Wails round trip,
// and the returned instant splits that trip into its inbound and outbound legs.
func (s *DevToolsService) Ping() int64 { return time.Now().UnixMilli() }

// maxEchoBytes caps the payload Echo will build. Large enough to price a
// terminal-sized frame, small enough that a typo in the caller cannot ask the
// process for a gigabyte.
const maxEchoBytes = 4 << 20

// Echo returns a payload of the requested size so the panel can separate the
// fixed cost of a Wails call from the cost of marshalling what it carries.
func (s *DevToolsService) Echo(bytes int) string {
	if bytes <= 0 {
		return ""
	}
	return strings.Repeat("x", min(bytes, maxEchoBytes))
}
