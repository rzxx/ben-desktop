package desktopcore

type CheckpointService struct {
	*App
}

func newCheckpointService(app *App) *CheckpointService {
	if app == nil {
		return nil
	}
	return &CheckpointService{App: app}
}
