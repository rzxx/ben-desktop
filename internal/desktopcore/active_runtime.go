package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apitypes "ben/core/api/types"
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
