// Package execenv resolves the environment the app runs subprocesses with.
//
// A desktop launch inherits the launcher's environment, not a shell's: macOS
// starts an .app bundle with PATH=/usr/bin:/bin:/usr/sbin:/sbin. Session hooks
// and shell actions are the user's own commands, written against the PATH their
// terminal has, so running them with the inherited one fails on anything a
// package manager, a version manager or a language toolchain installed
// (ADR 0041).
package execenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// The prefixes package managers install into, standing in for the PATH a
// desktop launch does not get. Order is precedence: a Homebrew binary is the
// one the user's shell would have picked over /usr/bin.
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

// SearchDirs returns those prefixes plus the per-user ones. It is where a
// binary lives when the environment does not say: tmuxbin searches the list for
// one executable (ADR 0039), and the resolver appends it to PATH.
func SearchDirs() []string {
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

// defaultTimeout bounds the login shell probe. A startup file that sources a
// version manager takes a noticeable fraction of this; one that hangs must not
// take session creation with it.
const defaultTimeout = 5 * time.Second

// probeCommand runs a program instead of echoing $PATH because the syntax to
// echo it is not portable: fish joins a list variable with spaces, so
// `printf %s $PATH` there produces something that is not a PATH at all.
// /usr/bin/env is absolute so a startup file that breaks PATH cannot hide it.
const probeCommand = "/usr/bin/env"

var errNoPath = errors.New("shell reported no PATH")

// Options configures a Resolver. The zero value probes $SHELL with the default
// timeout and logs nothing.
type Options struct {
	Logger zerolog.Logger
	// Shell is the login shell to ask. Empty reads $SHELL; still empty skips
	// the probe.
	Shell string
	// Timeout bounds one probe. Zero means defaultTimeout.
	Timeout time.Duration
	// Probe is the seam tests replace. Zero runs the real login shell.
	Probe func(ctx context.Context, shell string) (string, error)
}

// Resolver answers what PATH a subprocess runs with. It asks the user's login
// shell once per run and remembers the answer — a shell's PATH does not change
// under a running app, and re-running a login shell per hook command would
// charge every command that shell's startup cost.
type Resolver struct {
	logger  zerolog.Logger
	shell   string
	timeout time.Duration
	probe   func(ctx context.Context, shell string) (string, error)

	// mu serializes the probe as well as guarding path, so concurrent hooks
	// spawn one shell between them rather than one each.
	mu       sync.Mutex
	path     string
	resolved bool
}

func NewResolver(opts Options) *Resolver {
	r := &Resolver{logger: opts.Logger, shell: opts.Shell, timeout: opts.Timeout, probe: opts.Probe}
	if r.shell == "" {
		r.shell = os.Getenv("SHELL")
	}
	if r.timeout <= 0 {
		r.timeout = defaultTimeout
	}
	if r.probe == nil {
		r.probe = shellPath
	}
	return r
}

// Path returns the PATH value subprocesses run with: what the login shell
// reports, then what this process inherited, then the package-manager prefixes,
// first occurrence winning. A probe that fails is not fatal — the inherited
// PATH still runs everything in /usr/bin, which is what the app had before.
func (r *Resolver) Path(ctx context.Context) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolved {
		return r.path
	}

	inherited := os.Getenv("PATH")
	fromShell, err := r.resolveShellPath(ctx)
	switch {
	case err != nil:
		r.logger.Warn().Err(err).Str("shell", r.shell).
			Msg("login shell PATH unavailable; hook and shell-action commands run with the inherited PATH")
	case fromShell != "":
		r.logger.Info().Str("shell", r.shell).Msg("resolved subprocess PATH from the login shell")
	}

	r.path = join(fromShell, inherited, strings.Join(SearchDirs(), string(os.PathListSeparator)))
	r.resolved = true
	r.logger.Debug().Str("path", r.path).Msg("subprocess PATH")
	return r.path
}

// LookPath resolves a command name against the same PATH its child will run
// with. It exists because os/exec does not: exec.Command searches the PATH of
// the process that calls it and ignores Cmd.Env, which is the one this package
// is replacing. A name that is already a path is returned untouched.
func (r *Resolver) LookPath(ctx context.Context, name string) (string, error) {
	if strings.ContainsRune(name, filepath.Separator) {
		return name, nil
	}
	for _, dir := range filepath.SplitList(r.Path(ctx)) {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%q: %w", name, exec.ErrNotFound)
}

// Environ returns this process's environment with PATH replaced by Path.
func (r *Resolver) Environ(ctx context.Context) []string {
	path := r.Path(ctx)
	current := os.Environ()
	env := make([]string, 0, len(current)+1)
	for _, kv := range current {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return append(env, "PATH="+path)
}

func (r *Resolver) resolveShellPath(ctx context.Context) (string, error) {
	if r.shell == "" {
		return "", nil
	}
	// The probe outlives the caller's cancellation deliberately: it runs once
	// per app run, and a session creation cancelled mid-probe would otherwise
	// record a failure that had nothing to do with the shell.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
	defer cancel()
	return r.probe(probeCtx, r.shell)
}

// probeKillGrace bounds how long the probe waits on its output pipe after the
// shell is done. The context only bounds the shell's lifetime: a startup file
// that background-spawns anything inheriting stdout (`something &` in .zshrc)
// keeps the pipe open after the shell exits, and Output would block on it —
// with the resolver's lock held, stalling every spawn in the app. WaitDelay
// force-closes the pipe instead; the same mechanism as dispatch's
// shellKillGrace.
const probeKillGrace = 2 * time.Second

// shellPath asks the user's login shell for its PATH. Interactive (-i) as well
// as login (-l), because the PATH a terminal shows is as often set in an
// interactive startup file (.zshrc) as in a login one.
func shellPath(ctx context.Context, shell string) (string, error) {
	cmd := exec.CommandContext(ctx, shell, "-ilc", probeCommand)
	cmd.WaitDelay = probeKillGrace
	out, err := cmd.Output()
	// ErrWaitDelay means the shell exited cleanly but something it left
	// running held the pipe past the grace. Its answer is already in out —
	// a shell that answered is not failed for what it left behind.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return "", err
	}
	// A startup file is free to print, so the environment is scanned for the
	// assignment rather than read positionally.
	for line := range strings.SplitSeq(string(out), "\n") {
		if path, ok := strings.CutPrefix(line, "PATH="); ok {
			if path = strings.TrimSpace(path); path != "" {
				return path, nil
			}
		}
	}
	return "", errNoPath
}

// join concatenates PATH values, dropping empty and repeated entries so the
// result is what a shell would have searched, in that order, once each.
func join(lists ...string) string {
	separator := string(os.PathListSeparator)
	seen := make(map[string]bool)
	dirs := make([]string, 0, len(lists)*8)
	for _, list := range lists {
		for dir := range strings.SplitSeq(list, separator) {
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, separator)
}
