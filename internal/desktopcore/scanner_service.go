package desktopcore

type ScannerService struct {
	*App
}

func newScannerService(app *App) *ScannerService {
	if app == nil {
		return nil
	}
	return &ScannerService{App: app}
}
