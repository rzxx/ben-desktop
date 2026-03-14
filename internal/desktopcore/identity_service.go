package desktopcore

import (
	"context"

	apitypes "ben/desktop/api/types"
)

type IdentityMembershipService struct {
	app *App
}

func newIdentityMembershipService(app *App) *IdentityMembershipService {
	if app == nil {
		return nil
	}
	return &IdentityMembershipService{app: app}
}

func (s *IdentityMembershipService) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return s.app.EnsureLocalContext(ctx)
}
