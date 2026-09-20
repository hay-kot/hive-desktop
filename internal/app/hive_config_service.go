package app

import (
	"context"
	"errors"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/hiveconf"
)

// HiveSetup is the external Hive configuration as the first-run and settings
// screens see it: what the file declares, what this machine could run, and
// whether the environment is overriding the choice.
type HiveSetup struct {
	Config hiveconf.Setup `json:"config"`
	// Agents is the catalog to pick from, marked by what resolved on PATH. It
	// travels with the setup because both screens that read one need the
	// other, and the two are one question — which agent, from what is here.
	Agents []hiveconf.AgentOption `json:"agents"`
	// DefaultAgentOverride is HIVE_DEFAULT_AGENT when it is set and names a
	// profile the file declares — that is, when it would actually win over
	// agents.default at load. It is reported rather than hidden because
	// without it, a user who exports the variable picks an agent in the UI,
	// sees it saved, and watches sessions start with a different one.
	DefaultAgentOverride string `json:"defaultAgentOverride"`
}

// HiveSetupRequest is a whole edit, not a delta: the screen holds every
// profile and every workspace while it is open, so a save reconciles the file
// to exactly this set. Profiles the screen loaded and did not touch come back
// unchanged, which is what keeps a hand-written profile from being dropped by
// an edit that was only about workspaces.
type HiveSetupRequest struct {
	DefaultAgent string             `json:"defaultAgent"`
	Profiles     []hiveconf.Profile `json:"profiles"`
	Workspaces   []string           `json:"workspaces"`
}

// defaultAgentSource reads HIVE_DEFAULT_AGENT from the environment a launched
// app resolves through the login shell. Declared here rather than reaching for
// hive's own constant: the name of a variable is not worth an import across
// the hivecore seam, and app.go already owns the one reader of it.
type defaultAgentSource interface {
	DefaultAgent(context.Context) string
}

type hiveConfigOptions struct {
	Location     func() HiveConfigLocation
	LookPath     hiveconf.LookPath
	DefaultAgent defaultAgentSource
	// Reload rebuilds the Hive-config-derived services. Save calls it so a
	// write takes effect in the running process instead of at the next launch
	// (ADR the-hive-runtime-rebinds-on-a-config-write-instead-of-requiring-a-restart).
	Reload func(context.Context) error
}

// HiveConfigService reads and writes the Hive CLI configuration the desktop
// shares with the `hive` binary.
//
// It is separate from SystemService, which owns the app's own locations: this
// one owns a file another product also writes, and the whole of its contract
// is that an edit touches the two keys the desktop asked about and nothing
// else.
type HiveConfigService struct {
	location     func() HiveConfigLocation
	lookPath     hiveconf.LookPath
	defaultAgent defaultAgentSource
	reload       func(context.Context) error
}

func newHiveConfigService(opts hiveConfigOptions) *HiveConfigService {
	return &HiveConfigService{
		location:     opts.Location,
		lookPath:     opts.LookPath,
		defaultAgent: opts.DefaultAgent,
		reload:       opts.Reload,
	}
}

// Setup reports the current configuration and the agents this machine can run.
func (s *HiveConfigService) Setup(ctx context.Context) HiveSetup {
	location := s.location()
	setup := hiveconf.Load(location.Path)
	view := HiveSetup{
		Config: setup,
		Agents: hiveconf.AgentOptions(ctx, s.lookPath),
	}
	// Reported only when it would actually win. Hive ignores the variable when
	// it names no configured profile (resolveHiveDefaultAgent), so announcing
	// one that does not would warn about an override that is not happening.
	if s.defaultAgent != nil {
		override := strings.TrimSpace(s.defaultAgent.DefaultAgent(ctx))
		for _, profile := range setup.Profiles {
			if profile.Name == override {
				view.DefaultAgentOverride = override
				break
			}
		}
	}
	return view
}

// InspectWorkspace validates a candidate parent folder and reports what is in
// it. The repository count is the confirmation that matters: a folder with no
// repositories in it is almost always the wrong folder, and saying so before
// the save is cheaper than an empty session picker afterwards.
func (s *HiveConfigService) InspectWorkspace(_ context.Context, path string) (hiveconf.Workspace, error) {
	if err := hiveconf.ValidateWorkspace(path); err != nil {
		return hiveconf.Workspace{}, Wrap(err, KindInvalid, "%s", path)
	}
	return hiveconf.Inspect(path), nil
}

// Save writes the edit and reloads the runtime from it.
//
// The write is validated first and is atomic, so a rejected edit leaves the
// file exactly as it was. A reload failure after a successful write is
// reported, not swallowed: the file on disk is the user's new configuration
// either way, and the honest answer is that it needs a restart to take effect.
func (s *HiveConfigService) Save(ctx context.Context, req HiveSetupRequest) (HiveSetup, error) {
	location := s.location()
	if location.Path == "" {
		return HiveSetup{}, Errorf(KindInternal, "no Hive config path is available")
	}

	edit := hiveconf.Edit{
		DefaultAgent: strings.TrimSpace(req.DefaultAgent),
		Workspaces:   make([]string, 0, len(req.Workspaces)),
	}
	seen := make(map[string]bool, len(req.Workspaces))
	for _, workspace := range req.Workspaces {
		trimmed := strings.TrimSpace(workspace)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		edit.Workspaces = append(edit.Workspaces, trimmed)
	}
	for _, profile := range req.Profiles {
		edit.Profiles = append(edit.Profiles, hiveconf.Profile{
			Name:    strings.TrimSpace(profile.Name),
			Command: strings.TrimSpace(profile.Command),
			Flags:   profile.Flags,
		})
	}

	if err := hiveconf.Apply(location.Path, edit); err != nil {
		return HiveSetup{}, Wrap(err, hiveWriteKind(err), "saving the Hive configuration")
	}
	if s.reload != nil {
		if err := s.reload(ctx); err != nil {
			return s.Setup(ctx), Wrap(err, KindInternal, "the Hive configuration was saved, but reloading it failed — restart Hive to apply it")
		}
	}
	return s.Setup(ctx), nil
}

// hiveWriteKind separates an edit the user can fix from a disk that would not
// take the write. Only the former is worth showing beside the field that
// caused it.
func hiveWriteKind(err error) Kind {
	if _, ok := errors.AsType[hiveconf.InvalidEditError](err); ok {
		return KindInvalid
	}
	return KindInternal
}
