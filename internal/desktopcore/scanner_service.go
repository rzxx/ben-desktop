package desktopcore

import "context"

type ScannerService struct {
	app *App
}

func newScannerService(app *App) *ScannerService {
	if app == nil {
		return nil
	}
	return &ScannerService{app: app}
}

func (s *ScannerService) SyncActive(ctx context.Context) error {
	return s.app.syncActiveScanWatcher(ctx)
}
