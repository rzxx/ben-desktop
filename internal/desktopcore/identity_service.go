package desktopcore

type IdentityMembershipService struct {
	*App
}

func newIdentityMembershipService(app *App) *IdentityMembershipService {
	if app == nil {
		return nil
	}
	return &IdentityMembershipService{App: app}
}
