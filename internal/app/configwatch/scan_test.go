package configwatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
)

func TestStreamedRevisionCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("version: 1\nname: café\n")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	revision, state, err := fileRevision(t.Context(), path)
	require.NoError(t, err)
	require.Equal(t, configstate.Valid, state)
	require.Equal(t, configstate.BytesRevision(data), revision)
}

type blockedReadFile struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (f *blockedReadFile) Read([]byte) (int, error) {
	f.once.Do(func() { close(f.started) })
	<-f.closed
	return 0, errors.New("closed")
}

func (f *blockedReadFile) Close() error {
	f.once.Do(func() {})
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

func TestFileRevisionCancellationJoinsReadWorker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("blocked"), 0o600))
	file := &blockedReadFile{started: make(chan struct{}), closed: make(chan struct{})}
	originalOpen := openFile
	openFile = func(string) (readFile, error) { return file, nil }
	t.Cleanup(func() { openFile = originalOpen })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, err := fileRevision(ctx, path)
		done <- err
	}()
	<-file.started
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("file revision did not join its blocked read")
	}
	select {
	case <-file.closed:
	default:
		t.Fatal("file revision did not close the blocked reader")
	}
}

func TestFixedDirectoryIsObservationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.Mkdir(path, 0o700))
	_, _, err := fileRevision(t.Context(), path)
	require.Error(t, err)
}

type sequentialTopologyAuthority struct {
	topologies []Topology
	index      int
}

func (a *sequentialTopologyAuthority) Topology(context.Context) (Topology, error) {
	topology := a.topologies[a.index]
	if a.index < len(a.topologies)-1 {
		a.index++
	}
	return topology, nil
}

func (a *sequentialTopologyAuthority) Match(Change) Match { return Match{} }

func TestScanRegistersCandidateTopologyBeforeReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	authority := fixedAuthority{dir: dir, path: path}

	order := make(chan string, 2)
	originalOpen := openFile
	openFile = func(path string) (readFile, error) {
		order <- "read"
		return originalOpen(path)
	}
	t.Cleanup(func() { openFile = originalOpen })
	_, err := scanAuthority(t.Context(), authority, func(Topology) { order <- "add" })
	require.NoError(t, err)
	require.Equal(t, "add", <-order)
	require.Equal(t, "read", <-order)
}

func TestScanRegistersSecondTopologyBeforeSecondRead(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	secondPath := filepath.Join(secondDir, "config.yml")
	require.NoError(t, os.WriteFile(secondPath, []byte("two"), 0o600))
	authority := &sequentialTopologyAuthority{topologies: []Topology{
		{Directories: []string{firstDir}, Files: []AuthorityFile{{Key: "config", Path: filepath.Join(firstDir, "missing.yml")}}},
		{Directories: []string{secondDir}, Files: []AuthorityFile{{Key: "config", Path: secondPath}}},
	}}

	originalOpen := openFile
	secondRegistered := false
	openFile = func(path string) (readFile, error) {
		if path == secondPath && !secondRegistered {
			return nil, errors.New("second topology was not registered before reading")
		}
		return originalOpen(path)
	}
	t.Cleanup(func() { openFile = originalOpen })
	_, err := scanAuthority(t.Context(), authority, func(topology Topology) {
		if topology.Directories[0] == secondDir {
			secondRegistered = true
		}
	})
	require.NoError(t, err)
	require.True(t, secondRegistered)
}
