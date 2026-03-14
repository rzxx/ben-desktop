package desktopcore

type SyncService struct {
	app *App
}

func newSyncService(app *App) *SyncService {
	if app == nil {
		return nil
	}
	return &SyncService{app: app}
}
