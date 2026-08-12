package app

import (
	"context"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/prompts"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/skills"
)

// SkillsService installs the paste-ready prompts as agent skills. It composes
// three things it does not own: PromptsService renders the skill bodies against
// this install, the skills.Installer writes them and tracks drift, and the
// settings store holds the per-target directories and the auto-update toggle.
//
// The unit of management is the agent, all-or-nothing: turning an agent on
// installs every skill to it; turning it off removes them. Whether an agent is on
// is derived from whether anything is installed, not from a stored flag.
type SkillsService struct {
	prompts   *PromptsService
	installer *skills.Installer
	settings  *settings.Store
}

func newSkillsService(p *PromptsService, installer *skills.Installer, store *settings.Store) *SkillsService {
	return &SkillsService{prompts: p, installer: installer, settings: store}
}

// SkillEntry is one installable skill, shown as an informational list entry with
// its preview text. The install state is per agent, not per skill, so it lives on
// SkillTarget rather than here.
type SkillEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Target names the surface the underlying prompt configures (a path or URL),
	// shown for orientation.
	Target string `json:"target"`
	Text   string `json:"text"` // rendered body, for preview and copy
}

// SkillTarget is one agent destination with its resolved directory and install
// state. Installed is how many skills are on this agent (0 means "off"); NeedsSync
// is set when an on agent has a skill missing or drifted that Sync would fix.
type SkillTarget struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Dir       string `json:"dir"`     // configured directory (override or default), as entered
	Default   bool   `json:"default"` // true when using the built-in default
	Installed int    `json:"installed"`
	NeedsSync bool   `json:"needsSync"`
}

// SkillsCatalog is the whole Skills settings surface in one value, so the
// frontend renders and every mutation returns the same shape.
type SkillsCatalog struct {
	Skills     []SkillEntry  `json:"skills"`
	Targets    []SkillTarget `json:"targets"`
	AutoUpdate bool          `json:"autoUpdate"`
}

// SkillsSyncResult is what a provisioning sync did, so the UI can report it.
type SkillsSyncResult struct {
	Catalog   SkillsCatalog `json:"catalog"`
	Installed int           `json:"installed"` // newly written
	Updated   int           `json:"updated"`   // app-changed, rewritten
	Restored  int           `json:"restored"`  // missing file recreated
	Skipped   int           `json:"skipped"`   // user-edited, left untouched
}

// SkillsTargetResult is what turning one agent on or off did.
type SkillsTargetResult struct {
	Catalog SkillsCatalog `json:"catalog"`
	Count   int           `json:"count"` // skills installed or removed
	Kept    int           `json:"kept"`  // user-edited files left in place (uninstall only)
}

// Catalog is the Skills surface: every listable prompt as a skill, the resolved
// target directories with their install counts, and the auto-update toggle.
func (s *SkillsService) Catalog(ctx context.Context, in prompts.Input) (SkillsCatalog, error) {
	return s.view(ctx, in)
}

// InstallTarget installs every skill to one agent — the "turn on" action. It
// overwrites unconditionally, so it also brings a partially-installed agent fully
// up to date.
func (s *SkillsService) InstallTarget(ctx context.Context, in prompts.Input, targetID string) (SkillsTargetResult, error) {
	target, dir, err := s.target(targetID)
	if err != nil {
		return SkillsTargetResult{}, err
	}
	list, err := s.prompts.Catalog(ctx, in)
	if err != nil {
		return SkillsTargetResult{}, err
	}
	count := 0
	for _, p := range list {
		if _, err := s.installer.Install(toSkill(p), target, dir); err != nil {
			return SkillsTargetResult{}, Wrap(err, KindInternal, "installing skill %q for %q", p.ID, targetID)
		}
		count++
	}
	catalog, err := s.view(ctx, in)
	if err != nil {
		return SkillsTargetResult{}, err
	}
	return SkillsTargetResult{Catalog: catalog, Count: count}, nil
}

// UninstallTarget removes every skill from one agent — the "turn off" action. A
// file the user edited is left in place and counted as kept.
func (s *SkillsService) UninstallTarget(ctx context.Context, in prompts.Input, targetID string) (SkillsTargetResult, error) {
	target, ok := skills.TargetByID(targetID)
	if !ok {
		return SkillsTargetResult{}, Errorf(KindNotFound, "unknown skill target %q", targetID)
	}
	removed, kept, err := s.installer.RemoveTarget(target)
	if err != nil {
		return SkillsTargetResult{}, Wrap(err, KindInternal, "uninstalling skills for %q", targetID)
	}
	catalog, err := s.view(ctx, in)
	if err != nil {
		return SkillsTargetResult{}, err
	}
	return SkillsTargetResult{Catalog: catalog, Count: removed, Kept: kept}, nil
}

// Sync provisions and maintains every agent that is on: it installs each skill the
// agent is missing and updates any that drifted, leaving files the user edited
// untouched and agents with nothing installed alone. It returns the refreshed
// catalog plus a summary for the caller to report.
func (s *SkillsService) Sync(ctx context.Context, in prompts.Input) (SkillsSyncResult, error) {
	cfg, err := s.settings.Effective()
	if err != nil {
		return SkillsSyncResult{}, Wrap(err, KindInternal, "reading settings")
	}
	list, err := s.prompts.Catalog(ctx, in)
	if err != nil {
		return SkillsSyncResult{}, err
	}

	var res SkillsSyncResult
	for _, rt := range resolveTargets(cfg) {
		if s.installer.InstalledCount(rt.target.ID) == 0 {
			continue // an "off" agent — never surprise-install
		}
		for _, p := range list {
			skill := toSkill(p)
			status, err := s.installer.Status(skill, rt.target, rt.dir)
			if err != nil {
				return res, Wrap(err, KindInternal, "reading skill status")
			}
			switch status.State {
			case skills.StateUpToDate:
			case skills.StateModified:
				res.Skipped++
			case skills.StateNotInstalled, skills.StateOutdated, skills.StateMissing:
				if _, err := s.installer.Install(skill, rt.target, rt.dir); err != nil {
					return res, Wrap(err, KindInternal, "installing skill %q for %q", p.ID, rt.target.ID)
				}
				switch status.State {
				case skills.StateNotInstalled:
					res.Installed++
				case skills.StateOutdated:
					res.Updated++
				case skills.StateMissing:
					res.Restored++
				default:
				}
			}
		}
	}

	res.Catalog, err = s.view(ctx, in)
	if err != nil {
		return res, err
	}
	return res, nil
}

// SetTargetDir overrides where a target installs. An empty directory clears the
// override, restoring the built-in default.
func (s *SkillsService) SetTargetDir(ctx context.Context, in prompts.Input, targetID, dir string) (SkillsCatalog, error) {
	if _, ok := skills.TargetByID(targetID); !ok {
		return SkillsCatalog{}, Errorf(KindNotFound, "unknown skill target %q", targetID)
	}
	dir = strings.TrimSpace(dir)
	if err := s.updateTarget(targetID, func(cfg *settings.SkillTargetSettings) { cfg.Dir = dir }); err != nil {
		return SkillsCatalog{}, Wrap(err, KindInternal, "saving skill target directory")
	}
	res, err := s.Sync(ctx, in)
	if err != nil {
		return SkillsCatalog{}, err
	}
	return res.Catalog, nil
}

// updateTarget applies mutate to one target's config, then drops the entry if it
// has decayed back to its default (no dir override) so the file stays sparse.
func (s *SkillsService) updateTarget(targetID string, mutate func(*settings.SkillTargetSettings)) error {
	_, err := s.settings.Update(func(cfg *settings.Settings) error {
		if cfg.Skills.Targets == nil {
			cfg.Skills.Targets = map[string]settings.SkillTargetSettings{}
		}
		entry := cfg.Skills.Targets[targetID]
		mutate(&entry)
		if strings.TrimSpace(entry.Dir) == "" {
			delete(cfg.Skills.Targets, targetID)
		} else {
			cfg.Skills.Targets[targetID] = entry
		}
		return nil
	})
	return err
}

// SetAutoUpdate persists the on-start maintenance toggle.
func (s *SkillsService) SetAutoUpdate(ctx context.Context, in prompts.Input, enabled bool) (SkillsCatalog, error) {
	if _, err := s.settings.Update(func(cfg *settings.Settings) error {
		cfg.Skills.AutoUpdate = enabled
		return nil
	}); err != nil {
		return SkillsCatalog{}, Wrap(err, KindInternal, "saving skill auto-update")
	}
	return s.view(ctx, in)
}

// SyncInstalled is the headless on-start entry point: it maintains already-
// installed skills (updates drift, restores a deleted file) but never installs a
// new one, so an agent with nothing installed is never surprised. The
// keyboard-shortcuts skill needs the command catalog the frontend owns, so on
// start it renders as absent and its installed file is left untouched until a sync
// runs with that context (the Skills tab).
func (s *SkillsService) SyncInstalled(ctx context.Context) (skills.SyncResult, error) {
	cfg, err := s.settings.Effective()
	if err != nil {
		return skills.SyncResult{}, Wrap(err, KindInternal, "reading settings")
	}
	catalog, err := s.skillMap(ctx, prompts.Input{})
	if err != nil {
		return skills.SyncResult{}, err
	}
	dirs := map[string]string{}
	for _, rt := range resolveTargets(cfg) {
		dirs[rt.target.ID] = rt.dir
	}
	return s.installer.Sync(catalog, func(t skills.Target) string { return dirs[t.ID] })
}

func (s *SkillsService) view(ctx context.Context, in prompts.Input) (SkillsCatalog, error) {
	cfg, err := s.settings.Effective()
	if err != nil {
		return SkillsCatalog{}, Wrap(err, KindInternal, "reading settings")
	}
	list, err := s.prompts.Catalog(ctx, in)
	if err != nil {
		return SkillsCatalog{}, err
	}
	resolved := resolveTargets(cfg)

	entries := make([]SkillEntry, 0, len(list))
	for _, p := range list {
		entries = append(entries, SkillEntry{
			ID:          p.ID,
			Name:        skillSlug(p.ID),
			Title:       p.Title,
			Description: skillDescription(p),
			Target:      p.Target,
			Text:        p.Text,
		})
	}

	targetInfos := make([]SkillTarget, 0, len(resolved))
	for _, rt := range resolved {
		installed, missing, drifted := 0, 0, false
		for _, p := range list {
			status, err := s.installer.Status(toSkill(p), rt.target, rt.dir)
			if err != nil {
				return SkillsCatalog{}, Wrap(err, KindInternal, "reading skill status")
			}
			switch status.State {
			case skills.StateUpToDate, skills.StateModified:
				installed++
			case skills.StateOutdated, skills.StateMissing:
				installed++
				drifted = true
			case skills.StateNotInstalled:
				missing++
			}
		}
		targetInfos = append(targetInfos, SkillTarget{
			ID:        rt.target.ID,
			Label:     rt.target.Label,
			Dir:       rt.dir,
			Default:   rt.isDefault,
			Installed: installed,
			// An on agent (installed > 0) that is missing a skill or has drift is
			// out of date; Sync would fix it.
			NeedsSync: installed > 0 && (drifted || missing > 0),
		})
	}
	return SkillsCatalog{Skills: entries, Targets: targetInfos, AutoUpdate: cfg.Skills.AutoUpdate}, nil
}

// ShippedSkills lists every skill this build ships as a workspace can enable
// it: the installed slug plus the labels the skill catalogue shows. A prompt
// that cannot render without caller-supplied context (the keybindings
// catalog, which only the frontend holds) is absent from the listing, so the
// catalogue never offers a skill a workspace open could not resolve.
func (s *SkillsService) ShippedSkills(ctx context.Context) ([]agentws.ShippedSkill, error) {
	list, err := s.prompts.Catalog(ctx, prompts.Input{})
	if err != nil {
		return nil, err
	}
	out := make([]agentws.ShippedSkill, 0, len(list))
	for _, p := range list {
		out = append(out, agentws.ShippedSkill{
			Slug:        skillSlug(p.ID),
			Title:       p.Title,
			Description: p.Description,
		})
	}
	return out, nil
}

// skillMap renders the current catalog as a lookup for the installer's sync.
func (s *SkillsService) skillMap(ctx context.Context, in prompts.Input) (map[string]skills.Skill, error) {
	list, err := s.prompts.Catalog(ctx, in)
	if err != nil {
		return nil, err
	}
	out := make(map[string]skills.Skill, len(list))
	for _, p := range list {
		out[p.ID] = toSkill(p)
	}
	return out, nil
}

// RenderSkill renders one listed prompt as a SKILL.md body for installation
// into a workspace rather than into an agent's home directory. It renders
// against the claude target: Target.Render needs one named target and all
// four registered targets share one skillBodyTmpl today, so the choice is
// arbitrary now — it stops being arbitrary the moment they diverge.
func (s *SkillsService) RenderSkill(ctx context.Context, id string) (name, body string, err error) {
	p, err := s.prompts.Render(ctx, id, prompts.Input{})
	if err != nil {
		return "", "", err
	}
	target, ok := skills.TargetByID("claude")
	if !ok {
		return "", "", Errorf(KindInternal, "unknown skill target %q", "claude")
	}
	skill := toSkill(p)
	rendered, err := target.Render(skill)
	if err != nil {
		return "", "", Wrap(err, KindInternal, "rendering skill %q", id)
	}
	return skill.Name, rendered, nil
}

func (s *SkillsService) target(id string) (skills.Target, string, error) {
	target, ok := skills.TargetByID(id)
	if !ok {
		return skills.Target{}, "", Errorf(KindNotFound, "unknown skill target %q", id)
	}
	cfg, err := s.settings.Effective()
	if err != nil {
		return skills.Target{}, "", Wrap(err, KindInternal, "reading settings")
	}
	return target, targetDir(cfg, target), nil
}

type resolvedTarget struct {
	target    skills.Target
	dir       string
	isDefault bool
}

func resolveTargets(cfg settings.Settings) []resolvedTarget {
	out := make([]resolvedTarget, 0, len(skills.Targets()))
	for _, t := range skills.Targets() {
		override := strings.TrimSpace(cfg.Skills.Targets[t.ID].Dir)
		rt := resolvedTarget{target: t, dir: override, isDefault: override == ""}
		if rt.isDefault {
			rt.dir = t.DefaultDir
		}
		out = append(out, rt)
	}
	return out
}

func targetDir(cfg settings.Settings, t skills.Target) string {
	if override := strings.TrimSpace(cfg.Skills.Targets[t.ID].Dir); override != "" {
		return override
	}
	return t.DefaultDir
}

// toSkill maps a rendered prompt onto the installer's Skill: a namespaced slug,
// a trigger-oriented description, and the prompt text as the body.
func toSkill(p prompts.Prompt) skills.Skill {
	return skills.Skill{
		ID:          p.ID,
		Name:        skillSlug(p.ID),
		Description: skillDescription(p),
		Body:        p.Text,
	}
}

func skillSlug(id string) string { return "hive-" + id }

// skillDescription builds the SKILL.md frontmatter description: what the skill
// does (the prompt's own description) plus where it applies, clamped to the
// standard's 1024-character limit.
func skillDescription(p prompts.Prompt) string {
	desc := strings.TrimSpace(p.Description)
	if target := strings.TrimSpace(p.Target); target != "" {
		desc += " Use when configuring this in Hive Desktop (" + target + ")."
	}
	if len(desc) > 1024 {
		desc = strings.TrimSpace(string([]rune(desc)[:min(len([]rune(desc)), 1024)]))
	}
	return desc
}
