package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// watchDebounce coalesces editor save bursts (write + rename + chmod) into one
// reload.
const watchDebounce = 250 * time.Millisecond

// Watcher invokes onChange when settings.yaml changes on disk, so edits made
// outside the app apply live. Like the flows and actions watchers it watches
// the parent directory rather than the file: editors and atomic writers replace
// the file by rename, which silently drops a watch registered on the file
// itself — and a directory watch works before the file first exists.
//
// The basename filter has to be exact here: Store's own atomic write creates
// `.settings-*.yaml` siblings in the same directory, and a prefix match would
// reload on every temporary file it makes.
type Watcher struct {
	path     string
	onChange func()
	logger   zerolog.Logger
	watcher  *fsnotify.Watcher

	stopOnce sync.Once
	stop     chan struct{}
}

func NewWatcher(path string, onChange func(), logger zerolog.Logger) (*Watcher, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("settings: create config dir: %w", err)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("settings: start settings watcher: %w", err)
	}
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("settings: watch %s: %w", dir, err)
	}
	return &Watcher{
		path:     path,
		onChange: onChange,
		logger:   logger,
		watcher:  watcher,
		stop:     make(chan struct{}),
	}, nil
}

// Start runs the watch loop in a goroutine until Close.
func (w *Watcher) Start() {
	go w.run()
}

func (w *Watcher) Close() {
	w.stopOnce.Do(func() {
		close(w.stop)
		if err := w.watcher.Close(); err != nil {
			w.logger.Debug().Err(err).Msg("settings watcher close failed")
		}
	})
}

func (w *Watcher) run() {
	var debounce <-chan time.Time
	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != filepath.Base(w.path) {
				continue
			}
			if !event.Op.Has(fsnotify.Create) && !event.Op.Has(fsnotify.Write) &&
				!event.Op.Has(fsnotify.Rename) && !event.Op.Has(fsnotify.Remove) {
				continue
			}
			debounce = time.After(watchDebounce)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn().Err(err).Msg("settings watch error")
		case <-debounce:
			debounce = nil
			w.onChange()
		}
	}
}
