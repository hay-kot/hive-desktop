// Package skills installs Hive Desktop's paste-ready prompts as agent skills:
// SKILL.md files written into the directories coding agents scan. It is the
// installer half of the prompt system — internal/app/prompts renders the text,
// this package wraps that text with per-target frontmatter, writes it atomically,
// and keeps an index so an installed skill can be re-synced when the app would
// render it differently (a moved config path, a new node type).
//
// The unit of work is a Skill (already-rendered content) written to a Target
// (an agent's skills directory). Drift detection distinguishes a change the app
// made to what it renders — safe to overwrite — from an edit the user made to the
// installed file — never overwritten. Adding a target agent is one entry in the
// registry (targets.go); everything here is target-agnostic.
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Skill is the content the installer writes, already rendered by the prompt
// engine. Name is the on-disk slug and, per the Agent Skills standard, must equal
// the directory the SKILL.md lives in.
type Skill struct {
	ID          string // stable prompt id, e.g. "flows"
	Name        string // namespaced slug and directory name, e.g. "hive-flows"
	Title       string // human label, for the UI (not written to disk)
	Description string // frontmatter description: what the skill does and when
	Body        string // the rendered prompt text
}

// State is the install status of one skill on one target.
type State string

const (
	StateNotInstalled State = "not-installed"
	StateUpToDate     State = "up-to-date"
	// StateOutdated: the file still matches what we wrote, but the app would now
	// render something different. Sync rewrites it.
	StateOutdated State = "outdated"
	// StateModified: the file was edited outside the app. Sync leaves it; only an
	// explicit re-install overwrites it.
	StateModified State = "modified"
	// StateMissing: the index has an entry but the file is gone. Sync recreates it.
	StateMissing State = "missing"
)

// Install reports where a skill is (or would be) installed on a target and its
// current state.
type Install struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	State  State  `json:"state"`
}

// SyncResult summarises one sync pass.
type SyncResult struct {
	Updated  int `json:"updated"`  // outdated files rewritten
	Restored int `json:"restored"` // missing files recreated
	Modified int `json:"modified"` // user-edited files left untouched
	Skipped  int `json:"skipped"`  // index entries for an unknown skill or target
}

// Installer writes, removes, and re-syncs skill files, backed by a persisted
// index. Safe for concurrent use: the on-start sync and a UI install can run at
// once.
type Installer struct {
	mu    sync.Mutex
	index *index
}

// NewInstaller loads (or starts) the index at indexPath.
func NewInstaller(indexPath string) (*Installer, error) {
	idx, err := loadIndex(indexPath)
	if err != nil {
		return nil, err
	}
	return &Installer{index: idx}, nil
}

// Install writes s onto t at dir (a ~ prefix is expanded), records it in the
// index, and returns the resulting install. It overwrites unconditionally, so it
// is also how the UI force-updates a modified file.
func (in *Installer) Install(s Skill, t Target, dir string) (Install, error) {
	if err := ValidateSkill(s); err != nil {
		return Install{}, err
	}
	in.mu.Lock()
	defer in.mu.Unlock()

	abs, err := absPath(s, t, dir)
	if err != nil {
		return Install{}, err
	}
	content, err := t.Render(s)
	if err != nil {
		return Install{}, err
	}
	if err := atomicWrite(abs, []byte(content)); err != nil {
		return Install{}, fmt.Errorf("skills: write %s: %w", abs, err)
	}

	now := time.Now()
	key := entryKey{s.ID, t.ID}
	entry := in.index.entries[key]
	removeCleanPreviousInstall(entry, abs)
	if entry.InstalledAt.IsZero() {
		entry.InstalledAt = now
	}
	entry.Skill, entry.Target, entry.Path, entry.Hash, entry.UpdatedAt = s.ID, t.ID, abs, digest(content), now
	in.index.entries[key] = entry
	if err := in.index.save(); err != nil {
		return Install{}, err
	}
	return Install{Target: t.ID, Path: abs, State: StateUpToDate}, nil
}

// Status reports the current install of s on t without touching disk. dir is the
// target's configured directory, used only to name where a not-yet-installed
// skill would go.
func (in *Installer) Status(s Skill, t Target, dir string) (Install, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	abs, err := absPath(s, t, dir)
	if err != nil {
		return Install{}, err
	}
	entry, ok := in.index.entries[entryKey{s.ID, t.ID}]
	if !ok {
		return Install{Target: t.ID, Path: abs, State: StateNotInstalled}, nil
	}
	content, err := t.Render(s)
	if err != nil {
		return Install{}, err
	}
	state, err := driftAt(entry, abs, content)
	if err != nil {
		return Install{}, err
	}
	return Install{Target: t.ID, Path: abs, State: state}, nil
}

// Remove deletes the installed file and drops the index entry. A file that no
// longer matches what we wrote (a user edit) is left in place — only the entry is
// removed — so an uninstall never destroys the user's work. Removing an empty
// skill directory afterward keeps the target dir tidy.
func (in *Installer) Remove(s Skill, t Target) error {
	in.mu.Lock()
	defer in.mu.Unlock()

	key := entryKey{s.ID, t.ID}
	entry, ok := in.index.entries[key]
	if !ok {
		return nil
	}
	if _, _, err := removeIndexedFile(entry); err != nil {
		return err
	}
	delete(in.index.entries, key)
	return in.index.save()
}

// Sync re-renders every indexed skill against current content: outdated files are
// rewritten, missing files recreated, user-edited files left untouched. skills is
// the current catalog keyed by skill id; an entry whose skill or target the build
// no longer knows is skipped. enabled, when non-nil, skips any target it reports
// false for, so a disabled target's installs go dormant rather than being updated.
func (in *Installer) Sync(skills map[string]Skill, enabled func(targetID string) bool) (SyncResult, error) {
	return in.SyncInDirs(skills, nil, enabled)
}

func (in *Installer) SyncInDirs(skills map[string]Skill, dirFor func(Target) string, enabled func(targetID string) bool) (SyncResult, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	var res SyncResult
	changed := false
	for key, entry := range in.index.entries {
		s, ok := skills[key.skill]
		if !ok {
			res.Skipped++
			continue
		}
		t, ok := TargetByID(key.target)
		if !ok {
			res.Skipped++
			continue
		}
		if enabled != nil && !enabled(key.target) {
			res.Skipped++
			continue
		}
		desired := entry.Path
		if dirFor != nil {
			dir := dirFor(t)
			var err error
			desired, err = absPath(s, t, dir)
			if err != nil {
				return res, err
			}
		}
		content, err := t.Render(s)
		if err != nil {
			return res, err
		}
		state, err := driftAt(entry, desired, content)
		if err != nil {
			return res, err
		}
		switch state {
		case StateModified:
			res.Modified++
		case StateOutdated, StateMissing:
			if err := atomicWrite(desired, []byte(content)); err != nil {
				return res, fmt.Errorf("skills: write %s: %w", desired, err)
			}
			removeCleanPreviousInstall(entry, desired)
			entry.Path = desired
			entry.Hash = digest(content)
			entry.UpdatedAt = time.Now()
			in.index.entries[key] = entry
			changed = true
			if state == StateMissing {
				res.Restored++
			} else {
				res.Updated++
			}
		case StateNotInstalled, StateUpToDate:
		}
	}
	if changed {
		if err := in.index.save(); err != nil {
			return res, err
		}
	}
	return res, nil
}

// InstalledCount reports how many skills are indexed for a target — the signal an
// agent is "on" (has skills installed).
func (in *Installer) InstalledCount(targetID string) int {
	in.mu.Lock()
	defer in.mu.Unlock()
	n := 0
	for key := range in.index.entries {
		if key.target == targetID {
			n++
		}
	}
	return n
}

// RemoveTarget uninstalls every skill indexed for a target: it deletes each file
// whose hash still matches what we wrote, drops every entry, and leaves a file the
// user edited in place. It returns how many files were deleted and how many edited
// files were kept.
func (in *Installer) RemoveTarget(t Target) (removed, kept int, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	changed := false
	for key, entry := range in.index.entries {
		if key.target != t.ID {
			continue
		}
		wasRemoved, wasKept, removeErr := removeIndexedFile(entry)
		if removeErr != nil {
			if changed {
				_ = in.index.save()
			}
			return removed, kept, removeErr
		}
		if wasRemoved {
			removed++
		}
		if wasKept {
			kept++
		}
		delete(in.index.entries, key)
		changed = true
	}
	if changed {
		if saveErr := in.index.save(); saveErr != nil {
			return removed, kept, saveErr
		}
	}
	return removed, kept, nil
}

func driftAt(entry Entry, desiredPath, rendered string) (State, error) {
	if entry.Path != desiredPath {
		return StateMissing, nil
	}
	data, err := os.ReadFile(entry.Path)
	if errors.Is(err, os.ErrNotExist) {
		return StateMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("skills: read %s: %w", entry.Path, err)
	}
	switch {
	case digest(string(data)) != entry.Hash:
		return StateModified, nil
	case digest(rendered) != entry.Hash:
		return StateOutdated, nil
	default:
		return StateUpToDate, nil
	}
}

func removeIndexedFile(entry Entry) (removed, kept bool, err error) {
	data, err := os.ReadFile(entry.Path)
	if errors.Is(err, os.ErrNotExist) {
		return true, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("skills: read %s: %w", entry.Path, err)
	}
	if digest(string(data)) != entry.Hash {
		return false, true, nil
	}
	if err := os.Remove(entry.Path); err != nil {
		return false, false, fmt.Errorf("skills: remove %s: %w", entry.Path, err)
	}
	removeEmptyDir(filepath.Dir(entry.Path))
	return true, false, nil
}

func removeCleanPreviousInstall(entry Entry, desiredPath string) {
	if entry.Path == "" || entry.Path == desiredPath {
		return
	}
	if data, err := os.ReadFile(entry.Path); err == nil && digest(string(data)) == entry.Hash {
		if err := os.Remove(entry.Path); err == nil {
			removeEmptyDir(filepath.Dir(entry.Path))
		}
	}
}

func absPath(s Skill, t Target, dir string) (string, error) {
	rel, err := t.RelPath(s)
	if err != nil {
		return "", err
	}
	base, err := expandHome(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.FromSlash(rel)), nil
}

// digest matches the repo convention (internal/app/profileimg): the first 16
// bytes of a SHA-256, hex-encoded.
func digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:16])
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("skills: resolve home dir: %w", err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".skill-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func removeEmptyDir(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidateSkill enforces the Agent Skills naming rules before a file is written,
// so an install never produces a SKILL.md an agent would reject.
func ValidateSkill(s Skill) error {
	if s.ID == "" {
		return errors.New("skills: skill id is required")
	}
	if l := len(s.Name); l == 0 || l > 64 {
		return fmt.Errorf("skills: skill name %q must be 1-64 characters", s.Name)
	}
	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("skills: skill name %q must be lowercase letters, digits and single hyphens", s.Name)
	}
	if strings.Contains(s.Name, "anthropic") || strings.Contains(s.Name, "claude") {
		return fmt.Errorf("skills: skill name %q must not contain a reserved word", s.Name)
	}
	if strings.TrimSpace(s.Description) == "" {
		return errors.New("skills: skill description is required")
	}
	if len(s.Description) > 1024 {
		return errors.New("skills: skill description must be at most 1024 characters")
	}
	if strings.TrimSpace(s.Body) == "" {
		return errors.New("skills: skill body is empty")
	}
	return nil
}
