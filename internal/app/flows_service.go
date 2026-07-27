package app

import (
	"context"
	"errors"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/profileimg"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// FlowsService owns the flow definitions: the listing the picker renders,
// the CRUD the editor drives, the layout files the canvas persists, and each
// profile's sidebar-rail avatar.
type FlowsService struct {
	flows     *flow.FlowStore
	db        *store.DB
	creds     credentials.Store
	images    *profileimg.Store
	onUpdated func()
}

func newFlowsService(flows *flow.FlowStore, db *store.DB, creds credentials.Store, images *profileimg.Store, onUpdated func()) *FlowsService {
	return &FlowsService{flows: flows, db: db, creds: creds, images: images, onUpdated: onUpdated}
}

// seedCredential is the account a starter graph fetches as, or "" when there
// is no unambiguous one.
//
// Exactly one connected account is the case this resolves. Zero has nothing
// to seed with; several is not guessed at, because seeding a whole workspace
// against the wrong account is not something a user would notice until the
// feed was already wrong. Neither is an error — a workspace is the thing that
// exists without any credential, so both simply mean "unseeded".
func (s *FlowsService) seedCredential() (string, error) {
	refs, err := credentials.ListProvider(s.creds, ghsource.Provider)
	if err != nil {
		return "", Wrap(err, KindInternal, "reading credentials")
	}
	if len(refs) != 1 {
		return "", nil
	}
	return refs[0].String(), nil
}

func (s *FlowsService) notifyUpdated() {
	if s.onUpdated != nil {
		s.onUpdated()
	}
}

// Statuses returns one status per flow file — valid and invalid alike — so a
// broken file shows up with its error instead of silently vanishing.
func (s *FlowsService) Statuses(context.Context) []flow.FlowStatus {
	return s.flows.Statuses()
}

// Create makes a new flow named name, seeded with the starter graph when
// exactly one GitHub account is connected and empty otherwise.
//
// Creating is never refused for want of a credential. First run creates the
// workspace before it offers to connect anything, so the unseeded case is the
// expected one there, not a failure — SeedStarter is what fills it in once
// the account exists.
func (s *FlowsService) Create(_ context.Context, name string) (flow.Flow, error) {
	credential, err := s.seedCredential()
	if err != nil {
		return flow.Flow{}, err
	}
	var seed flow.Seed
	if credential != "" {
		seed = starterSeed(credential)
	}
	f, err := s.flows.Create(name, seed)
	if err != nil {
		return flow.Flow{}, Wrap(err, KindInvalid, "creating flow %q", name)
	}
	s.notifyUpdated()
	return f, nil
}

// SeedStarter fills an empty workspace with the starter graph, fetching as
// the one connected GitHub account. First run calls it when the account it
// offered to connect finally exists: the workspace was created a step
// earlier, before there was anything to seed it with.
//
// A workspace that already has nodes is refused rather than appended to.
// Appending a second starter graph onto a graph someone has since edited is
// not a mistake they can undo.
func (s *FlowsService) SeedStarter(_ context.Context, id string) (flow.Flow, error) {
	f, ok := s.flows.Get(id)
	if !ok {
		return flow.Flow{}, Errorf(KindNotFound, "flow %q not found", id)
	}
	if len(f.Nodes) > 0 {
		return flow.Flow{}, Errorf(KindInvalid, "workspace %q already has nodes", id)
	}
	credential, err := s.seedCredential()
	if err != nil {
		return flow.Flow{}, err
	}
	if credential == "" {
		return flow.Flow{}, Errorf(KindInvalid, "Connect exactly one GitHub account to add the starter feeds.")
	}

	seed := starterSeed(credential)
	f.Nodes, f.Wires = seed.Nodes, seed.Wires
	if err := s.flows.Save(f); err != nil {
		return flow.Flow{}, Wrap(err, KindInvalid, "seeding flow %q", id)
	}
	if err := s.flows.SaveLayout(id, seed.Layout); err != nil {
		return flow.Flow{}, Wrap(err, KindInternal, "saving layout for flow %q", id)
	}
	s.notifyUpdated()

	seeded, ok := s.flows.Get(id)
	if !ok {
		return flow.Flow{}, Errorf(KindInternal, "flow %q vanished while seeding", id)
	}
	return seeded, nil
}

// Rename changes a flow's display name while preserving its stable id and
// graph definition.
func (s *FlowsService) Rename(_ context.Context, id, name string) (flow.Flow, error) {
	f, err := s.flows.Rename(id, name)
	if err != nil {
		return flow.Flow{}, Wrap(err, KindInvalid, "renaming flow %q", id)
	}
	s.notifyUpdated()
	return f, nil
}

// SetEnabled controls whether a flow participates in polling and execution
// while preserving its feed data and graph definition.
func (s *FlowsService) SetEnabled(_ context.Context, id string, enabled bool) (flow.Flow, error) {
	f, err := s.flows.SetEnabled(id, enabled)
	if err != nil {
		return flow.Flow{}, Wrap(err, KindInvalid, "updating flow %q", id)
	}
	s.notifyUpdated()
	return f, nil
}

// Delete removes a flow's files before purging its durable state, in that
// order. FlowStore.Delete treats an already-missing file as success, which is
// what makes a retry after a files-first partial deletion idempotent.
func (s *FlowsService) Delete(ctx context.Context, id string) error {
	if err := s.flows.Delete(id); err != nil {
		return Wrap(err, KindInvalid, "deleting flow %q", id)
	}
	// A leftover avatar is orphaned data, never a reason to fail the delete.
	_ = s.images.Delete(id)
	s.notifyUpdated()
	if s.db == nil {
		return Errorf(KindUnavailable, "the desktop store is unavailable")
	}
	return Wrap(s.db.PurgeProfile(ctx, id), KindInternal, "purging inbox rows for flow %q", id)
}

// Get returns one flow's full definition for the editor.
func (s *FlowsService) Get(_ context.Context, id string) (flow.Flow, error) {
	f, ok := s.flows.Get(id)
	if !ok {
		return flow.Flow{}, Errorf(KindNotFound, "flow %q not found", id)
	}
	return f, nil
}

// Save validates and persists a flow's definition. An invalid flow is
// rejected and the last-good file on disk — and the served flow — are left
// untouched.
func (s *FlowsService) Save(_ context.Context, f flow.Flow) error {
	return Wrap(s.flows.Save(f), KindInvalid, "saving flow %q", f.ID)
}

// SetProfileImage normalizes raw into the square avatar the rail draws, stores
// it under the data dir, and records its content hash on the flow. Replacing
// an existing image overwrites it.
func (s *FlowsService) SetProfileImage(_ context.Context, id string, raw []byte) (flow.Flow, error) {
	if _, ok := s.flows.Get(id); !ok {
		return flow.Flow{}, Errorf(KindNotFound, "flow %q not found", id)
	}
	hash, err := s.images.Set(id, raw)
	if err != nil {
		return flow.Flow{}, mapImageError(err)
	}
	f, err := s.flows.SetImage(id, hash)
	if err != nil {
		// The bytes landed but the reference did not; drop the orphan so a
		// retry starts clean and nothing renders an unreferenced file.
		_ = s.images.Delete(id)
		return flow.Flow{}, Wrap(err, KindInternal, "recording image for flow %q", id)
	}
	s.notifyUpdated()
	return f, nil
}

// ClearProfileImage removes a profile's avatar: the reference is dropped first
// so the rail stops drawing it even if the file removal that follows fails,
// leaving at worst an orphaned file the next set or delete reclaims.
func (s *FlowsService) ClearProfileImage(_ context.Context, id string) (flow.Flow, error) {
	f, err := s.flows.SetImage(id, "")
	if err != nil {
		return flow.Flow{}, Wrap(err, KindInvalid, "clearing image for flow %q", id)
	}
	_ = s.images.Delete(id)
	s.notifyUpdated()
	return f, nil
}

// ProfileImage returns a profile's stored avatar PNG, or nil when it has none.
func (s *FlowsService) ProfileImage(_ context.Context, id string) ([]byte, error) {
	data, ok, err := s.images.Get(id)
	if err != nil {
		return nil, Wrap(err, KindInternal, "reading image for flow %q", id)
	}
	if !ok {
		return nil, nil
	}
	return data, nil
}

// mapImageError turns a normalization failure into a user-facing message: the
// bad-input cases are KindInvalid so the settings view can show them verbatim,
// while an I/O failure stays internal.
func mapImageError(err error) error {
	switch {
	case errors.Is(err, profileimg.ErrEmpty):
		return Errorf(KindInvalid, "No image was provided.")
	case errors.Is(err, profileimg.ErrUnsupported):
		return Errorf(KindInvalid, "That file isn't a supported image. Use PNG, JPEG, GIF, or WebP.")
	case errors.Is(err, profileimg.ErrTooLarge):
		return Errorf(KindInvalid, "That image is too large. Choose a smaller file.")
	default:
		return Wrap(err, KindInternal, "processing image")
	}
}

// Layout returns a flow's canvas positions. A missing or broken layout file
// is not an error: an empty Layout tells the editor to lay out nodes fresh.
func (s *FlowsService) Layout(_ context.Context, id string) flow.Layout {
	return s.flows.GetLayout(id)
}

func (s *FlowsService) SaveLayout(_ context.Context, id string, layout flow.Layout) error {
	return Wrap(s.flows.SaveLayout(id, layout), KindInternal, "saving layout for flow %q", id)
}

// Sidebar returns how a flow's feed nodes are grouped into folders and
// ordered. A missing or broken file is not an error — an empty layout falls
// back to flow-node order.
func (s *FlowsService) Sidebar(_ context.Context, id string) flow.SidebarLayout {
	return s.flows.GetSidebar(id)
}

func (s *FlowsService) SaveSidebar(_ context.Context, id string, layout flow.SidebarLayout) error {
	return Wrap(s.flows.SaveSidebar(id, layout), KindInternal, "saving sidebar for flow %q", id)
}
