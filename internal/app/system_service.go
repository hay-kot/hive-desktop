package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/colonyops/hive/pkg/osopen"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// SystemService owns the app's on-disk locations and the operations the
// System settings screen offers over them: open, reveal, and the
// point-only data/config directory overrides.
//
// Directory overrides take effect after a restart: they are written to the
// bootstrap pointer file and seeded into the environment at next launch.
// Nothing is moved.
//
// The native directory picker and Quit stay in the adapter — both are GUI,
// not domain.
type SystemService struct{ paths settings.Paths }

func newSystemService(paths ...settings.Paths) *SystemService {
	if len(paths) > 0 {
		return &SystemService{paths: paths[0]}
	}
	b, _ := settings.LoadBootstrap()
	return &SystemService{paths: settings.ResolvePaths(b, settings.ResolveOptions{MockMode: settings.MockMode()})}
}

// PathInfo describes a single on-disk location.
type PathInfo struct {
	Path string
	// Exists reports whether the path is present right now (a log file or
	// database may not exist until first written).
	Exists bool
	// Overridden reports whether a stored override backs this location. Only
	// meaningful for the data and config directories.
	Overridden bool
}

// SystemInfo is the full set of locations shown on the System settings screen.
type SystemInfo struct {
	DataDir   PathInfo
	ConfigDir PathInfo
	LogFile   PathInfo
	Database  PathInfo
}

// Info returns the effective locations for this process plus whether the
// data and config directories are backed by a stored override.
func (s *SystemService) Info(context.Context) SystemInfo {
	return SystemInfo{
		DataDir:   pathInfo(s.paths.DataDir, s.paths.DataDirOverridden),
		ConfigDir: pathInfo(s.paths.ConfigDir, s.paths.ConfigDirOverridden),
		LogFile:   pathInfo(s.paths.LogFile, false),
		Database:  pathInfo(store.DatabasePath(s.paths.StateDir), false),
	}
}

func pathInfo(path string, overridden bool) PathInfo {
	_, err := os.Stat(path)
	return PathInfo{Path: path, Exists: err == nil, Overridden: overridden}
}

// OpenPath opens one of the known system locations in the OS default
// application.
func (s *SystemService) OpenPath(_ context.Context, path string) error {
	if err := s.checkAllowed(path); err != nil {
		return err
	}
	return Wrap(osopen.Open(path), KindInternal, "opening %s", path)
}

// RevealPath reveals one of the known system locations in the OS file
// manager.
func (s *SystemService) RevealPath(_ context.Context, path string) error {
	if err := s.checkAllowed(path); err != nil {
		return err
	}
	return Wrap(osopen.Reveal(path), KindInternal, "revealing %s", path)
}

// SetDataDir persists a data-directory override. It validates the target and
// creates it if missing, but does not move existing data.
func (s *SystemService) SetDataDir(_ context.Context, path string) error {
	if err := validateDirOverride(path); err != nil {
		return err
	}
	b, err := settings.LoadBootstrap()
	if err != nil {
		return Wrap(err, KindInternal, "reading the bootstrap file")
	}
	b.DataDir = filepath.Clean(path)
	return Wrap(settings.SaveBootstrap(b), KindInternal, "saving the bootstrap file")
}

// SetConfigDir persists a config-directory override (flows, actions). Same
// semantics as SetDataDir.
func (s *SystemService) SetConfigDir(_ context.Context, path string) error {
	if err := validateDirOverride(path); err != nil {
		return err
	}
	b, err := settings.LoadBootstrap()
	if err != nil {
		return Wrap(err, KindInternal, "reading the bootstrap file")
	}
	b.ConfigDir = filepath.Clean(path)
	return Wrap(settings.SaveBootstrap(b), KindInternal, "saving the bootstrap file")
}

// ClearDataDir removes the data-directory override, reverting to the default
// location on the next launch.
func (s *SystemService) ClearDataDir(context.Context) error {
	return clearOverride(func(b *settings.Bootstrap) { b.DataDir = "" })
}

// ClearConfigDir removes the config-directory override.
func (s *SystemService) ClearConfigDir(context.Context) error {
	return clearOverride(func(b *settings.Bootstrap) { b.ConfigDir = "" })
}

func clearOverride(mutate func(*settings.Bootstrap)) error {
	b, err := settings.LoadBootstrap()
	if err != nil {
		return Wrap(err, KindInternal, "reading the bootstrap file")
	}
	mutate(&b)
	return Wrap(settings.SaveBootstrap(b), KindInternal, "saving the bootstrap file")
}

// checkAllowed rejects any path that is not one of the four known system
// locations, cleaned for comparison. This is a security control, not a
// convenience: without it OpenPath is an arbitrary-file-open RPC.
func (s *SystemService) checkAllowed(path string) error {
	allowed := map[string]struct{}{
		filepath.Clean(s.paths.DataDir):                      {},
		filepath.Clean(s.paths.ConfigDir):                    {},
		filepath.Clean(s.paths.LogFile):                      {},
		filepath.Clean(store.DatabasePath(s.paths.StateDir)): {},
	}
	if _, ok := allowed[filepath.Clean(path)]; !ok {
		return Errorf(KindInvalid, "path is not a known system location: %s", path)
	}
	return nil
}

// validateDirOverride ensures path is an absolute, creatable, writable
// directory before it is stored. The write probe is the point: a directory
// that exists but cannot be written to would fail at next launch, with the
// app already pointed at it.
func validateDirOverride(path string) error {
	if strings.TrimSpace(path) == "" {
		return Errorf(KindInvalid, "directory path is required")
	}
	if !filepath.IsAbs(path) {
		return Errorf(KindInvalid, "directory path must be absolute: %s", path)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return Wrap(err, KindInvalid, "create directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Wrap(err, KindInvalid, "stat directory")
	}
	if !info.IsDir() {
		return Errorf(KindInvalid, "not a directory: %s", path)
	}
	probe := filepath.Join(path, ".hive-write-test")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return Wrap(err, KindInvalid, "directory is not writable")
	}
	_ = os.Remove(probe)
	return nil
}
