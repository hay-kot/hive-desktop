//go:build !server

package tmuxcc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTmux is an in-memory control-mode server: it satisfies the process seam,
// answers commands with %begin/%end blocks, and lets a test inject arbitrary
// notification lines.
type fakeTmux struct {
	t    *testing.T
	slug string

	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	done    chan struct{}

	closeOnce sync.Once

	mu        sync.Mutex
	num       int
	commands  []string
	windows   []string
	captures  map[string][]string
	failures  map[string]string
	onCommand func(cmd string)
	closed    bool
}

func newFakeTmux(t *testing.T, slug string) *fakeTmux {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	f := &fakeTmux{
		t:        t,
		slug:     slug,
		stdinR:   stdinR,
		stdinW:   stdinW,
		stdoutR:  stdoutR,
		stdoutW:  stdoutW,
		done:     make(chan struct{}),
		captures: map[string][]string{},
		failures: map[string]string{},
	}
	t.Cleanup(f.closeStreams)
	return f
}

func (f *fakeTmux) factory() func(Options) process {
	return func(Options) process { return f }
}

func (f *fakeTmux) Start(context.Context) (io.Writer, io.Reader, error) {
	go f.serve()
	return f.stdinW, f.stdoutR, nil
}

func (f *fakeTmux) Wait() error {
	<-f.done
	return nil
}

func (f *fakeTmux) Kill() error {
	f.closeStreams()
	return nil
}

func (f *fakeTmux) closeStreams() {
	f.closeOnce.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		_ = f.stdoutW.Close()
		_ = f.stdinR.Close()
	})
}

func (f *fakeTmux) serve() {
	defer close(f.done)

	f.mu.Lock()
	f.writeLocked("%begin 100 0 0")
	f.writeLocked("%end 100 0 0")
	f.writeLocked("%session-changed $1 " + f.slug)
	f.mu.Unlock()

	scanner := bufio.NewScanner(f.stdinR)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		cmd := scanner.Text()
		f.mu.Lock()
		f.commands = append(f.commands, cmd)
		hook := f.onCommand
		f.mu.Unlock()
		if hook != nil {
			hook(cmd)
		}
		f.respond(cmd)
	}
}

func (f *fakeTmux) respond(cmd string) {
	f.mu.Lock()
	for prefix, msg := range f.failures {
		if strings.HasPrefix(cmd, prefix) {
			f.mu.Unlock()
			f.reply([]string{msg}, true)
			return
		}
	}
	f.mu.Unlock()

	switch {
	case cmd == "detach":
		f.reply(nil, false)
		f.emit("%exit")
		f.closeStreams()
	case strings.HasPrefix(cmd, "list-windows"):
		f.mu.Lock()
		windows := append([]string(nil), f.windows...)
		f.mu.Unlock()
		f.reply(windows, false)
	case strings.HasPrefix(cmd, "capture-pane"):
		f.mu.Lock()
		lines := f.captures[argAfter(cmd, "-t")]
		f.mu.Unlock()
		f.reply(lines, false)
	case strings.HasPrefix(cmd, "new-window"):
		f.reply([]string{"@9"}, false)
	default:
		f.reply(nil, false)
	}
}

func (f *fakeTmux) reply(lines []string, failed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.num++
	ts, num := 1000+f.num, f.num
	f.writeLocked(fmt.Sprintf("%%begin %d %d 1", ts, num))
	for _, line := range lines {
		f.writeLocked(line)
	}
	guard := "%end"
	if failed {
		guard = "%error"
	}
	f.writeLocked(fmt.Sprintf("%s %d %d 1", guard, ts, num))
}

func (f *fakeTmux) emit(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeLocked(line)
}

func (f *fakeTmux) writeLocked(line string) {
	if f.closed {
		return
	}
	if _, err := io.WriteString(f.stdoutW, line+"\n"); err != nil {
		f.closed = true
	}
}

func (f *fakeTmux) setWindows(lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows = lines
}

func (f *fakeTmux) setCapture(pane string, lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captures[pane] = lines
}

func (f *fakeTmux) sentCommands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func (f *fakeTmux) countCommands(prefix string) int {
	n := 0
	for _, cmd := range f.sentCommands() {
		if strings.HasPrefix(cmd, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeTmux) awaitCommands(t *testing.T, prefix string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.countCommands(prefix) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %q commands, saw %v", want, prefix, f.sentCommands())
}

func argAfter(cmd, flag string) string {
	fields := strings.Fields(cmd)
	for i, field := range fields {
		if field == flag && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// attachFake wires a client to a fake server with one window unless the test
// configured otherwise.
func attachFake(t *testing.T, f *fakeTmux, opts Options) *Client {
	t.Helper()
	if len(f.windows) == 0 {
		f.setWindows("@1 1 %1 claude")
	}
	opts.Slug = f.slug
	if opts.Cols == 0 {
		opts.Cols, opts.Rows = 80, 24
	}
	opts.newProcess = f.factory()

	ctx := t.Context()
	client, err := Attach(ctx, ctx, opts)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.WithoutCancel(ctx)) })
	return client
}

// collect reads events until stop returns true, and fails the test if that
// never happens.
func collect(t *testing.T, ch <-chan Event, stop func(Event) bool) []Event {
	t.Helper()
	var events []Event
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("event channel closed after %d events", len(events))
			}
			events = append(events, ev)
			if stop(ev) {
				return events
			}
		case <-timeout:
			t.Fatalf("timed out after %d events: %#v", len(events), events)
		}
	}
}
