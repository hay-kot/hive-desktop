package app

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/hay-kot/hive-desktop/internal/app/terminalimg"
)

type ClipboardImageReader interface {
	ReadImage(context.Context) ([]byte, error)
}

type TerminalImagesService struct {
	store     *terminalimg.Store
	clipboard ClipboardImageReader
}

func (s *TerminalImagesService) Clipboard(ctx context.Context) ([]string, error) {
	if s.clipboard == nil {
		return []string{}, nil
	}
	raw, err := s.clipboard.ReadImage(ctx)
	if err != nil {
		return nil, Wrap(err, KindUnavailable, "Could not read the clipboard image. Save it to a file and drag it into the terminal.")
	}
	if len(raw) == 0 {
		return []string{}, nil
	}
	return s.Save(ctx, []io.Reader{bytes.NewReader(raw)})
}

func (s *TerminalImagesService) Prepare(ctx context.Context, paths []string) ([]string, error) {
	pastes, err := s.store.Prepare(ctx, paths)
	return pastes, imageInputError(err)
}

func (s *TerminalImagesService) Save(ctx context.Context, images []io.Reader) ([]string, error) {
	pastes, err := s.store.Save(ctx, images)
	return pastes, imageInputError(err)
}

func imageInputError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, terminalimg.ErrInvalid) {
		return Wrap(err, KindInvalid, "%s", err)
	}
	if errors.Is(err, terminalimg.ErrQuota) {
		return Wrap(err, KindConflict, "%s", err)
	}
	return Wrap(err, KindInternal, "Could not prepare the image. Check that image storage is writable and has free space.")
}
