package ptyterm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	// readChunk is one PTY read. It is the latency/syscall trade: a read returns
	// as soon as the kernel has anything, so this only caps a burst.
	readChunk = 32 << 10
	// hangupGrace is how long a closed master gets to make the shell exit on its
	// own before the process is killed outright. Closing first is what lets the
	// shell hang up its own jobs; killing first orphans them.
	hangupGrace = 250 * time.Millisecond
)

// windowOptions is one tab's spawn.
type windowOptions struct {
	Name        string
	Dir         string
	Command     []string
	Env         []string
	Cols        int
	Rows        int
	ReplayBytes int
}

// ptyWindow is one tab: a PTY master, the process on the far end of it, and the
// replay ring for what it has printed.
type ptyWindow struct {
	id   string
	ring *ring

	file *os.File
	cmd  *exec.Cmd

	mu     sync.Mutex
	name   string
	cols   int
	rows   int
	active bool
	closed bool
	reason string

	readerDone chan struct{}
	closeOnce  sync.Once
}

// spawnWindow starts the command on a new PTY sized to opts. onOutput is called
// from the reader goroutine with a buffer it reuses, and owns writing the bytes
// to the window's ring — the session does both under one lock so a replay
// cannot interleave with live output. onExit fires once, after the reader has
// drained everything the process wrote.
func spawnWindow(id string, opts windowOptions, onOutput func(w *ptyWindow, at time.Time, data []byte), onExit func(windowID, reason string)) (*ptyWindow, error) {
	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("%w: no command", ErrInvalidName)
	}
	cmd := exec.Command(opts.Command[0], opts.Command[1:]...) //nolint:noctx // the child outlives the request; it is closed explicitly
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env

	size := &pty.Winsize{Cols: uint16(opts.Cols), Rows: uint16(opts.Rows)} //nolint:gosec // bounded by validateSize
	file, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return nil, fmt.Errorf("ptyterm: start %s: %w", opts.Command[0], err)
	}

	w := &ptyWindow{
		id:         id,
		ring:       newRing(opts.ReplayBytes),
		file:       file,
		cmd:        cmd,
		name:       opts.Name,
		cols:       opts.Cols,
		rows:       opts.Rows,
		readerDone: make(chan struct{}),
	}
	go w.read(onOutput, onExit)
	return w, nil
}

// read drains the PTY for the life of the process. Draining continues whether
// or not anything is subscribed: the far end blocks on a full PTY buffer, so a
// detached window that stopped being read would stop the process inside it.
func (w *ptyWindow) read(onOutput func(*ptyWindow, time.Time, []byte), onExit func(string, string)) {
	defer close(w.readerDone)

	buf := make([]byte, readChunk)
	for {
		n, err := w.file.Read(buf)
		if n > 0 {
			onOutput(w, time.Now(), buf[:n])
		}
		if err != nil {
			w.finish(exitReason(err, w.cmd))
			onExit(w.id, w.exitReason())
			return
		}
	}
}

// exitReason turns the read error that ends a PTY into something a user can
// read. A closed master reports EIO rather than EOF on Linux, and neither says
// anything about why the shell left, so the process's own status is what
// carries the answer.
func exitReason(readErr error, cmd *exec.Cmd) string {
	waitErr := cmd.Wait()
	var exit *exec.ExitError
	switch {
	case errors.As(waitErr, &exit):
		return fmt.Sprintf("exited with status %d", exit.ExitCode())
	case waitErr != nil:
		return waitErr.Error()
	case readErr == nil || errors.Is(readErr, io.EOF):
		return "exited"
	default:
		return "exited"
	}
}

func (w *ptyWindow) finish(reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.reason = reason
}

func (w *ptyWindow) exitReason() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason
}

func (w *ptyWindow) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

func (w *ptyWindow) write(p []byte) error {
	if w.isClosed() {
		return fmt.Errorf("%w: %s has exited", ErrUnknownWindow, w.id)
	}
	if _, err := w.file.Write(p); err != nil {
		return fmt.Errorf("ptyterm: write to %s: %w", w.id, err)
	}
	return nil
}

// resize is authoritative, not a vote: this window has exactly one client, so
// the size the caller asks for is the size the process is told about.
func (w *ptyWindow) resize(cols, rows int) error {
	if w.isClosed() {
		return fmt.Errorf("%w: %s has exited", ErrUnknownWindow, w.id)
	}
	if err := pty.Setsize(w.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil { //nolint:gosec // bounded by validateSize
		return fmt.Errorf("ptyterm: resize %s: %w", w.id, err)
	}
	w.mu.Lock()
	w.cols, w.rows = cols, rows
	w.mu.Unlock()
	return nil
}

func (w *ptyWindow) rename(name string) {
	w.mu.Lock()
	w.name = name
	w.mu.Unlock()
}

func (w *ptyWindow) setActive(active bool) {
	w.mu.Lock()
	w.active = active
	w.mu.Unlock()
}

func (w *ptyWindow) snapshot() Window {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Window{ID: w.id, Name: w.name, Active: w.active, Width: w.cols, Height: w.rows}
}

// close hangs the window up and waits for its reader to drain. Closing the
// master first raises SIGHUP on the foreground process group, which is what
// gives the shell its chance to hang up its own jobs; a kill goes first only if
// it does not take that chance.
func (w *ptyWindow) close() {
	w.closeOnce.Do(func() {
		_ = w.file.Close()
		select {
		case <-w.readerDone:
			return
		case <-time.After(hangupGrace):
		}
		if w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
		}
		<-w.readerDone
	})
}
