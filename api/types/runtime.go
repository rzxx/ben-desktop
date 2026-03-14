package apitypes

import (
	"context"
	"time"
)

const (
	ServiceDB      = "db"
	ServiceNetwork = "network"
	ServiceScanner = "scanner"
)

type ServiceState string

const (
	ServiceStateStarting ServiceState = "starting"
	ServiceStateRunning  ServiceState = "running"
	ServiceStateDegraded ServiceState = "degraded"
	ServiceStateStopping ServiceState = "stopping"
	ServiceStateStopped  ServiceState = "stopped"
)

type ServiceStatus struct {
	Service   string
	State     ServiceState
	LastError string
	Detail    string
	UpdatedAt time.Time
}

type RuntimeStatus struct {
	State     ServiceState
	LastError string
	Services  map[string]ServiceStatus
	UpdatedAt time.Time
}

type RuntimeSurface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() RuntimeStatus
}

// RuntimeAdminSurface exposes lifecycle recovery controls that are not part of
// the default player-facing API surface.
type RuntimeAdminSurface interface {
	RestartService(ctx context.Context, service string) error
}
