package main

import (
	"context"
	"sync"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/appupdate"
	"ben/desktop/internal/buildinfo"
)

type AppUpdateFacade struct {
	info buildinfo.Info

	mu      sync.RWMutex
	runner  *appupdate.CheckRunner
	running bool
}

func NewAppUpdateFacade(info buildinfo.Info) *AppUpdateFacade {
	return &AppUpdateFacade{info: info}
}

func (f *AppUpdateFacade) ServiceName() string { return "AppUpdateFacade" }

func (f *AppUpdateFacade) bindRunner(runner *appupdate.CheckRunner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runner = runner
}

func (f *AppUpdateFacade) GetStatus(context.Context) apitypes.AppUpdateStatus {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return apitypes.AppUpdateStatus{
		AppVersion:       f.info.AppVersion,
		BuildCommit:      f.info.BuildCommit,
		BuildTime:        f.info.BuildTime,
		GitHubRepository: f.info.GitHubRepository,
		Running:          f.running,
	}
}

func (f *AppUpdateFacade) CheckForUpdates(context.Context) (apitypes.AppUpdateCheckResult, error) {
	f.mu.Lock()
	if f.runner == nil {
		f.mu.Unlock()
		return apitypes.AppUpdateCheckResult{Started: false, Message: "Updater is unavailable"}, nil
	}
	if f.running {
		f.mu.Unlock()
		return apitypes.AppUpdateCheckResult{Started: false, Message: "An update check is already running"}, nil
	}
	started := f.runner.StartManualCheck(f.clearRunning)
	if started {
		f.running = true
	}
	f.mu.Unlock()
	if !started {
		return apitypes.AppUpdateCheckResult{Started: false, Message: "An update check is already running"}, nil
	}
	return apitypes.AppUpdateCheckResult{Started: true, Message: "Update check started"}, nil
}

func (f *AppUpdateFacade) clearRunning() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
}
