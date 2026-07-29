package tmuxcc

import "time"

// MetricsSink receives data-plane telemetry. Phase 1 emits bytes, buffer depth
// and pause/resume; FrameLatency is the WebSocket adapter's to report, since
// only it knows when a frame reached the socket.
type MetricsSink interface {
	BytesStreamed(session, window string, n int)
	FrameLatency(session, window string, d time.Duration)
	PauseEvent(session, window string)
	ResumeEvent(session, window string)
	StreamBufferDepth(session, window string, depth int)
}

// NopMetrics discards all telemetry. Passing it instead of nil keeps every
// holder of a MetricsSink free of nil-checks.
var NopMetrics MetricsSink = noopMetrics{}

type noopMetrics struct{}

func (noopMetrics) BytesStreamed(string, string, int)          {}
func (noopMetrics) FrameLatency(string, string, time.Duration) {}
func (noopMetrics) PauseEvent(string, string)                  {}
func (noopMetrics) ResumeEvent(string, string)                 {}
func (noopMetrics) StreamBufferDepth(string, string, int)      {}
