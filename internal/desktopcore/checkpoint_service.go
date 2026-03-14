package desktopcore

import (
	"context"

	apitypes "ben/desktop/api/types"
)

type CheckpointService struct {
	app *App
}

func newCheckpointService(app *App) *CheckpointService {
	if app == nil {
		return nil
	}
	return &CheckpointService{app: app}
}

func (s *CheckpointService) PublishCheckpoint(ctx context.Context) (apitypes.LibraryCheckpointManifest, error) {
	return s.app.PublishCheckpoint(ctx)
}

func (s *CheckpointService) StartPublishCheckpoint(ctx context.Context) (JobSnapshot, error) {
	return s.app.StartPublishCheckpoint(ctx)
}

func (s *CheckpointService) CompactCheckpoint(ctx context.Context, force bool) (apitypes.CheckpointCompactionResult, error) {
	return s.app.CompactCheckpoint(ctx, force)
}

func (s *CheckpointService) StartCompactCheckpoint(ctx context.Context, force bool) (JobSnapshot, error) {
	return s.app.StartCompactCheckpoint(ctx, force)
}
