package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hay-kot/hive-desktop/cmd/internal/devproxy"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/rs/zerolog"
)

const launchMarkerEnv = "HIVE_DESKTOP_LAUNCH_ENV"

var launchKeys = []string{
	settings.EnvDataDir,
	settings.EnvConfigDir,
	settings.EnvGitHubAPIBase,
	"WAILS_VITE_HOST",
	"WAILS_VITE_PORT",
	"WAILS_SERVER_HOST",
	"WAILS_SERVER_PORT",
	launchMarkerEnv,
}

type devtools struct {
	worktree    string
	instanceDir string
	markerPath  string
	launchPath  string
	lockPath    string
	stdout      io.Writer
	stderr      io.Writer
	pid         int
	alive       func(int) bool
	lookPath    func(string) (string, error)
	runCommand  func(string, ...string) error
	logger      zerolog.Logger
}

func newDevtools(worktree string, logger zerolog.Logger) *devtools {
	worktree = filepath.Clean(worktree)
	d := &devtools{
		worktree:    worktree,
		instanceDir: filepath.Join(worktree, ".hive-desktop"),
		launchPath:  filepath.Join(worktree, "launch.env"),
		lockPath:    filepath.Join(worktree, ".hive-desktop.lock"),
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		pid:         os.Getpid(),
		alive:       processAlive,
		lookPath:    exec.LookPath,
		logger:      logger,
	}
	d.markerPath = filepath.Join(d.instanceDir, ".instance")
	d.runCommand = func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdout = d.stdout
		cmd.Stderr = d.stderr
		return cmd.Run()
	}
	return d
}

func findWorktree() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("development tooling must run inside a git worktree: %w", err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errors.New("git returned an empty worktree path")
	}
	return filepath.Abs(root)
}

func (d *devtools) validatePaths() error {
	root, err := filepath.Abs(d.worktree)
	if err != nil {
		return err
	}
	if filepath.Clean(d.worktree) != root {
		return fmt.Errorf("worktree path must be absolute: %s", d.worktree)
	}
	wantInstance := filepath.Join(root, ".hive-desktop")
	wantLaunch := filepath.Join(root, "launch.env")
	wantLock := filepath.Join(root, ".hive-desktop.lock")
	if d.instanceDir != wantInstance || d.launchPath != wantLaunch || d.lockPath != wantLock {
		return errors.New("development paths escaped the worktree")
	}
	for _, path := range []string{d.instanceDir, d.launchPath, d.lockPath} {
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink at %s", path)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (d *devtools) prepare(fresh bool) error {
	if err := d.validatePaths(); err != nil {
		return err
	}
	existing, err := d.readLaunchIfPresent()
	if err != nil {
		return err
	}
	if fresh {
		if err := d.ensureLaunchInactive(existing); err != nil {
			return err
		}
		if err := d.removeInstance(); err != nil {
			return err
		}
		if err := removeRegularFile(d.launchPath); err != nil {
			return err
		}
	}

	if !fresh && existing != nil {
		if err := d.validateInstance(); err == nil {
			d.logger.Info().Str("instance", d.instanceDir).Str("launch_env", d.launchPath).Msg("reusing desktop development environment")
			return nil
		}
	}

	created, err := d.ensureInstance()
	if err != nil {
		return err
	}
	sourcePaths, err := installedPaths()
	if err != nil {
		return err
	}

	dataDir := filepath.Join(d.instanceDir, "data")
	configDir := filepath.Join(d.instanceDir, "config")
	if created {
		if err := d.seedData(sourcePaths.DataDir, dataDir); err != nil {
			return err
		}
		if err := d.seedConfig(sourcePaths.ConfigDir, configDir); err != nil {
			return err
		}
	}

	cfg, err := settings.NewStore(filepath.Join(configDir, "settings.yaml")).Persisted()
	if err != nil {
		return fmt.Errorf("load development settings: %w", err)
	}

	vitePort, err := resolvePort(cfg.Development.Vite, 0)
	if err != nil {
		return fmt.Errorf("resolve Vite port: %w", err)
	}
	wailsPort, err := resolvePort(cfg.Development.Wails, vitePort)
	if err != nil {
		return fmt.Errorf("resolve Wails port: %w", err)
	}
	// Development runs through the shared proxy by default (ADR 0017): the
	// address comes from the checked-in devserver config, so changing the port
	// there reaches every worktree without editing this. Opting out is setting
	// the same variable empty in the gitignored overrides.env, which mise loads
	// after launch.env.
	proxyListen := devproxy.ListenFromConfig(d.worktree)

	env := map[string]string{
		settings.EnvDataDir:       dataDir,
		settings.EnvConfigDir:     configDir,
		settings.EnvGitHubAPIBase: devproxy.BaseURL(proxyListen),
		"WAILS_VITE_HOST":         cfg.Development.Vite.Host,
		"WAILS_VITE_PORT":         strconv.Itoa(vitePort),
		"WAILS_SERVER_HOST":       cfg.Development.Wails.Host,
		"WAILS_SERVER_PORT":       strconv.Itoa(wailsPort),
		launchMarkerEnv:           d.launchPath,
	}
	if err := writeDotenvAtomic(d.launchPath, env); err != nil {
		return err
	}
	verb := "reusing"
	if created || fresh {
		verb = "prepared"
	}
	d.logger.Info().
		Str("state", verb).
		Str("instance", d.instanceDir).
		Str("launch_env", d.launchPath).
		Str("vite", net.JoinHostPort(cfg.Development.Vite.Host, strconv.Itoa(vitePort))).
		Str("wails", net.JoinHostPort(cfg.Development.Wails.Host, strconv.Itoa(wailsPort))).
		Msg("desktop development environment ready")
	return nil
}

func (d *devtools) reset() error {
	if err := d.validatePaths(); err != nil {
		return err
	}
	existing, err := d.readLaunchIfPresent()
	if err != nil {
		return err
	}
	if err := d.ensureLaunchInactive(existing); err != nil {
		return err
	}
	if err := d.removeInstance(); err != nil {
		return err
	}
	if err := removeRegularFile(d.launchPath); err != nil {
		return err
	}
	d.logger.Info().Str("instance", d.instanceDir).Str("launch_env", d.launchPath).Msg("reset desktop development environment")
	return nil
}

// ensureLaunchInactive keeps fresh/reset from deleting files beneath a Wails
// process launched with launch.env. A different process occupying either port
// is treated conservatively as active; regenerate only after it releases the
// configured address.
func (d *devtools) ensureLaunchInactive(launch map[string]string) error {
	if launch == nil {
		return nil
	}
	for _, server := range []struct {
		name, hostKey, portKey string
	}{
		{name: "Vite", hostKey: "WAILS_VITE_HOST", portKey: "WAILS_VITE_PORT"},
		{name: "Wails", hostKey: "WAILS_SERVER_HOST", portKey: "WAILS_SERVER_PORT"},
	} {
		address := net.JoinHostPort(launch[server.hostKey], launch[server.portKey])
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err != nil {
			continue
		}
		_ = connection.Close()
		return fmt.Errorf("%s development server is active at %s; stop it before fresh/reset", server.name, address)
	}
	return nil
}

func (d *devtools) ensureInstance() (bool, error) {
	if _, err := os.Lstat(d.instanceDir); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(d.instanceDir, 0o755); err != nil {
			return false, fmt.Errorf("create instance root: %w", err)
		}
		if err := os.WriteFile(d.markerPath, []byte(d.worktree+"\n"), 0o600); err != nil {
			_ = os.RemoveAll(d.instanceDir)
			return false, fmt.Errorf("write instance marker: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(d.instanceDir, "data", "desktop"), 0o755); err != nil {
			return false, err
		}
		if err := os.MkdirAll(filepath.Join(d.instanceDir, "config"), 0o755); err != nil {
			return false, err
		}
		return true, nil
	} else if err != nil {
		return false, err
	}
	if err := d.validateInstance(); err != nil {
		return false, err
	}
	return false, nil
}

func (d *devtools) validateInstance() error {
	if err := d.validatePaths(); err != nil {
		return err
	}
	info, err := os.Lstat(d.instanceDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", d.instanceDir)
	}
	marker, err := os.Lstat(d.markerPath)
	if err != nil {
		return fmt.Errorf("instance marker: %w", err)
	}
	if !marker.Mode().IsRegular() {
		return errors.New("instance marker is not a regular file")
	}
	owner, err := os.ReadFile(d.markerPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(owner)) != d.worktree {
		return fmt.Errorf("instance belongs to %q, not %q", strings.TrimSpace(string(owner)), d.worktree)
	}
	return nil
}

func (d *devtools) removeInstance() error {
	_, err := os.Lstat(d.instanceDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := d.validateInstance(); err != nil {
		return err
	}
	return os.RemoveAll(d.instanceDir)
}

func removeRegularFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to remove non-regular file %s", path)
	}
	return os.Remove(path)
}

// installedPaths ignores launch-time path overrides. Development always
// clones the installed app selected by bootstrap/XDG into this worktree's
// fixed .hive-desktop roots; overrides.env applies only when Wails launches.
func installedPaths() (settings.Paths, error) {
	dataDir, hadDataDir := os.LookupEnv(settings.EnvDataDir)
	configDir, hadConfigDir := os.LookupEnv(settings.EnvConfigDir)
	_ = os.Unsetenv(settings.EnvDataDir)
	_ = os.Unsetenv(settings.EnvConfigDir)
	defer func() {
		if hadDataDir {
			_ = os.Setenv(settings.EnvDataDir, dataDir)
		}
		if hadConfigDir {
			_ = os.Setenv(settings.EnvConfigDir, configDir)
		}
	}()

	bootstrap, err := settings.LoadBootstrap()
	if err != nil {
		return settings.Paths{}, fmt.Errorf("load installed bootstrap: %w", err)
	}
	return settings.ResolvePaths(bootstrap, ""), nil
}

func resolvePort(server settings.ServerSettings, excluded int) (int, error) {
	address := net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return 0, fmt.Errorf("preflight %s: %w", address, err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return 0, fmt.Errorf("preflight %s: listener address is %T, want *net.TCPAddr", address, listener.Addr())
	}
	port := addr.Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("close preflight listener: %w", err)
	}
	// Framework-owned listeners cannot accept this open socket, so a small
	// time-of-check/time-of-use race remains between preparation and startup.
	if server.Port == 0 && port == excluded {
		return resolvePort(server, excluded)
	}
	if server.Port != 0 && port == excluded {
		return 0, fmt.Errorf("configured port %d is assigned to both development servers", port)
	}
	return port, nil
}

func (d *devtools) seedData(source, destination string) error {
	if err := os.MkdirAll(filepath.Join(destination, "desktop"), 0o755); err != nil {
		return err
	}
	if err := d.copyDatabase(filepath.Join(source, "hive.db"), filepath.Join(destination, "hive.db")); err != nil {
		return err
	}
	if err := d.copyDatabase(filepath.Join(source, "desktop", "desktop-pipeline.db"), filepath.Join(destination, "desktop", "desktop-pipeline.db")); err != nil {
		return err
	}
	files, _ := filepath.Glob(filepath.Join(source, "desktop", "*.json"))
	for _, sourceFile := range files {
		if err := copyFile(sourceFile, filepath.Join(destination, "desktop", filepath.Base(sourceFile))); err != nil {
			return err
		}
	}
	return nil
}

func (d *devtools) copyDatabase(source, destination string) error {
	if _, err := os.Stat(source); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if sqlite, err := d.lookPath("sqlite3"); err == nil {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		_ = os.Remove(destination)
		escaped := strings.ReplaceAll(destination, "'", "''")
		if err := d.runCommand(sqlite, "-cmd", ".timeout 5000", source, "VACUUM INTO '"+escaped+"'"); err == nil {
			return nil
		} else {
			d.logger.Warn().Err(err).Str("source", source).Msg("SQLite snapshot failed; falling back to file copy")
		}
		_ = os.Remove(destination)
	}
	if err := copyFile(source, destination); err != nil {
		return err
	}
	for _, extension := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(source + extension); err == nil {
			if err := copyFile(source+extension, destination+extension); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *devtools) seedConfig(source, destination string) error {
	if _, err := os.Stat(source); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := copyTreeMaterialized(source, destination, make(map[string]bool)); err != nil {
		return fmt.Errorf("copy installed config: %w", err)
	}
	_ = os.Remove(filepath.Join(destination, "bootstrap.yaml"))
	// The settings schema intentionally broke; do not seed an old flat file.
	_ = os.Remove(filepath.Join(destination, "settings.yaml"))
	return nil
}

func copyTreeMaterialized(source, destination string, stack map[string]bool) error {
	realSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	realSource, err = filepath.Abs(realSource)
	if err != nil {
		return err
	}
	if stack[realSource] {
		return fmt.Errorf("symlink cycle at %s", source)
	}
	info, err := os.Stat(realSource)
	if err != nil {
		return err
	}
	if info.IsDir() {
		stack[realSource] = true
		defer delete(stack, realSource)
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(realSource)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTreeMaterialized(filepath.Join(realSource, entry.Name()), filepath.Join(destination, entry.Name()), stack); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported config file type at %s", source)
	}
	return copyFile(realSource, destination)
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (d *devtools) readLaunchIfPresent() (map[string]string, error) {
	data, err := os.ReadFile(d.launchPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	env, err := parseDotenv(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", d.launchPath, err)
	}
	if env[launchMarkerEnv] != d.launchPath {
		return nil, fmt.Errorf("%s is not owned by this worktree", d.launchPath)
	}
	for _, key := range launchKeys {
		if _, ok := env[key]; !ok {
			return nil, fmt.Errorf("%s is missing %s", d.launchPath, key)
		}
	}
	return env, nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
