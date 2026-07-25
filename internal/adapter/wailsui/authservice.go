package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/auth"
)

// AuthService exposes authentication to the frontend. State changes are
// pushed via the auth:updated signal; the frontend re-reads Status on receipt.
type AuthService struct {
	auth *app.AuthService
}

func NewAuthService(a *app.AuthService) *AuthService { return &AuthService{auth: a} }

func (s *AuthService) Status() auth.Status { return s.auth.Status(context.Background()) }

func (s *AuthService) StartDeviceFlow(ctx context.Context) (auth.DeviceFlowInfo, error) {
	return s.auth.StartDeviceFlow(ctx)
}

func (s *AuthService) CancelDeviceFlow() { s.auth.CancelDeviceFlow(context.Background()) }

func (s *AuthService) SetToken(ctx context.Context, token string) (auth.Status, error) {
	return s.auth.SetToken(ctx, token)
}

func (s *AuthService) SignOut() error { return s.auth.SignOut(context.Background()) }
