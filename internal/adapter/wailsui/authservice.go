package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/auth"
)

// AuthService is the Wails service exposing authentication to the frontend.
// State changes are pushed via the auth:updated event; the frontend re-reads
// Status on receipt.
type AuthService struct {
	backend auth.Backend
}

func NewAuthService(backend auth.Backend) *AuthService {
	return &AuthService{backend: backend}
}

func (s *AuthService) Status() auth.Status {
	return s.backend.Status(context.Background())
}

func (s *AuthService) StartDeviceFlow() (auth.DeviceFlowInfo, error) {
	return s.backend.StartDeviceFlow(context.Background())
}

func (s *AuthService) CancelDeviceFlow() {
	s.backend.CancelDeviceFlow()
}

func (s *AuthService) SetToken(token string) (auth.Status, error) {
	return s.backend.SetToken(context.Background(), token)
}

func (s *AuthService) SignOut() error {
	return s.backend.SignOut()
}
