package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	env "github.com/caarlos0/env/v11"
	"gopkg.in/yaml.v3"

	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
)

// Store serializes access to one settings.yaml and keeps environment overrides
// process-local. All mutations go through Update so concurrent UI/background
// writes cannot lose each other.
//
// It also serves a last-good snapshot. Current never fails: a running app whose
// settings.yaml is momentarily unparsable keeps the values it was already using
// rather than degrading to nothing, the same way a flow that cannot be built
// keeps its predecessor in service (ADR 0041). Reload swaps the snapshot; a
// failed reload leaves the previous one in service and is reported by
// LoadError.
//
// Lock order is mu then settingsFileMu, never the reverse.
type Store struct {
	path string

	mu      sync.Mutex
	loaded  bool
	current Settings
	loadErr error
}

var settingsFileMu sync.Mutex

func NewStore(path string) *Store { return &Store{path: path, current: DefaultSettings()} }
func (s *Store) Path() string     { return s.path }

// Current returns the effective settings this process is serving, by value so
// callers keep their own copy. Before the first successful load it is the
// compiled defaults — the same safe shape a missing file resolves to.
func (s *Store) Current() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	return s.current
}

// LoadError reports why the snapshot Current serves is older than the file on
// disk, or nil when the two agree.
func (s *Store) LoadError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	return s.loadErr
}

// Reload re-reads settings.yaml and swaps the snapshot. On failure it returns
// the snapshot still in service alongside the error.
func (s *Store) Reload() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadLocked()
}

func (s *Store) ensureLoadedLocked() {
	if !s.loaded {
		_, _ = s.reloadLocked()
	}
}

func (s *Store) reloadLocked() (Settings, error) {
	cfg, err := s.load(true)
	s.loaded = true
	if err != nil {
		s.loadErr = err
		return s.current, err
	}
	s.current, s.loadErr = cfg, nil
	return cfg, nil
}

func (s *Store) Persisted() (Settings, error) { return s.load(false) }

func (s *Store) load(withEnvironment bool) (Settings, error) {
	settingsFileMu.Lock()
	defer settingsFileMu.Unlock()
	return loadSettingsAt(s.path, withEnvironment)
}

// Update atomically applies mutate to persisted settings, refreshes the
// snapshot, and returns the effective value after environment overrides are
// reapplied.
//
// A file that will not parse fails the write rather than falling back to the
// snapshot: last-good keeps a running app reading, but writing last-good back
// would overwrite whatever the user has mid-edit on disk.
func (s *Store) Update(mutate func(*Settings) error) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	effective, err := s.updateFile(mutate)
	if err != nil {
		return Settings{}, err
	}
	s.loaded, s.current, s.loadErr = true, effective, nil
	return effective, nil
}

func (s *Store) updateFile(mutate func(*Settings) error) (Settings, error) {
	settingsFileMu.Lock()
	defer settingsFileMu.Unlock()

	cfg, err := loadSettingsAt(s.path, false)
	if err != nil {
		return Settings{}, err
	}
	if err := mutate(&cfg); err != nil {
		return Settings{}, err
	}
	if err := saveSettingsAt(s.path, cfg); err != nil {
		return Settings{}, err
	}
	return loadSettingsAt(s.path, true)
}

func loadSettingsAt(path string, withEnvironment bool) (Settings, error) {
	cfg := DefaultSettings()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Settings{}, fmt.Errorf("read desktop settings: %w", err)
	}
	data, _, err := configmigrate.SettingsSet.Apply(raw)
	if err != nil {
		return Settings{}, fmt.Errorf("migrate desktop settings: %w", err)
	}
	if len(bytes.TrimSpace(data)) > 0 {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return Settings{}, fmt.Errorf("parse desktop settings: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err == nil {
				return Settings{}, fmt.Errorf("parse desktop settings: multiple YAML documents are not allowed")
			}
			return Settings{}, fmt.Errorf("parse desktop settings: %w", err)
		}
	}

	// Persisted settings must be valid on their own. An environment override is
	// process-local and must not hide a broken file that a later launch would
	// still be unable to use.
	if err := cfg.Validate(); err != nil {
		return Settings{}, fmt.Errorf("validate persisted desktop settings: %w", err)
	}

	if withEnvironment {
		cfg.overrides = make(map[string]bool)
		if err := env.ParseWithOptions(&cfg, env.Options{OnSet: func(tag string, _ any, isDefault bool) {
			if value, ok := os.LookupEnv(tag); !isDefault && ok && value != "" {
				cfg.overrides[tag] = true
			}
		}}); err != nil {
			return Settings{}, fmt.Errorf("parse desktop environment: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return Settings{}, fmt.Errorf("validate effective desktop settings: %w", err)
		}
	}
	return cfg, nil
}

func saveSettingsAt(path string, cfg Settings) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate desktop settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create desktop settings dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal desktop settings: %w", err)
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".settings-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary desktop settings: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod temporary desktop settings: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary desktop settings: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temporary desktop settings: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary desktop settings: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace desktop settings: %w", err)
	}
	return nil
}
