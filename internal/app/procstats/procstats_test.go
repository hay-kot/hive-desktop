package procstats

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSampleReadsThisProcess(t *testing.T) {
	stats, err := New(int32(os.Getpid())).Sample(t.Context())
	require.NoError(t, err)

	require.Equal(t, int32(os.Getpid()), stats.Process.PID)
	require.NotZero(t, stats.Process.RSSBytes, "a running process holds resident memory")
	require.Positive(t, stats.Go.Goroutines)
	require.Equal(t, len(stats.Children) >= maxProcesses, stats.ChildrenTruncated)
}

func TestTotalsIncludeTheProcessTree(t *testing.T) {
	stats := Stats{
		Process:  Process{RSSBytes: 100, CPUPercent: 1.5},
		Children: []Process{{RSSBytes: 20, CPUPercent: 0.5}, {RSSBytes: 5, CPUPercent: 2}},
	}
	require.Equal(t, uint64(125), stats.TotalRSSBytes())
	require.InDelta(t, 4.0, stats.TotalCPUPercent(), 0.0001)
}

func TestPercentIgnoresImpossibleWindows(t *testing.T) {
	require.InDelta(t, 50.0, percent(0.5, 1), 0.0001)
	require.InDelta(t, 200.0, percent(4, 2), 0.0001, "a process on several cores exceeds one core's worth")
	require.Zero(t, percent(1, 0), "a zero-length window is not a rate")
	require.Zero(t, percent(-1, 1), "a counter that went backwards is not a rate")
}

// A sampler that outlives the processes it has seen must not keep their marks:
// a terminal-heavy session churns short-lived shells all day.
func TestSamplerForgetsProcessesItNoLongerSees(t *testing.T) {
	s := New(int32(os.Getpid()))
	s.marks[999999] = cpuMark{seconds: 1}

	_, err := s.Sample(t.Context())
	require.NoError(t, err)
	require.NotContains(t, s.marks, int32(999999))
	require.Contains(t, s.marks, s.pid)
}
