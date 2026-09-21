package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
)

type TerminalImagesService struct{ images *app.TerminalImagesService }

func NewTerminalImagesService(images *app.TerminalImagesService) *TerminalImagesService {
	return &TerminalImagesService{images: images}
}

func (s *TerminalImagesService) Clipboard(ctx context.Context) ([]string, error) {
	return s.images.Clipboard(ctx)
}
