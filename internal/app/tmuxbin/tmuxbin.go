// Package tmuxbin locates the tmux executable.
//
// A desktop launch does not inherit the user's shell PATH — macOS starts an
// .app bundle with /usr/bin:/bin:/usr/sbin:/sbin — so exec.LookPath cannot see
// a Homebrew, MacPorts or Nix tmux that every terminal on the machine finds.
// Both things in this app that exec tmux (terminal mode's control client and
// Hive session spawning) resolve it through here instead.
package tmuxbin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ErrNotFound reports that neither PATH nor any searched prefix held tmux.
var ErrNotFound = errors.New("tmuxbin: tmux not found")

// The prefixes package managers install into, standing in for the PATH a
// desktop launch does not get. Order is precedence: a Homebrew tmux is the one
// the user's shell would have picked over /usr/bin.
var systemDirs = []string{
	"/opt/homebrew/bin",                 // Homebrew, Apple silicon
	"/usr/local/bin",                    // Homebrew on Intel, manual installs
	"/opt/local/bin",                    // MacPorts
	"/home/linuxbrew/.linuxbrew/bin",    // Homebrew on Linux
	"/run/current-system/sw/bin",        // NixOS
	"/nix/var/nix/profiles/default/bin", // Nix, multi-user
	"/usr/bin",
	"/bin",
}

// Locate returns the tmux binary to exec. override is terminal.tmux_path: when
// set it is used or it fails, never fallen back from — a configured path that
// does not work is a mistake to report, not a reason to silently run a
// different tmux.
func Locate(override string) (string, error) { return locate(override, searchDirs()) }

func locate(override string, dirs []string) (string, error) {
	if override != "" {
		if err := usable(override); err != nil {
			return "", fmt.Errorf("terminal.tmux_path: %w", err)
		}
		return override, nil
	}
	if path, err := exec.LookPath("tmux"); err == nil {
		return path, nil
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, "tmux")
		if usable(candidate) == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w on PATH or in %s; set terminal.tmux_path in settings.yaml to point at it", ErrNotFound, strings.Join(dirs, ", "))
}

func searchDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return systemDirs
	}
	return slices.Concat(systemDirs, []string{
		filepath.Join(home, ".nix-profile", "bin"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
	})
}

// usable follows symlinks, because a Homebrew or Nix bin entry is a link into a
// versioned prefix.
func usable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

// Resolver locates tmux on demand and remembers the answer. A failure is not
// remembered, so installing tmux takes effect without relaunching the app —
// the same policy the terminal's version probe follows.
type Resolver struct {
	override string

	mu   sync.Mutex
	path string
}

func NewResolver(override string) *Resolver { return &Resolver{override: override} }

func (r *Resolver) Path() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path != "" {
		return r.path, nil
	}
	path, err := Locate(r.override)
	if err != nil {
		return "", err
	}
	r.path = path
	return path, nil
}
