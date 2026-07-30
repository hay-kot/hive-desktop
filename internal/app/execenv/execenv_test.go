package execenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pathEntries(t *testing.T, value string) []string {
	t.Helper()
	return strings.Split(value, string(os.PathListSeparator))
}

func TestPathPrefersTheLoginShell(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")

	r := NewResolver(Options{Shell: "/bin/zsh", Probe: func(context.Context, string) (string, error) {
		return "/opt/tools/bin:/usr/bin", nil
	}})

	entries := pathEntries(t, r.Path(t.Context()))
	require.Equal(t, "/opt/tools/bin", entries[0], "the shell's PATH comes first")
	assert.Contains(t, entries, "/usr/bin")
	assert.Contains(t, entries, "/opt/homebrew/bin", "the package-manager prefixes still backstop it")
	assert.Equal(t, entries, slicesUnique(entries), "a directory both sides name is searched once")
}

// A probe that fails is the common case on a machine with an exotic shell, so
// it must degrade to what the app could already reach rather than to nothing.
func TestPathFallsBackWhenTheProbeFails(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")

	r := NewResolver(Options{Shell: "/bin/zsh", Probe: func(context.Context, string) (string, error) {
		return "", errors.New("exit status 1")
	}})

	entries := pathEntries(t, r.Path(t.Context()))
	assert.Equal(t, []string{"/usr/bin", "/bin"}, entries[:2])
	assert.Contains(t, entries, "/opt/homebrew/bin")
}

func TestPathWithoutAShellIsTheInheritedPathPlusPrefixes(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("SHELL", "")

	probed := false
	r := NewResolver(Options{Probe: func(context.Context, string) (string, error) {
		probed = true
		return "/never", nil
	}})

	entries := pathEntries(t, r.Path(t.Context()))
	assert.False(t, probed, "there is no shell to ask")
	assert.Equal(t, "/usr/bin", entries[0])
	assert.Contains(t, entries, "/opt/homebrew/bin")
}

// The shell is asked once per run: its PATH does not change under a running
// app, and hooks arrive one command at a time.
func TestPathProbesOnceAcrossConcurrentCallers(t *testing.T) {
	var probes atomic.Int64
	r := NewResolver(Options{Shell: "/bin/zsh", Probe: func(context.Context, string) (string, error) {
		probes.Add(1)
		time.Sleep(10 * time.Millisecond)
		return "/opt/tools/bin", nil
	}})

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			assert.Contains(t, r.Path(t.Context()), "/opt/tools/bin")
		})
	}
	wg.Wait()

	assert.Equal(t, int64(1), probes.Load())
}

// A failed probe is remembered too. Retrying would charge every later hook
// command the startup cost of a shell that already declined to answer.
func TestPathRemembersAFailedProbe(t *testing.T) {
	var probes atomic.Int64
	r := NewResolver(Options{Shell: "/bin/zsh", Probe: func(context.Context, string) (string, error) {
		probes.Add(1)
		return "", errors.New("no")
	}})

	require.NotEmpty(t, r.Path(t.Context()))
	require.NotEmpty(t, r.Path(t.Context()))
	assert.Equal(t, int64(1), probes.Load())
}

// Cancelling session creation must not be recorded as the shell's answer, so
// the probe runs on a context of its own.
func TestProbeOutlivesACancelledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	r := NewResolver(Options{Shell: "/bin/zsh", Probe: func(ctx context.Context, _ string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "/opt/tools/bin", nil
	}})

	assert.Contains(t, r.Path(ctx), "/opt/tools/bin")
}

func TestProbeIsBoundedByTheTimeout(t *testing.T) {
	r := NewResolver(Options{Shell: "/bin/zsh", Timeout: 20 * time.Millisecond, Probe: func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}})

	done := make(chan string, 1)
	go func() { done <- r.Path(t.Context()) }()
	select {
	case path := <-done:
		assert.NotEmpty(t, path)
	case <-time.After(5 * time.Second):
		t.Fatal("a hanging shell must not hang session creation")
	}
}

func TestEnvironReplacesOnlyPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HIVE_EXECENV_MARKER", "kept")

	r := NewResolver(Options{Shell: "/bin/zsh", Probe: func(context.Context, string) (string, error) {
		return "/opt/tools/bin", nil
	}})

	env := r.Environ(t.Context())
	var paths []string
	marker := false
	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, "PATH="); ok {
			paths = append(paths, value)
		}
		if kv == "HIVE_EXECENV_MARKER=kept" {
			marker = true
		}
	}
	require.Len(t, paths, 1, "exactly one PATH reaches the child")
	assert.Equal(t, r.Path(t.Context()), paths[0])
	assert.True(t, marker, "the rest of the environment is passed through")
}

// The real probe, against a stub standing in for the user's shell: it must read
// the child's own environment rather than trust the shell to echo a variable.
func TestShellPathReadsTheShellsEnvironment(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	require.NoError(t, os.WriteFile(shell, []byte(
		"#!/bin/sh\n"+
			"echo 'startup file noise'\n"+
			"export PATH=/opt/tools/bin:/usr/bin\n"+
			"exec \"$2\"\n"), 0o755))

	path, err := shellPath(t.Context(), shell)
	require.NoError(t, err)
	assert.Equal(t, "/opt/tools/bin:/usr/bin", path)
}

// A startup file may leave a background process holding the shell's stdout
// (`something &` in .zshrc). The shell's own exit must end the probe anyway —
// otherwise the resolver's lock is held until that stranger exits.
func TestShellPathReturnsWhenAChildHoldsThePipeOpen(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	require.NoError(t, os.WriteFile(shell, []byte(
		"#!/bin/sh\n"+
			"export PATH=/opt/tools/bin:/usr/bin\n"+
			"/bin/sleep 10 &\n"+
			"exec \"$2\"\n"), 0o755))

	start := time.Now()
	path, err := shellPath(t.Context(), shell)
	require.NoError(t, err)
	assert.Equal(t, "/opt/tools/bin:/usr/bin", path)
	assert.Less(t, time.Since(start), 8*time.Second,
		"the probe must end with the shell, not with whatever it left running")
}

func TestShellPathFailsWhenTheShellReportsNothing(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\necho hello\n"), 0o755))

	_, err := shellPath(t.Context(), shell)
	require.ErrorIs(t, err, errNoPath)
}

func slicesUnique(entries []string) []string {
	seen := make(map[string]bool, len(entries))
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		out = append(out, entry)
	}
	return out
}

// os/exec resolves a command name against the calling process's PATH and
// ignores the environment handed to the child, so the resolver has to answer
// this itself or a command only the login shell knows about never starts.
func TestLookPathSearchesTheResolvedPath(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	require.NoError(t, os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme"), []byte("not executable"), 0o644))
	t.Setenv("PATH", "/nonexistent")

	r := NewResolver(Options{Shell: "/bin/sh", Probe: func(context.Context, string) (string, error) {
		return dir, nil
	}})

	got, err := r.LookPath(t.Context(), "tool")
	require.NoError(t, err)
	assert.Equal(t, tool, got)

	got, err = r.LookPath(t.Context(), "/usr/bin/env")
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/env", got, "a path is not a name to search for")

	_, err = r.LookPath(t.Context(), "readme")
	require.ErrorIs(t, err, exec.ErrNotFound, "a file that is not executable is not a command")
}
