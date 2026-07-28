package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/sources/grafana"
)

// GrafanaService exposes the Grafana connector's stack auth to the frontend:
// connecting a stack by pasting its URL and a service-account token, and
// disconnecting one. State changes are pushed via the connection:updated
// signal; the frontend re-reads the Integrations list on receipt.
//
// It is one connector's service, not the app's login. Nothing in the app is
// gated on a stack being connected.
type GrafanaService struct {
	grafana *app.GrafanaService
}

func NewGrafanaService(g *app.GrafanaService) *GrafanaService { return &GrafanaService{grafana: g} }

func (s *GrafanaService) Connect(ctx context.Context, url, token string) (grafana.Stack, error) {
	return s.grafana.Connect(ctx, url, token)
}

func (s *GrafanaService) Disconnect(ctx context.Context, account string) error {
	return s.grafana.Disconnect(ctx, account)
}
