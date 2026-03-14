package desktopcore

import "context"

type SyncService struct {
	app *App
}

func newSyncService(app *App) *SyncService {
	if app == nil {
		return nil
	}
	return &SyncService{app: app}
}

func (s *SyncService) StartSyncNow(ctx context.Context) (JobSnapshot, error) {
	return s.app.StartSyncNow(ctx)
}

func (s *SyncService) StartConnectPeer(ctx context.Context, peerAddr string) (JobSnapshot, error) {
	return s.app.StartConnectPeer(ctx, peerAddr)
}
