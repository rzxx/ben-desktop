package desktopcore

type SyncService struct {
	*App
}

func newSyncService(app *App) *SyncService {
	if app == nil {
		return nil
	}
	return &SyncService{App: app}
}
