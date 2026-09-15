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
