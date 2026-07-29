package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/prompts"
)

// SkillsService installs the paste-ready prompts as agent skills — the installer
// surface behind Settings ▸ Skills. Every method returns the whole catalog so a
// mutation and a read are the same shape; the input carries the frontend-owned
// command catalog the keyboard-shortcuts skill needs, exactly as PromptsService.
//
// Transport only. Rendering, installation, and drift detection live in the core
// (internal/app/skills, app.SkillsService).
type SkillsService struct {
	skills *app.SkillsService
}

func NewSkillsService(s *app.SkillsService) *SkillsService {
	return &SkillsService{skills: s}
}

func (s *SkillsService) Catalog(ctx context.Context, input prompts.Input) (app.SkillsCatalog, error) {
	return s.skills.Catalog(ctx, input)
}

func (s *SkillsService) InstallTarget(ctx context.Context, input prompts.Input, targetID string) (app.SkillsTargetResult, error) {
	return s.skills.InstallTarget(ctx, input, targetID)
}

func (s *SkillsService) UninstallTarget(ctx context.Context, input prompts.Input, targetID string) (app.SkillsTargetResult, error) {
	return s.skills.UninstallTarget(ctx, input, targetID)
}

func (s *SkillsService) Sync(ctx context.Context, input prompts.Input) (app.SkillsSyncResult, error) {
	return s.skills.Sync(ctx, input)
}

func (s *SkillsService) SetTargetDir(ctx context.Context, input prompts.Input, targetID, dir string) (app.SkillsCatalog, error) {
	return s.skills.SetTargetDir(ctx, input, targetID, dir)
}

func (s *SkillsService) SetAutoUpdate(ctx context.Context, input prompts.Input, enabled bool) (app.SkillsCatalog, error) {
	return s.skills.SetAutoUpdate(ctx, input, enabled)
}
