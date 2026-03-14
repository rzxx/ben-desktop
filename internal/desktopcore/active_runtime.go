package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apitypes "ben/desktop/api/types"
)

var errActiveLibraryRuntimeStopped = errors.New("active library runtime is no longer available")

type activeLibraryRuntime struct {
	libraryID string
	ctx       context.Context
	cancel    context.CancelFunc
}

func (a *App) syncActiveLibraryRuntime(ctx context.Context) (apitypes.LocalContext, bool, error) {
	if a == nil {
		return apitypes.LocalContext{}, false, nil
	}

	local, err := a.requireActiveContext(ctx)
	if err != nil {
		if errors.Is(err, apitypes.ErrNoActiveLibrary) {
			a.clearActiveLibraryRuntime()
			return apitypes.LocalContext{}, false, nil
		}
		return apitypes.LocalContext{}, false, err
	}

	libraryID := strings.TrimSpace(local.LibraryID)
	a.runtimeMu.Lock()
	current := a.activeRuntime
	if current != nil && strings.TrimSpace(current.libraryID) == libraryID {
		a.runtimeMu.Unlock()
		return local, true, nil
	}

	scopeCtx, cancel := context.WithCancel(context.Background())
	a.activeRuntime = &activeLibraryRuntime{
		libraryID: libraryID,
		ctx:       scopeCtx,
		cancel:    cancel,
	}
	a.runtimeMu.Unlock()

	if current != nil && current.cancel != nil {
		current.cancel()
	}
	return local, true, nil
}

func (a *App) clearActiveLibraryRuntime() {
	if a == nil {
		return
	}

	a.runtimeMu.Lock()
	current := a.activeRuntime
	a.activeRuntime = nil
	a.runtimeMu.Unlock()

	if current != nil && current.cancel != nil {
		current.cancel()
	}
}

func (a *App) activeLibraryTaskContext(ctx context.Context, libraryID string) (context.Context, func(), error) {
	if a == nil {
		return nil, func() {}, fmt.Errorf("%w: app is nil", errActiveLibraryRuntimeStopped)
	}

	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return nil, func() {}, fmt.Errorf("%w: library id is required", errActiveLibraryRuntimeStopped)
	}

	a.runtimeMu.Lock()
	current := a.activeRuntime
	if current == nil || strings.TrimSpace(current.libraryID) != libraryID {
		a.runtimeMu.Unlock()
		local, ok, err := a.syncActiveLibraryRuntime(ctx)
		if err != nil {
			return nil, func() {}, err
		}
		if !ok || strings.TrimSpace(local.LibraryID) != libraryID {
			return nil, func() {}, fmt.Errorf("%w: %s", errActiveLibraryRuntimeStopped, libraryID)
		}
		a.runtimeMu.Lock()
		current = a.activeRuntime
		if current == nil || strings.TrimSpace(current.libraryID) != libraryID {
			a.runtimeMu.Unlock()
			return nil, func() {}, fmt.Errorf("%w: %s", errActiveLibraryRuntimeStopped, libraryID)
		}
	}
	scopeCtx := current.ctx
	a.runtimeMu.Unlock()

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(scopeCtx, cancel)
	return runCtx, func() {
		stop()
		cancel()
	}, nil
}

func (a *App) startActiveLibraryJob(
	ctx context.Context,
	jobID string,
	kind string,
	libraryID string,
	queuedMessage string,
	startupFailureMessage string,
	run func(context.Context),
) (JobSnapshot, error) {
	if a == nil {
		return JobSnapshot{}, fmt.Errorf("%w: app is nil", errActiveLibraryRuntimeStopped)
	}

	snapshot, started := a.jobs.Begin(jobID, kind, libraryID, queuedMessage)
	if !started {
		return snapshot, nil
	}

	runCtx, cleanup, err := a.activeLibraryTaskContext(ctx, libraryID)
	if err != nil {
		return a.failActiveLibraryJobStartup(jobID, kind, libraryID, startupFailureMessage, err), err
	}

	go func() {
		defer cleanup()
		run(runCtx)
	}()
	return snapshot, nil
}

func (a *App) failActiveLibraryJobStartup(jobID string, kind string, libraryID string, message string, err error) JobSnapshot {
	job := a.jobs.Track(jobID, kind, libraryID)
	if job == nil {
		return JobSnapshot{}
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "job failed to start"
	}
	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, apitypes.ErrNoActiveLibrary),
		errors.Is(err, errActiveLibraryRuntimeStopped):
		return job.Fail(1, message, nil)
	default:
		return job.Fail(1, message, err)
	}
}
