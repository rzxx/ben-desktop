//go:build nompv

package playback

import (
	"context"
	"errors"
)

var errNoMPVBackend = errors.New("mpv playback backend is disabled in this build")

type stubBackend struct {
	loadedURI string
	playing   bool
	paused    bool
	volume    int
	position  int64
	duration  *int64
	events    chan BackendEvent
}

func newBackend() Backend {
	return &stubBackend{
		volume: DefaultVolume,
		events: make(chan BackendEvent),
	}
}

func (b *stubBackend) Load(_ context.Context, uri string) error {
	b.loadedURI = uri
	b.position = 0
	return errNoMPVBackend
}

func (b *stubBackend) ActivatePreloaded(context.Context, string) (BackendActivationRef, error) {
	return BackendActivationRef{}, ErrUnsupportedPreloadActivation
}

func (b *stubBackend) Play(context.Context) error {
	if b.loadedURI == "" {
		return errNoMPVBackend
	}
	b.playing = true
	b.paused = false
	return errNoMPVBackend
}

func (b *stubBackend) Pause(context.Context) error {
	if !b.playing {
		return errNoMPVBackend
	}
	b.paused = true
	return errNoMPVBackend
}

func (b *stubBackend) Stop(context.Context) error {
	b.playing = false
	b.paused = false
	b.position = 0
	return errNoMPVBackend
}

func (b *stubBackend) SeekTo(_ context.Context, positionMS int64) error {
	if positionMS < 0 {
		positionMS = 0
	}
	b.position = positionMS
	return errNoMPVBackend
}

func (b *stubBackend) SetVolume(_ context.Context, volume int) error {
	b.volume = volume
	return nil
}

func (b *stubBackend) PositionMS() (int64, error) {
	return b.position, nil
}

func (b *stubBackend) DurationMS() (*int64, error) {
	return cloneInt64Ptr(b.duration), nil
}

func (b *stubBackend) Events() <-chan BackendEvent {
	return b.events
}

func (b *stubBackend) SupportsPreload() bool {
	return false
}

func (b *stubBackend) PreloadNext(context.Context, string) error {
	return errNoMPVBackend
}

func (b *stubBackend) ClearPreloaded(context.Context) error {
	return errNoMPVBackend
}

func (b *stubBackend) Close() error {
	close(b.events)
	return nil
}
