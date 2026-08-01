//go:build !server

package tmuxcc

import (
	"context"
	"testing"
	"time"
)

// TestMeasureAttachVolume is a measurement, not an assertion: it reports what
// one attach actually pushes at the renderer — the byte volume and the event
// count — because that, rather than the attach call's own wall time, is what
// the frontend has to decode and write into an emulator before a pane is
// readable. Run it with -v.
func TestMeasureAttachVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real tmux")
	}
	for _, windows := range []int{1, 4, 8} {
		t.Run(name(windows), func(t *testing.T) {
			s := startBenchServer(t)
			s.attachEnv(t)
			const slug = "volume"
			s.newSession(t, slug, windows, true)

			started := time.Now()
			client, err := Attach(context.Background(), context.Background(), benchAttachOptions(slug, s.binary))
			if err != nil {
				t.Fatalf("attach: %v", err)
			}
			defer func() { _ = client.Close(context.Background()) }()
			attachDuration := time.Since(started)

			events, unsubscribe := client.Subscribe()
			defer unsubscribe()

			var outputBytes, outputEvents, otherEvents int
			// The backlog is already complete when Subscribe returns — attach
			// published every snapshot before it answered — so a short idle gap
			// is enough to know it has all been drained.
			idle := time.NewTimer(750 * time.Millisecond)
			defer idle.Stop()
		drain:
			for {
				select {
				case ev, ok := <-events:
					if !ok {
						break drain
					}
					if out, isOutput := ev.(Output); isOutput {
						outputBytes += len(out.Data)
						outputEvents++
					} else {
						otherEvents++
					}
					idle.Reset(250 * time.Millisecond)
				case <-idle.C:
					break drain
				}
			}

			t.Logf("windows=%d attach=%v output_bytes=%d output_events=%d other_events=%d bytes_per_window=%d",
				windows, attachDuration.Round(time.Microsecond), outputBytes, outputEvents, otherEvents,
				outputBytes/windows)
		})
	}
}

func name(windows int) string {
	switch windows {
	case 1:
		return "1window"
	case 4:
		return "4windows"
	default:
		return "8windows"
	}
}
