package desktopcore

type IdentityMembershipService struct {
	app *App
}

func newIdentityMembershipService(app *App) *IdentityMembershipService {
	if app == nil {
		return nil
	}
	return &IdentityMembershipService{app: app}
}
