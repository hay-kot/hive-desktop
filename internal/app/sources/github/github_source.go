package github

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/feed"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// githubSource is the Source that produces one flow github-source node's
// current items. It does not fetch GitHub itself: it delegates to
// feed.LiveProvider.SourceItems, the same coalesced, cached, conditional-
// request fetch path the desktop feed already uses — so the pipeline gains a
// producer without a second implementation of GitHub fetching to keep in
// sync.
type githubSource struct {
	live      *feed.LiveProvider
	def       feed.SourceDef
	topic     string // "source:<flowId>/<nodeId>"
	profileID string
	policy    flow.ResurfacePolicy
}

func (s *githubSource) IngestMetadata() ingest.SourceMetadata {
	return ingest.SourceMetadata{ProfileID: s.profileID, SourceKind: "github", Policy: store.ResurfacePolicy(s.policy)}
}

// searchDef exposes this source's definition for the producer's batched
// prefetch. Notifications retain their independent conditional REST fetch.
func (s *githubSource) SearchDef() (feed.SourceDef, bool) {
	return s.def, s.def.Kind == "search"
}

// Compile-time proof that the connector still satisfies the ingest
// capabilities across the package boundary. Without these an
// unexported-method regression is a silent runtime fallback to generic
// ingestion -- no classifier, no absence confirmation -- rather than a build
// failure.
var (
	_ ingest.Source          = (*githubSource)(nil)
	_ ingest.MetadataSource  = (*githubSource)(nil)
	_ ingest.SearchDefSource = (*githubSource)(nil)
)

// Produce emits one Msg per current item of the source, JSON-encoding
// feed.Item as the payload. Topic is the flow-qualified
// "source:<flowId>/<nodeId>" so a frontend graph only ingests its own source
// nodes' rows; Key is the item's stable ID, used to skip unchanged source
// values within that topic.
func (s *githubSource) Produce(ctx context.Context, emit func(ingest.Msg) error) error {
	items, err := s.live.SourceItems(ctx, s.def)
	if err != nil {
		return fmt.Errorf("pipeline: fetching source %q: %w", s.def.ID, err)
	}

	for _, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("pipeline: encoding item %q from source %q: %w", item.ID, s.def.ID, err)
		}
		msg := ingest.Msg{
			Key: item.ID, Topic: s.topic, Payload: payload,
			SourceKind: "github",
		}
		if err := emit(msg); err != nil {
			return err
		}
	}
	return nil
}

// FlowLister is the subset of *flow.FlowStore the source lister needs: the
// current set of loaded flows. Producer calls it once per tick — rather than
// fixing the set at construction — so a source node added to, edited in, or
// removed from any flow takes effect on the next tick without a restart.
type FlowLister interface {
	List() []flow.Flow
}

// NewFlowSourceLister returns a SourceLister over every enabled github-source
// node across all flows: one githubSource per node, keyed and topic-tagged by
// its flow-qualified id "<flowId>/<nodeId>". Two nodes with identical fetch
// config still share one GitHub request — LiveProvider keys its cache on
// kind+query+limit, not id — while producing distinct topics so each flow's
// graph ingests only its own rows.
func NewFlowSourceLister(live *feed.LiveProvider, flows FlowLister) ingest.SourceLister {
	return func(context.Context) (map[string]ingest.Source, error) {
		out := map[string]ingest.Source{}
		for _, f := range flows.List() {
			if !f.Enabled {
				continue
			}
			for _, node := range f.Nodes {
				if node.Disabled || node.Type != "github-source" {
					continue
				}
				cfg, ok := node.Config.(*flow.GithubSourceConfig)
				if !ok {
					continue
				}
				id := f.ID + "/" + node.ID
				out[id] = &githubSource{
					live:      live,
					def:       feed.SourceDef{ID: id, Kind: cfg.Kind, Query: cfg.Query, Limit: cfg.Limit},
					topic:     "source:" + id,
					profileID: f.ID,
					policy:    f.Resurface,
				}
			}
		}
		return out, nil
	}
}
