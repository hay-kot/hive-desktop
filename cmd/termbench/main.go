// Command termbench measures the tmux and process-managed terminal backends
// against each other on the same workloads (ADR 0045).
//
// It drives internal/app/tmuxcc and internal/app/ptyterm directly, below the
// HTTP transport and the renderer, so what it reports is the engine rather than
// the socket. Both run the same shell — /bin/sh, not the user's — because a
// prompt that loads a version manager would dominate every number here.
//
//	go run ./cmd/termbench                 # every benchmark, both engines
//	go run ./cmd/termbench -engine pty     # one engine
//	go run ./cmd/termbench -bytes 33554432 # a bigger throughput payload
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/ptyterm"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
)

// doneMarker terminates every command the benchmark sends, so a measurement
// ends when the shell says it is finished rather than when output goes quiet.
//
// doneCommand prints it but does not contain it: the shell echoes the command
// line back before running it, and a marker that survived that round trip would
// stop the measurement on the keystroke instead of on the output.
const (
	doneMarker  = "__termbench_done__"
	doneCommand = `echo __termbench""_done__`
)

// frame is one output chunk, flattened from whichever engine produced it.
type frame struct {
	at   time.Time
	data []byte
}

// engine is the surface both backends are measured through. It is deliberately
// the smallest thing the benchmarks need: anything either engine offers that
// the other does not would make a comparison through it meaningless.
type engine interface {
	name() string
	start(ctx context.Context, slug, dir string) error
	// attach reports the id of the window input is written to.
	attach(ctx context.Context, slug string, cols, rows int) (windowID string, err error)
	subscribe(slug string) (<-chan frame, func(), error)
	write(ctx context.Context, slug, windowID string, p []byte) error
	kill(ctx context.Context, slug string) error
	close(ctx context.Context)
}

func main() {
	var (
		which     = flag.String("engine", "both", "which backend to measure: tmux, pty, or both")
		echoes    = flag.Int("echoes", 200, "keystroke round trips per engine")
		attaches  = flag.Int("attaches", 10, "cold start+attach cycles per engine")
		payload   = flag.Int("bytes", 8<<20, "throughput payload size in bytes")
		cols      = flag.Int("cols", 200, "terminal width")
		rows      = flag.Int("rows", 50, "terminal height")
		keepAlive = flag.Bool("keep", false, "leave the benchmark's sessions running afterwards")
	)
	flag.Parse()

	if err := run(*which, options{
		echoes:    *echoes,
		attaches:  *attaches,
		payload:   *payload,
		cols:      *cols,
		rows:      *rows,
		keepAlive: *keepAlive,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "termbench:", err)
		os.Exit(1)
	}
}

type options struct {
	echoes    int
	attaches  int
	payload   int
	cols      int
	rows      int
	keepAlive bool
}

type result struct {
	engine     string
	coldStart  stats
	firstPaint stats
	echo       stats
	throughput float64 // MiB/s
	delivered  int     // bytes the stream actually carried
	elapsed    time.Duration
}

func run(which string, opts options) error {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "termbench")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	payloadPath, err := writePayload(dir, opts.payload)
	if err != nil {
		return err
	}

	var results []result
	for _, name := range enginesFor(which) {
		eng, err := newEngine(ctx, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", name, err)
			continue
		}
		res, err := measure(ctx, eng, dir, payloadPath, opts)
		if !opts.keepAlive {
			eng.close(ctx)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		results = append(results, res)
	}
	report(results, opts)
	return nil
}

func enginesFor(which string) []string {
	switch which {
	case "tmux", "pty":
		return []string{which}
	default:
		return []string{"tmux", "pty"}
	}
}

func newEngine(ctx context.Context, name string) (engine, error) {
	switch name {
	case "tmux":
		return newTmuxEngine(ctx)
	default:
		return newPtyEngine(), nil
	}
}

// measure runs every benchmark against one engine.
func measure(ctx context.Context, eng engine, dir, payloadPath string, opts options) (result, error) {
	res := result{engine: eng.name()}

	// Cold start: what a session costs from nothing to a stream with bytes on
	// it. Both halves are timed because they are paid at different moments —
	// the start is the click, the first paint is when the pane stops being blank.
	for i := range opts.attaches {
		slug := fmt.Sprintf("termbench-cold-%d", i)
		startedAt := time.Now()
		if err := eng.start(ctx, slug, dir); err != nil {
			return res, fmt.Errorf("start: %w", err)
		}
		windowID, err := eng.attach(ctx, slug, opts.cols, opts.rows)
		if err != nil {
			return res, fmt.Errorf("attach: %w", err)
		}
		res.coldStart.add(time.Since(startedAt))

		events, unsubscribe, err := eng.subscribe(slug)
		if err != nil {
			return res, fmt.Errorf("subscribe: %w", err)
		}
		paintFrom := time.Now()
		// A shell that has already printed its prompt sends nothing until it is
		// prodded, so first paint is measured against a write rather than
		// against the attach: an idle session has no paint to wait for.
		if err := eng.write(ctx, slug, windowID, []byte("\n")); err != nil {
			return res, fmt.Errorf("write: %w", err)
		}
		if _, err := awaitBytes(events, 5*time.Second); err != nil {
			return res, fmt.Errorf("first paint: %w", err)
		}
		res.firstPaint.add(time.Since(paintFrom))
		unsubscribe()
		if err := eng.kill(ctx, slug); err != nil {
			return res, fmt.Errorf("kill: %w", err)
		}
	}

	// Echo and throughput share one session: they measure the steady state, and
	// a fresh session per sample would measure the spawn instead.
	slug := "termbench-live"
	if err := eng.start(ctx, slug, dir); err != nil {
		return res, fmt.Errorf("start: %w", err)
	}
	windowID, err := eng.attach(ctx, slug, opts.cols, opts.rows)
	if err != nil {
		return res, fmt.Errorf("attach: %w", err)
	}
	events, unsubscribe, err := eng.subscribe(slug)
	if err != nil {
		return res, fmt.Errorf("subscribe: %w", err)
	}
	defer unsubscribe()

	drain(events, 300*time.Millisecond)

	// Keystroke round trip: the shell's own echo of one character, which is the
	// path a user feels on every key. Ctrl-U clears the line so the prompt does
	// not grow a 200-character command over the run.
	for range opts.echoes {
		sentAt := time.Now()
		if err := eng.write(ctx, slug, windowID, []byte("x")); err != nil {
			return res, fmt.Errorf("echo write: %w", err)
		}
		if _, err := awaitBytes(events, 5*time.Second); err != nil {
			return res, fmt.Errorf("echo: %w", err)
		}
		res.echo.add(time.Since(sentAt))
		if err := eng.write(ctx, slug, windowID, []byte{0x15}); err != nil {
			return res, fmt.Errorf("echo clear: %w", err)
		}
		drain(events, 5*time.Millisecond)
	}

	// Throughput: a file of known size catted into the pane, timed from the
	// command to the marker that follows it. The delivered count is reported
	// beside the rate because the two engines need not carry the same number of
	// bytes — tmux re-renders the pane, a PTY does not.
	command := fmt.Sprintf("cat %s; %s\n", payloadPath, doneCommand)
	sentAt := time.Now()
	if err := eng.write(ctx, slug, windowID, []byte(command)); err != nil {
		return res, fmt.Errorf("throughput write: %w", err)
	}
	delivered, err := awaitMarker(events, doneMarker, 5*time.Minute)
	if err != nil {
		return res, fmt.Errorf("throughput: %w", err)
	}
	res.elapsed = time.Since(sentAt)
	res.delivered = delivered
	res.throughput = float64(opts.payload) / (1 << 20) / res.elapsed.Seconds()

	if !opts.keepAlive {
		if err := eng.kill(ctx, slug); err != nil {
			return res, fmt.Errorf("kill: %w", err)
		}
	}
	return res, nil
}

// writePayload lays down a file of printable lines. Printable rather than
// random bytes so neither engine spends the run resynchronising an emulator on
// escape sequences that were never meant to be one.
func writePayload(dir string, size int) (string, error) {
	path := filepath.Join(dir, "payload.txt")
	file, err := os.Create(path) //nolint:gosec // a temp dir this process made
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriterSize(file, 1<<20)
	line := strings.Repeat("termbench payload line, filler to a realistic width. ", 2) + "\n"
	for written := 0; written < size; written += len(line) {
		if _, err := writer.WriteString(line); err != nil {
			return "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", err
	}
	return path, nil
}

// awaitBytes waits for the next non-empty chunk.
func awaitBytes(events <-chan frame, timeout time.Duration) (int, error) {
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return 0, io.ErrUnexpectedEOF
			}
			if len(ev.data) > 0 {
				return len(ev.data), nil
			}
		case <-deadline:
			return 0, fmt.Errorf("timed out after %s", timeout)
		}
	}
}

// awaitMarker reads until the marker appears, counting everything it saw. Only
// the tail is searched: the marker cannot straddle more than one chunk boundary,
// and keeping the whole stream to search it would measure this process's
// allocator as much as the engine.
func awaitMarker(events <-chan frame, marker string, timeout time.Duration) (int, error) {
	deadline := time.After(timeout)
	total := 0
	tail := make([]byte, 0, 2*len(marker))
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return total, io.ErrUnexpectedEOF
			}
			total += len(ev.data)
			tail = append(tail, ev.data...)
			if len(tail) > 2*len(marker) {
				tail = tail[len(tail)-2*len(marker):]
			}
			if strings.Contains(string(tail), marker) {
				return total, nil
			}
		case <-deadline:
			return total, fmt.Errorf("timed out after %s having read %d bytes", timeout, total)
		}
	}
}

// drain swallows whatever is already queued, so the next measurement does not
// begin by reading the previous one's tail.
func drain(events <-chan frame, quiet time.Duration) {
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-time.After(quiet):
			return
		}
	}
}

// ---- tmux ----

type tmuxEngine struct {
	manager *tmuxcc.Manager
	binary  string
}

func newTmuxEngine(ctx context.Context) (engine, error) {
	manager := tmuxcc.NewManager(ctx, tmuxcc.ManagerOptions{Logger: zerolog.Nop()})
	if err := manager.Available(ctx); err != nil {
		return nil, err
	}
	binary, err := exec.LookPath("tmux")
	if err != nil {
		return nil, err
	}
	return &tmuxEngine{manager: manager, binary: binary}, nil
}

func (e *tmuxEngine) name() string { return "tmux" }

// start creates the session with `new-session` rather than through hive's spawn
// path: what is being measured is the multiplexer, so the window must hold the
// same bare shell the pty engine runs and nothing else.
func (e *tmuxEngine) start(ctx context.Context, slug, dir string) error {
	if exists, err := e.manager.HasSession(ctx, slug); err == nil && exists {
		return nil
	}
	cmd := exec.CommandContext(ctx, e.binary, "new-session", "-d", "-s", slug, "-c", dir, "/bin/sh")
	cmd.Env = scrubbedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("new-session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *tmuxEngine) attach(ctx context.Context, slug string, cols, rows int) (string, error) {
	windows, err := e.manager.Attach(ctx, slug, cols, rows)
	if err != nil {
		return "", err
	}
	if len(windows) == 0 {
		return "", fmt.Errorf("session %s has no windows", slug)
	}
	return windows[0].ID, nil
}

func (e *tmuxEngine) subscribe(slug string) (<-chan frame, func(), error) {
	events, unsubscribe, err := e.manager.Subscribe(slug)
	if err != nil {
		return nil, nil, err
	}
	out := make(chan frame, 256)
	go func() {
		defer close(out)
		for ev := range events {
			if output, ok := ev.(tmuxcc.Output); ok {
				out <- frame{at: output.At, data: output.Data}
			}
		}
	}()
	return out, unsubscribe, nil
}

func (e *tmuxEngine) write(ctx context.Context, slug, windowID string, p []byte) error {
	client, ok := e.manager.Client(slug)
	if !ok {
		return fmt.Errorf("no client for %s", slug)
	}
	return client.Write(ctx, windowID, p)
}

func (e *tmuxEngine) kill(ctx context.Context, slug string) error {
	_, err := e.manager.KillSession(ctx, slug)
	return err
}

func (e *tmuxEngine) close(ctx context.Context) { _ = e.manager.Stop(ctx) }

// scrubbedEnv drops the inherited tmux client variables so a benchmark run from
// inside tmux creates its sessions on the same server it attaches to.
func scrubbedEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ---- pty ----

type ptyEngine struct{ manager *ptyterm.Manager }

func newPtyEngine() engine {
	return &ptyEngine{manager: ptyterm.NewManager(ptyterm.ManagerOptions{Shell: []string{"/bin/sh"}})}
}

func (e *ptyEngine) name() string { return "pty" }

func (e *ptyEngine) start(ctx context.Context, slug, dir string) error {
	_, err := e.manager.Start(ctx, slug, dir)
	return err
}

func (e *ptyEngine) attach(ctx context.Context, slug string, cols, rows int) (string, error) {
	windows, err := e.manager.Attach(ctx, slug, cols, rows)
	if err != nil {
		return "", err
	}
	if len(windows) == 0 {
		return "", fmt.Errorf("session %s has no windows", slug)
	}
	return windows[0].ID, nil
}

func (e *ptyEngine) subscribe(slug string) (<-chan frame, func(), error) {
	events, unsubscribe, err := e.manager.Subscribe(slug)
	if err != nil {
		return nil, nil, err
	}
	out := make(chan frame, 256)
	go func() {
		defer close(out)
		for ev := range events {
			if output, ok := ev.(ptyterm.Output); ok {
				out <- frame{at: output.At, data: output.Data}
			}
		}
	}()
	return out, unsubscribe, nil
}

func (e *ptyEngine) write(_ context.Context, slug, windowID string, p []byte) error {
	return e.manager.Write(slug, windowID, p)
}

func (e *ptyEngine) kill(_ context.Context, slug string) error {
	_, err := e.manager.Kill(slug)
	return err
}

func (e *ptyEngine) close(ctx context.Context) { _ = e.manager.Stop(ctx) }

// ---- reporting ----

type stats struct{ samples []time.Duration }

func (s *stats) add(d time.Duration) { s.samples = append(s.samples, d) }

func (s *stats) percentile(p float64) time.Duration {
	if len(s.samples) == 0 {
		return 0
	}
	sorted := slices.Clone(s.samples)
	slices.Sort(sorted)
	index := int(p * float64(len(sorted)-1))
	return sorted[index]
}

func (s *stats) mean() time.Duration {
	if len(s.samples) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range s.samples {
		total += d
	}
	return total / time.Duration(len(s.samples))
}

func report(results []result, opts options) {
	if len(results) == 0 {
		fmt.Println("no engines were measured")
		return
	}
	fmt.Printf("\n%d echo round trips, %d cold starts, %s payload, %dx%d\n\n",
		opts.echoes, opts.attaches, humanBytes(opts.payload), opts.cols, opts.rows)

	row := "%-22s" + strings.Repeat("%14s", len(results)) + "\n"
	header := []any{"metric"}
	for _, res := range results {
		header = append(header, res.engine)
	}
	fmt.Printf(row, header...)
	fmt.Println(strings.Repeat("─", 22+14*len(results)))

	printRow(row, "cold start p50", results, func(r result) string { return dur(r.coldStart.percentile(0.5)) })
	printRow(row, "cold start p95", results, func(r result) string { return dur(r.coldStart.percentile(0.95)) })
	printRow(row, "first paint p50", results, func(r result) string { return dur(r.firstPaint.percentile(0.5)) })
	printRow(row, "echo p50", results, func(r result) string { return dur(r.echo.percentile(0.5)) })
	printRow(row, "echo p95", results, func(r result) string { return dur(r.echo.percentile(0.95)) })
	printRow(row, "echo p99", results, func(r result) string { return dur(r.echo.percentile(0.99)) })
	printRow(row, "echo mean", results, func(r result) string { return dur(r.echo.mean()) })
	printRow(row, "throughput", results, func(r result) string { return fmt.Sprintf("%.1f MiB/s", r.throughput) })
	printRow(row, "payload wall time", results, func(r result) string { return dur(r.elapsed) })
	printRow(row, "bytes delivered", results, func(r result) string { return humanBytes(r.delivered) })

	fmt.Println("\nbytes delivered is what the stream carried, which need not equal the payload:")
	fmt.Println("tmux re-renders the pane and reports what it drew, a PTY forwards what was written.")
}

func printRow(format, label string, results []result, value func(result) string) {
	cells := []any{label}
	for _, res := range results {
		cells = append(cells, value(res))
	}
	fmt.Printf(format, cells...)
}

func dur(d time.Duration) string {
	switch {
	case d == 0:
		return "-"
	case d < time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
