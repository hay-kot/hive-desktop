package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/terminalimg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type imageClipboardStub struct {
	raw   []byte
	err   error
	reads int
}

func (s *imageClipboardStub) ReadImage(context.Context) ([]byte, error) {
	s.reads++
	return s.raw, s.err
}

func TestTerminalImagesClipboardReadsOnlyOnRequest(t *testing.T) {
	var raw bytes.Buffer
	require.NoError(t, png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	clipboard := &imageClipboardStub{raw: raw.Bytes()}
	service := &TerminalImagesService{store: terminalimg.NewStore(t.TempDir()), clipboard: clipboard}
	assert.Zero(t, clipboard.reads)
	pastes, err := service.Clipboard(t.Context())
	require.NoError(t, err)
	require.Len(t, pastes, 1)
	assert.Equal(t, 1, clipboard.reads)
	clipboard.raw = nil
	pastes, err = service.Clipboard(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pastes)
	clipboard.err = errors.New("clipboard denied")
	_, err = service.Clipboard(t.Context())
	require.Error(t, err)
	assert.Equal(t, KindUnavailable, KindOf(err))
	service.clipboard = nil
	pastes, err = service.Clipboard(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pastes)
}
