package desktopcore

type OperatorService struct {
	*App
}

func newOperatorService(app *App) *OperatorService {
	if app == nil {
		return nil
	}
	return &OperatorService{App: app}
}
