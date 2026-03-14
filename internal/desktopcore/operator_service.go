package desktopcore

import (
	"context"

	apitypes "ben/desktop/api/types"
)

type OperatorService struct {
	app *App
}

func newOperatorService(app *App) *OperatorService {
	if app == nil {
		return nil
	}
	return &OperatorService{app: app}
}

func (s *OperatorService) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return s.app.EnsureLocalContext(ctx)
}

func (s *OperatorService) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return s.app.Inspect(ctx)
}

func (s *OperatorService) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return s.app.InspectLibraryOplog(ctx, libraryID)
}

func (s *OperatorService) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	return s.app.ActivityStatus(ctx)
}

func (s *OperatorService) NetworkStatus() apitypes.NetworkStatus {
	return s.app.NetworkStatus()
}

func (s *OperatorService) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return s.app.CheckpointStatus(ctx)
}
