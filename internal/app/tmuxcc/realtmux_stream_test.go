//go:build !server

package tmuxcc

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestMeasureStreamShape reports the shape of the event stream a burst of
// output produces — how many Output events, how big each one is, and how long
// the burst takes to drain. Frame count is what every per-message cost in the
// transport and the webview is multiplied by, so it decides whether coalescing
// is worth anything.
func TestMeasureStreamShape(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real tmux")
	}
	s := startBenchServer(t)
	s.attachEnv(t)
	const slug = "stream"
	s.newSession(t, slug, 1, false)

	client, err := Attach(context.Background(), context.Background(), benchAttachOptions(slug, s.binary))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	events, unsubscribe := client.Subscribe()
	defer unsubscribe()
	drain(events, 500*time.Millisecond) // the attach's own paint

	fill := fillFile(t, filepath.Dir(s.socket))
	started := time.Now()
	s.run(t, "send-keys", "-t", slug, "/bin/cat "+fill, "Enter")

	var bytesSeen, count int
	smallest, largest := 1<<30, 0
	idle := time.NewTimer(3 * time.Second)
	defer idle.Stop()
	var last time.Time
loop:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break loop
			}
			if out, isOutput := ev.(Output); isOutput {
				n := len(out.Data)
				bytesSeen += n
				count++
				smallest = min(smallest, n)
				largest = max(largest, n)
				last = time.Now()
			}
			idle.Reset(400 * time.Millisecond)
		case <-idle.C:
			break loop
		}
	}
	elapsed := last.Sub(started)
	if count == 0 {
		t.Fatal("no output observed")
	}
	t.Logf("events=%d bytes=%d mean=%dB min=%dB max=%dB elapsed=%v rate=%.0f events/s throughput=%.1f MB/s",
		count, bytesSeen, bytesSeen/count, smallest, largest, elapsed.Round(time.Millisecond),
		float64(count)/elapsed.Seconds(), float64(bytesSeen)/elapsed.Seconds()/(1<<20))
}

func drain(events <-chan Event, window time.Duration) {
	deadline := time.NewTimer(window)
	defer deadline.Stop()
	for {
		select {
		case <-events:
		case <-deadline.C:
			return
		}
	}
}

// TestMeasureCoalescingUnderLoad reports the frame count a burst produces for
// consumers of different speeds. Both halves matter: a consumer that keeps up
// must see its output unmerged (merging it would mean latency was added for
// nothing), and a consumer that falls behind — which is what a webview does
// under a fast redraw — must see materially fewer, larger frames.
func TestMeasureCoalescingUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real tmux")
	}
	for _, perFrame := range []time.Duration{0, 100 * time.Microsecond, 500 * time.Microsecond} {
		t.Run(perFrame.String(), func(t *testing.T) {
			s := startBenchServer(t)
			s.attachEnv(t)
			const slug = "coalesce"
			s.newSession(t, slug, 1, false)

			client, err := Attach(context.Background(), context.Background(), benchAttachOptions(slug, s.binary))
			if err != nil {
				t.Fatalf("attach: %v", err)
			}
			defer func() { _ = client.Close(context.Background()) }()

			events, unsubscribe := client.Subscribe()
			defer unsubscribe()
			drain(events, 500*time.Millisecond)

			fill := fillFile(t, filepath.Dir(s.socket))
			s.run(t, "send-keys", "-t", slug, "/bin/cat "+fill, "Enter")

			var bytesSeen, frames, largest int
			idle := time.NewTimer(5 * time.Second)
			defer idle.Stop()
		loop:
			for {
				select {
				case ev, ok := <-events:
					if !ok {
						break loop
					}
					if out, isOutput := ev.(Output); isOutput {
						bytesSeen += len(out.Data)
						frames++
						largest = max(largest, len(out.Data))
						if perFrame > 0 {
							time.Sleep(perFrame)
						}
					}
					idle.Reset(600 * time.Millisecond)
				case <-idle.C:
					break loop
				}
			}
			if frames == 0 {
				t.Fatal("no output observed")
			}
			t.Logf("consumer_cost=%-8v frames=%-6d bytes=%d mean=%dB max=%dB",
				perFrame, frames, bytesSeen, bytesSeen/frames, largest)
		})
	}
}
