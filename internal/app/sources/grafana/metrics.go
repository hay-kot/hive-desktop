package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	"github.com/hay-kot/hive-desktop/internal/app/sources/grafana/client"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// metricsSource polls one node's PromQL query. It emits exactly one message per
// tick, keyed by the node id, so the node maps to a single durable inbox item
// whose payload is the latest query result. A downstream function node inspects
// msg.Payload.result to gate whether that item appears in a feed, but cannot
// change its title or url — those are minted at ingest from what this emits.
type metricsSource struct {
	fetcher *fetcher
	dsUID   string
	expr    string
	title   string
	topic   string
	key     string
}

var _ connector.PullSource = (*metricsSource)(nil)

// payload is what a metrics poll emits. title is the canonical presentation
// field the ingest boundary promotes onto the inbox item; result is the opaque
// query response a function node reads. There is no url — a metric has no
// canonical link, so the item is content-only.
type payload struct {
	Title  string             `json:"title"`
	Result client.QueryResult `json:"result"`
}

// Produce runs the query and emits one message. The producer records the
// authoritative snapshot after this returns, so the source never appends one
// itself; an emit of a single key means the node's one item is present, and a
// fetch error leaves the previous snapshot in place rather than clearing it.
func (s *metricsSource) Produce(ctx context.Context, emit func(store.Msg) error) error {
	result, err := s.fetcher.Query(ctx, s.dsUID, s.expr)
	if err != nil {
		return fmt.Errorf("grafana metrics %q: %w", s.key, err)
	}
	body, err := json.Marshal(payload{Title: s.itemTitle(), Result: result})
	if err != nil {
		return fmt.Errorf("grafana metrics %q: encoding payload: %w", s.key, err)
	}
	return emit(store.Msg{Key: s.key, Topic: s.topic, SourceKind: SourceKind, Payload: body})
}

// itemTitle is the configured feed title, or the query itself so the feed never
// shows a bare node id.
func (s *metricsSource) itemTitle() string {
	if t := strings.TrimSpace(s.title); t != "" {
		return t
	}
	return "Grafana: " + s.expr
}
