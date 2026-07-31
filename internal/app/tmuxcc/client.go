// Package tmuxcc is a tmux control-mode (`tmux -C`) client: it attaches to one
// existing tmux session, models that session's windows, streams each window's
// decoded output, and drives tmux with ordinary commands. It owns no transport
// and no UI — an adapter subscribes to a client's event stream and renders it.
package tmuxcc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	minDimension = 1
	maxDimension = 1000

	attachTimeout = 10 * time.Second
	detachTimeout = 2 * time.Second

	// sendKeysChunk keeps one send-keys command far inside tmux's command
	// length limit once each byte is expanded to hex.
	sendKeysChunk = 64

	// The window name goes last: it is the only field that can contain spaces.
	listWindowsFormat = "#{window_id} #{window_active} #{pane_id} #{window_width} #{window_height} #{window_name}"

	// The pane's cursor as an emulator addresses it: 0-based row, then column.
	cursorFormat = "#{cursor_y} #{cursor_x}"

	// historyLines bounds the scrollback a first paint replays. It is tmux's own
	// default history-limit, so on an unconfigured tmux it is the whole history
	// rather than a bound anyone runs into, and it costs a few hundred KB per
	// window against an 8 MiB broker.
	historyLines = 2000
)

var (
	// ErrUnavailable is returned on the server build, on unsupported platforms,
	// and when tmux is missing or older than 3.2.
	ErrUnavailable = errors.New("tmuxcc: control mode unavailable")
	// ErrInvalidSize is returned for out-of-bounds cols/rows.
	ErrInvalidSize = errors.New("tmuxcc: invalid terminal size")
	// ErrNotAttached is returned for a slug with no live client.
	ErrNotAttached = errors.New("tmuxcc: no client attached")
	// ErrUnknownWindow is returned for a window id absent from the window set.
	ErrUnknownWindow = errors.New("tmuxcc: unknown window")
	// ErrInvalidName is returned for a window name tmux could not carry.
	ErrInvalidName = errors.New("tmuxcc: invalid window name")
)

// Options configures one attach.
type Options struct {
	Slug string // tmux session name == Hive session slug
	// Cols and Rows are the opening size vote, and 0x0 means the caller has
	// nothing measured to vote with — see unsized.
	Cols        int
	Rows        int
	Binary      string // tmux executable; empty resolves "tmux" through $PATH
	BufferBytes int    // broker bound; 0 == defaultBufferBytes
	Metrics     MetricsSink
	Logger      zerolog.Logger
	OnExit      func(slug, reason string)

	newProcess func(Options) process
}

// unsized reports an attach that sets no client size at all. tmux ignores a
// control client until it has set one, so an unsized attach leaves the session
// at the size its other clients gave it instead of squeezing it to a
// placeholder; the first Resize is what joins the negotiation.
func (o Options) unsized() bool { return o.Cols == 0 && o.Rows == 0 }

func (o *Options) normalize() error {
	if o.Slug == "" {
		return fmt.Errorf("%w: empty session slug", ErrNotAttached)
	}
	if err := validateAttachSize(o.Cols, o.Rows); err != nil {
		return err
	}
	if o.Binary == "" {
		o.Binary = defaultBinary
	}
	if o.Metrics == nil {
		o.Metrics = NopMetrics
	}
	if o.newProcess == nil {
		o.newProcess = newExecProcess
	}
	return nil
}

// Client is one control-mode connection to one Hive session.
type Client struct {
	slug    string
	log     zerolog.Logger
	metrics MetricsSink
	onExit  func(slug, reason string)

	proc   process
	gw     *Gateway
	ctrl   *controller
	events *broker
	paint  *paintGate

	cancel        context.CancelFunc
	readerDone    chan struct{}
	workerDone    chan struct{}
	reconcileReq  chan struct{}
	handshake     chan struct{}
	attached      chan struct{}
	handshakeOnce sync.Once

	mu         sync.Mutex
	exitReason string
	tornDown   bool

	closeOnce    sync.Once
	teardownOnce sync.Once
}

// Attach spawns `tmux -C attach -t <slug>` over the process seam and runs the
// attach sequence: handshake, client size, window enumeration, then a
// capture-pane first paint per window. Live output produced during that
// sequence is buffered and replayed behind each window's snapshot.
//
// ctx bounds the handshake and the attach commands only; lifetime bounds the
// client's own goroutines, which outlive the request that opened them.
func Attach(ctx, lifetime context.Context, opts Options) (*Client, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}
	if !platformSupported() {
		return nil, ErrUnavailable
	}

	c := &Client{
		slug:         opts.Slug,
		log:          opts.Logger.With().Str("session", opts.Slug).Logger(),
		metrics:      opts.Metrics,
		onExit:       opts.OnExit,
		ctrl:         newController(),
		readerDone:   make(chan struct{}),
		workerDone:   make(chan struct{}),
		reconcileReq: make(chan struct{}, 1),
		handshake:    make(chan struct{}),
		attached:     make(chan struct{}),
	}
	c.events = newBroker(opts.BufferBytes, func() { c.teardown("overflow") })
	c.paint = newPaintGate(c.emitOutput)

	c.proc = opts.newProcess(opts)
	stdin, stdout, err := c.proc.Start(lifetime)
	if err != nil {
		return nil, err
	}
	c.gw = NewGateway(stdin, c.onNotification, c.log)

	lifeCtx, cancel := context.WithCancel(lifetime)
	c.cancel = cancel

	go c.read(stdout)
	go c.worker(lifeCtx)

	attachCtx, cancelAttach := context.WithTimeout(ctx, attachTimeout)
	defer cancelAttach()

	if err := c.awaitHandshake(attachCtx); err != nil {
		c.teardown("attach failed")
		return nil, err
	}
	if err := c.negotiate(attachCtx, opts); err != nil {
		c.teardown("attach failed")
		return nil, err
	}

	close(c.attached)
	c.publish(LifecycleChanged{Kind: LifecycleAttached})
	return c, nil
}

// Windows returns a snapshot of the current window set.
func (c *Client) Windows() []Window { return c.ctrl.Windows() }

// Subscribe returns this client's event channel plus its unsubscribe func.
// There is one active subscriber: a second call closes the first channel.
func (c *Client) Subscribe() (<-chan Event, func()) { return c.events.subscribe() }

// Write sends bytes to a window's active pane. Every byte goes as hex
// (send-keys -H), which sidesteps tmux's key-name and literal parsing
// entirely.
func (c *Client) Write(ctx context.Context, windowID string, p []byte) error {
	pane, err := c.activePane(windowID)
	if err != nil {
		return err
	}
	for len(p) > 0 {
		n := min(sendKeysChunk, len(p))
		var cmd strings.Builder
		cmd.WriteString("send-keys -H -t ")
		cmd.WriteString(pane)
		for _, b := range p[:n] {
			fmt.Fprintf(&cmd, " %02x", b)
		}
		if _, err := c.gw.Send(ctx, cmd.String()); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

// Resize renegotiates the client size. Every client attached to a window
// renders the same grid and tmux's window-size option picks whose size that is
// — by default the most recently used client's — so this is a vote, and
// %layout-change is the answer.
func (c *Client) Resize(ctx context.Context, cols, rows int) error {
	if err := validateSize(cols, rows); err != nil {
		return err
	}
	_, err := c.gw.Send(ctx, fmt.Sprintf("refresh-client -C %d,%d", cols, rows))
	return err
}

func (c *Client) SelectWindow(ctx context.Context, windowID string) error {
	if err := c.requireWindow(windowID); err != nil {
		return err
	}
	_, err := c.gw.Send(ctx, "select-window -t "+windowID)
	return err
}

// NewWindow creates a window and returns its id. The tab set itself is driven
// by the %window-add notification that follows.
func (c *Client) NewWindow(ctx context.Context) (string, error) {
	lines, err := c.gw.Send(ctx, `new-window -P -F "#{window_id}"`)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 || !validWindowID(lines[0]) {
		return "", fmt.Errorf("tmuxcc: new-window returned no window id")
	}
	return lines[0], nil
}

func (c *Client) CloseWindow(ctx context.Context, windowID string) error {
	if err := c.requireWindow(windowID); err != nil {
		return err
	}
	_, err := c.gw.Send(ctx, "kill-window -t "+windowID)
	return err
}

func (c *Client) RenameWindow(ctx context.Context, windowID, name string) error {
	if err := c.requireWindow(windowID); err != nil {
		return err
	}
	quoted, err := quoteArgument(name)
	if err != nil {
		return err
	}
	_, err = c.gw.Send(ctx, "rename-window -t "+windowID+" "+quoted)
	return err
}

// Close detaches and waits for the process to exit. Idempotent.
func (c *Client) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		// Recorded before the detach round trip: tmux answers it with %exit and
		// closes the stream, and the reader must not race a different reason in.
		c.noteExit("detached")
		detachCtx, cancel := context.WithTimeout(ctx, detachTimeout)
		_, _ = c.gw.Send(detachCtx, "detach")
		cancel()
		c.teardown(c.exitReasonOr("detached"))
	})
	return nil
}

func (c *Client) awaitHandshake(ctx context.Context) error {
	select {
	case <-c.handshake:
		return nil
	case <-c.readerDone:
		if serverErr := c.gw.ServerError(); serverErr != "" {
			return fmt.Errorf("tmuxcc: control stream ended before attach: %w (tmux: %s)", c.procWaitError(), serverErr)
		}
		return fmt.Errorf("tmuxcc: control stream ended before attach: %w", c.procWaitError())
	case <-ctx.Done():
		return fmt.Errorf("tmuxcc: attach handshake: %w", ctx.Err())
	}
}

// negotiate votes the opening size before it enumerates or captures anything,
// so the snapshot each window is first painted from is already the size tmux
// settled on rather than one it is about to reflow away from.
func (c *Client) negotiate(ctx context.Context, opts Options) error {
	if !opts.unsized() {
		if _, err := c.gw.Send(ctx, fmt.Sprintf("refresh-client -C %d,%d", opts.Cols, opts.Rows)); err != nil {
			return err
		}
	}

	windows, err := c.listWindows(ctx)
	if err != nil {
		return err
	}
	c.ctrl.set(windows)

	for _, w := range windows {
		if w.ActivePane == "" {
			continue
		}
		if err := c.firstPaint(ctx, w.ActivePane, w.Height); err != nil {
			return err
		}
	}
	c.paint.openAll()
	return nil
}

// firstPaint snapshots a pane and replays the live output that arrived while
// the snapshot was in flight. Every path releases the pane: one left held
// buffers its output forever.
func (c *Client) firstPaint(ctx context.Context, pane string, rows int) error {
	c.paint.mark(pane)
	painted, err := c.snapshot(ctx, pane, rows)
	if err != nil {
		c.paint.release(pane, nil)
		return err
	}
	c.paint.release(pane, painted)
	return nil
}

// snapshot renders a pane as the byte stream an emulator replays into an empty
// grid: bounded scrollback, then the visible screen at exactly the grid's
// height, then the cursor.
//
// The height is what makes this safe for a pane whose program is on the
// alternate screen. An emulator pins its viewport to the last rows it was
// written, so a screen written short would seat the pane's top row somewhere
// down the viewport, and every cursor-addressed redraw the program made after
// that would land rows away from where it aimed.
//
// The cursor is read first, and after the mark rather than before it: output
// tmux produces between the two is absent from the cursor but present in the
// replay that follows the snapshot, which redraws it and carries the cursor
// where it belongs. Read before the mark, that output would be discarded as
// already snapshotted and nothing would ever correct the position.
func (c *Client) snapshot(ctx context.Context, pane string, rows int) ([]byte, error) {
	cursor, err := c.snapshotCmd(ctx, pane, `display-message -p -t `+pane+` "`+cursorFormat+`"`)
	if err != nil {
		return nil, err
	}
	// -J on the history and not on the screen: a scrollback row is worth
	// rejoining to the logical line it was wrapped from, so searching and
	// selecting it read as one line, but a joined screen row would re-wrap into
	// more rows than it was captured from and break the height above.
	history, err := c.snapshotCmd(ctx, pane, fmt.Sprintf("capture-pane -pe -J -S -%d -E -1 -t %s", historyLines, pane))
	if err != nil {
		return nil, err
	}
	screen, err := c.snapshotCmd(ctx, pane, "capture-pane -pe -S 0 -t "+pane)
	if err != nil {
		return nil, err
	}
	return snapshotBytes(history, screen, rows, parseCursor(cursor)), nil
}

// snapshotCmd runs one snapshot command. A tmux-side failure — a pane that
// closed mid-attach is the usual one — yields no lines, so the rest of the
// snapshot still paints; only a broken control stream is an error.
func (c *Client) snapshotCmd(ctx context.Context, pane, cmd string) ([]string, error) {
	lines, err := c.gw.Send(ctx, cmd)
	if err == nil {
		return lines, nil
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		return nil, err
	}
	c.log.Warn().Err(err).Str("pane", pane).Str("command", cmd).Msg("snapshot command skipped")
	return nil, nil
}

func (c *Client) listWindows(ctx context.Context) ([]Window, error) {
	lines, err := c.gw.Send(ctx, `list-windows -F "`+listWindowsFormat+`"`)
	if err != nil {
		return nil, err
	}
	windows := make([]Window, 0, len(lines))
	for _, line := range lines {
		w, ok := parseWindowLine(line)
		if !ok {
			c.log.Warn().Str("line", line).Msg("unparseable list-windows row")
			continue
		}
		windows = append(windows, w)
	}
	return windows, nil
}

// worker runs the commands the reader may not: the reader goroutine must never
// call the blocking Send, because the reply it would wait for arrives on that
// same goroutine.
func (c *Client) worker(ctx context.Context) {
	defer close(c.workerDone)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.reconcileReq:
			select {
			case <-c.attached:
			case <-ctx.Done():
				return
			}
			c.runReconcile(ctx)
		}
	}
}

// runReconcile refreshes the window set and first-paints whatever it just
// discovered. A window created after attach reaches us as %window-add, which
// carries no pane: its output is unroutable until this runs, so the snapshot is
// the only thing that puts the new tab's prompt on screen.
func (c *Client) runReconcile(ctx context.Context) {
	windows, err := c.listWindows(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn().Err(err).Msg("window reconcile failed")
		}
		return
	}

	var unpainted []Window
	for _, w := range windows {
		if w.ActivePane != "" && c.paint.hold(w.ActivePane) {
			unpainted = append(unpainted, w)
		}
	}
	for _, ev := range c.ctrl.reconcile(windows) {
		c.publish(ev)
	}
	for i, w := range unpainted {
		if err := c.firstPaint(ctx, w.ActivePane, w.Height); err != nil {
			if ctx.Err() == nil {
				c.log.Warn().Err(err).Str("pane", w.ActivePane).Msg("first paint failed")
			}
			// hold took the gate for the whole batch; a pane this loop never
			// reaches would buffer its output for the life of the client.
			for _, unreached := range unpainted[i+1:] {
				c.paint.discard(unreached.ActivePane)
			}
			return
		}
	}
}

// requestReconcile coalesces: a pending request absorbs later triggers.
func (c *Client) requestReconcile() {
	select {
	case c.reconcileReq <- struct{}{}:
	default:
	}
}

func (c *Client) read(stdout io.Reader) {
	defer close(c.readerDone)
	scanner := newLineScanner(stdout, maxLineBytes)
	for {
		line, err := scanner.next()
		if err != nil {
			c.gw.fail(errGatewayClosed)
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				c.log.Debug().Err(err).Msg("control stream ended")
			}
			go c.teardown(c.exitReasonOr("exited"))
			return
		}
		if err := c.gw.Feed(line); err != nil {
			c.publish(LifecycleChanged{Kind: LifecycleError, Message: err.Error()})
			go c.teardown("protocol error")
			return
		}
	}
}

func (c *Client) onNotification(n Notification) {
	switch v := n.(type) {
	case OutputNotification:
		c.paint.route(v.Pane, v.Data, time.Now())
		return
	case SessionChanged:
		c.handshakeOnce.Do(func() { close(c.handshake) })
	case WindowCloseNotification:
		// Ahead of the controller forgetting the window, which is what still
		// resolves the pane here.
		if w, ok := c.ctrl.byID(v.Window); ok && w.ActivePane != "" {
			c.paint.discard(w.ActivePane)
		}
	case ExitNotification:
		c.noteExit(v.Reason)
		// tmux closes the stream right after %exit; the kill is insurance
		// against a child that does not.
		go c.terminate()
	case WindowAddNotification, LayoutChanged:
		c.requestReconcile()
	}
	for _, ev := range c.ctrl.apply(n) {
		c.publish(ev)
	}
}

// emitOutput resolves a pane to its window and forwards the bytes. Output from
// a non-active pane is counted and dropped rather than buffered: it is never
// rendered, so letting it consume the broker's byte budget would let a
// background pane tear the session down.
func (c *Client) emitOutput(pane string, data []byte, at time.Time) {
	w, ok := c.ctrl.windowForPane(pane)
	if !ok {
		c.log.Debug().Str("pane", pane).Msg("output for unknown pane dropped")
		return
	}
	c.metrics.BytesStreamed(c.slug, w.ID, len(data))
	if w.ActivePane != pane {
		return
	}
	c.events.publish(Output{At: at, WindowID: w.ID, PaneID: pane, Data: data})
	c.metrics.StreamBufferDepth(c.slug, w.ID, c.events.depth())
}

func (c *Client) publish(ev Event) {
	if lc, ok := ev.(LifecycleChanged); ok {
		switch lc.Kind {
		case LifecyclePaused:
			c.metrics.PauseEvent(c.slug, lc.WindowID)
		case LifecycleResumed:
			c.metrics.ResumeEvent(c.slug, lc.WindowID)
		default:
		}
	}
	c.events.publish(ev)
}

func (c *Client) terminate() { _ = c.proc.Kill() }

// teardown kills the child, joins the goroutines, and closes the event stream.
// Reason is the first one recorded: overflow and protocol errors beat the EOF
// that follows them.
func (c *Client) teardown(reason string) {
	c.teardownOnce.Do(func() {
		// Set before anything else: the manager reads it to decide whether the
		// client it is about to register is already dead.
		c.markTornDown(reason)
		c.cancel()
		_ = c.proc.Kill()
		<-c.readerDone
		<-c.workerDone
		_ = c.proc.Wait()
		c.gw.fail(errGatewayClosed)
		c.events.publish(LifecycleChanged{Kind: LifecycleExited, Message: reason})
		c.events.close()
		if c.onExit != nil {
			c.onExit(c.slug, reason)
		}
	})
}

func (c *Client) procWaitError() error {
	if err := c.proc.Wait(); err != nil {
		return err
	}
	return io.EOF
}

func (c *Client) noteExit(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exitReason == "" {
		c.exitReason = reason
	}
}

func (c *Client) markTornDown(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tornDown = true
	if c.exitReason == "" {
		c.exitReason = reason
	}
}

// exited reports the exit reason once teardown has begun. It is how the manager
// avoids registering — or handing back — a client that is already gone.
func (c *Client) exited() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitReason, c.tornDown
}

func (c *Client) exitReasonOr(fallback string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exitReason == "" {
		return fallback
	}
	return c.exitReason
}

func (c *Client) activePane(windowID string) (string, error) {
	w, err := c.window(windowID)
	if err != nil {
		return "", err
	}
	if w.ActivePane == "" {
		return "", fmt.Errorf("%w: %s has no pane", ErrUnknownWindow, windowID)
	}
	return w.ActivePane, nil
}

// window is the gate every command argument passes: a window id reaches a
// tmux command line only if it is @<digits> and one this client tracks.
func (c *Client) window(windowID string) (Window, error) {
	if !validWindowID(windowID) {
		return Window{}, fmt.Errorf("%w: %q", ErrUnknownWindow, windowID)
	}
	w, ok := c.ctrl.byID(windowID)
	if !ok {
		return Window{}, fmt.Errorf("%w: %s", ErrUnknownWindow, windowID)
	}
	return w, nil
}

func (c *Client) requireWindow(windowID string) error {
	_, err := c.window(windowID)
	return err
}

func validateSize(cols, rows int) error {
	if cols < minDimension || cols > maxDimension || rows < minDimension || rows > maxDimension {
		return fmt.Errorf("%w: %dx%d", ErrInvalidSize, cols, rows)
	}
	return nil
}

// validateAttachSize also accepts 0x0, the unsized attach. Half of one is still
// invalid: a caller with a measurement has both numbers.
func validateAttachSize(cols, rows int) error {
	if cols == 0 && rows == 0 {
		return nil
	}
	return validateSize(cols, rows)
}

func platformSupported() bool {
	return buildSupportsTerminal && (runtime.GOOS == "darwin" || runtime.GOOS == "linux")
}

func parseWindowLine(line string) (Window, bool) {
	fields := strings.SplitN(line, " ", 6)
	if len(fields) < 5 || !validWindowID(fields[0]) {
		return Window{}, false
	}
	w := Window{
		ID:         fields[0],
		Active:     fields[1] == "1",
		ActivePane: fields[2],
		Width:      atoi([]byte(fields[3])),
		Height:     atoi([]byte(fields[4])),
	}
	if len(fields) == 6 {
		w.Name = fields[5]
	}
	return w, true
}

// snapshotBytes joins a captured pane into one replay: history rows, then
// exactly rows screen rows, then the cursor. Nothing homes or clears first —
// writing history-plus-a-full-screen scrolls the history out of the viewport on
// its own, which leaves the screen occupying the viewport exactly and the
// history reachable above it as scrollback.
func snapshotBytes(history, screen []string, rows int, cur cursor) []byte {
	screen = fitRows(screen, rows)
	if len(history)+len(screen) == 0 {
		return nil
	}
	lines := make([]string, 0, len(history)+len(screen))
	lines = append(lines, history...)
	lines = append(lines, screen...)

	out := []byte(strings.Join(lines, "\r\n"))
	if cur.reported {
		out = fmt.Appendf(out, "\x1b[%d;%dH", cur.row+1, cur.col+1)
	}
	return out
}

// fitRows holds a captured screen to the grid it is replayed into. tmux answers
// a visible-screen capture with one line per row, so this is normally a no-op;
// a short reply pads at the bottom and a long one keeps the bottom, because the
// bottom of a screen is the live end of it either way.
func fitRows(screen []string, rows int) []string {
	switch {
	case rows <= 0 || len(screen) == rows:
		return screen
	case len(screen) > rows:
		return screen[len(screen)-rows:]
	default:
		padded := make([]string, rows)
		copy(padded, screen)
		return padded
	}
}

// cursor is a pane's cursor as tmux reports it: 0-based row and column within
// the visible screen. An unreported cursor leaves the replay's own end position
// standing rather than guessing at one.
type cursor struct {
	row, col int
	reported bool
}

func parseCursor(lines []string) cursor {
	if len(lines) == 0 {
		return cursor{}
	}
	row, col, found := strings.Cut(strings.TrimSpace(lines[0]), " ")
	if !found || !isDigits([]byte(row)) || !isDigits([]byte(col)) {
		return cursor{}
	}
	return cursor{row: atoi([]byte(row)), col: atoi([]byte(col)), reported: true}
}

// quoteArgument single-quotes a tmux command argument. A newline would break
// the line-oriented protocol itself, so it is refused rather than escaped.
func quoteArgument(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidName)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: control character", ErrInvalidName)
		}
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}
