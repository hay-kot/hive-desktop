//go:build !server

package tmuxcc

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMeasureAttachPhases breaks one attach into the phases it is actually made
// of, so an optimization targets the phase that costs rather than the one that
// looks expensive. Run with -v.
func TestMeasureAttachPhases(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real tmux")
	}
	s := startBenchServer(t)
	s.attachEnv(t)
	const slug = "phases"
	s.newSession(t, slug, 4, true)

	client, err := Attach(context.Background(), context.Background(), benchAttachOptions(slug, s.binary))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	windows := client.Windows()
	pane := windows[0].ActivePane

	// Each command is timed on an already-attached client, so what is measured
	// is the round trip and tmux's own work — not the process spawn.
	for _, tc := range []struct {
		label string
		cmd   string
	}{
		{"display-message(cursor)", `display-message -p -t ` + pane + ` "` + cursorFormat + `"`},
		{"capture-pane(history)", fmt.Sprintf("capture-pane -pe -J -S -%d -E -1 -t %s", historyLines, pane)},
		{"capture-pane(screen)", "capture-pane -pe -S 0 -t " + pane},
		{"list-windows", `list-windows -F "` + listWindowsFormat + `"`},
	} {
		const runs = 20
		var total time.Duration
		var lines, bytes int
		for range runs {
			started := time.Now()
			out, err := client.gw.Send(context.Background(), tc.cmd)
			total += time.Since(started)
			if err != nil {
				t.Fatalf("%s: %v", tc.label, err)
			}
			lines = len(out)
			bytes = 0
			for _, l := range out {
				bytes += len(l)
			}
		}
		t.Logf("%-26s avg=%-10v lines=%-6d bytes=%d", tc.label, (total / runs).Round(time.Microsecond), lines, bytes)
	}

	// The whole per-pane snapshot, for comparison with the sum of its parts.
	const runs = 20
	var total time.Duration
	for range runs {
		started := time.Now()
		if _, err := client.snapshot(context.Background(), pane, 50); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		total += time.Since(started)
	}
	t.Logf("%-26s avg=%v", "snapshot(3 commands)", (total / runs).Round(time.Microsecond))
}

// TestMeasureCaptureVariants times the capture-pane formulations the snapshot
// could use, to see whether any flag is carrying avoidable cost.
func TestMeasureCaptureVariants(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real tmux")
	}
	s := startBenchServer(t)
	s.attachEnv(t)
	const slug = "variants"
	s.newSession(t, slug, 1, true)

	client, err := Attach(context.Background(), context.Background(), benchAttachOptions(slug, s.binary))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()
	pane := client.Windows()[0].ActivePane

	for _, tc := range []struct{ label, cmd string }{
		{"-pe -J (current)", fmt.Sprintf("capture-pane -pe -J -S -%d -E -1 -t %s", historyLines, pane)},
		{"-pe    (no join)", fmt.Sprintf("capture-pane -pe -S -%d -E -1 -t %s", historyLines, pane)},
		{"-p  -J (no escapes)", fmt.Sprintf("capture-pane -p -J -S -%d -E -1 -t %s", historyLines, pane)},
		{"-pe -J 500 lines", fmt.Sprintf("capture-pane -pe -J -S -%d -E -1 -t %s", 500, pane)},
		{"-pe -J 1000 lines", fmt.Sprintf("capture-pane -pe -J -S -%d -E -1 -t %s", 1000, pane)},
	} {
		const runs = 20
		var total time.Duration
		var lines, bytes int
		for range runs {
			started := time.Now()
			out, err := client.gw.Send(context.Background(), tc.cmd)
			total += time.Since(started)
			if err != nil {
				t.Fatalf("%s: %v", tc.label, err)
			}
			lines, bytes = len(out), 0
			for _, l := range out {
				bytes += len(l)
			}
		}
		t.Logf("%-22s avg=%-10v lines=%-6d bytes=%d", tc.label, (total / runs).Round(time.Microsecond), lines, bytes)
	}
}
