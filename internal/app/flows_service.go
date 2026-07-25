package app

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// FlowsService owns the flow definitions: the listing the picker renders,
// the CRUD the editor drives, and the layout files the canvas persists.
type FlowsService struct {
	flows     *flow.FlowStore
	db        *store.DB
	creds     credentials.Store
	onUpdated func()
}

func newFlowsService(flows *flow.FlowStore, db *store.DB, creds credentials.Store, onUpdated func()) *FlowsService {
	return &FlowsService{flows: flows, db: db, creds: creds, onUpdated: onUpdated}
}

// seedCredential is the account a new workspace's starter graph fetches as.
//
// Exactly one connected account is the case this resolves; several is
// reported rather than guessed at, because seeding a whole workspace against
// the wrong account is not something a user would notice until the feed was
// already wrong.
func (s *FlowsService) seedCredential() (string, error) {
	refs, err := credentials.ListProvider(s.creds, ghsource.Provider)
	if err != nil {
		return "", Wrap(err, KindInternal, "reading credentials")
	}
	switch len(refs) {
	case 0:
		return "", Errorf(KindInvalid, "Connect a GitHub account before creating a workspace.")
	case 1:
		return refs[0].String(), nil
	default:
		return "", Errorf(KindInvalid, "Several GitHub accounts are connected; pick one to create a workspace with.")
	}
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

// Create seeds a new flow named name, whose starter graph fetches as the
// connected GitHub account.
//
// No connected account is a KindInvalid failure rather than an empty starter
// graph: the graph is the point of creating a workspace, and a workspace of
// sources that cannot fetch is a worse first impression than being told to
// connect an account first.
func (s *FlowsService) Create(_ context.Context, name string) (flow.Flow, error) {
	credential, err := s.seedCredential()
	if err != nil {
		return flow.Flow{}, err
	}
	f, err := s.flows.Create(name, credential)
	if err != nil {
		return flow.Flow{}, Wrap(err, KindInvalid, "creating flow %q", name)
	}
	s.notifyUpdated()
	return f, nil
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
