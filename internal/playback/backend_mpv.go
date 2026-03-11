//go:build libmpv

package playback

import (
	"context"
	"math"
	"sync"

	mpv "github.com/gen2brain/go-mpv"
)

type mpvBackend struct {
	client *mpv.Mpv
	once   sync.Once
	events sync.Once
	done   chan struct{}
	out    chan BackendEvent
	wg     sync.WaitGroup
	mu     sync.Mutex

	currentURI   string
	preloadedURI string
}

func newBackend() Backend {
	client := mpv.New()
	_ = client.SetOptionString("terminal", "no")
	_ = client.SetOptionString("video", "no")
	_ = client.SetOptionString("audio-display", "no")
	_ = client.SetOptionString("gapless-audio", "yes")
	_ = client.SetOptionString("prefetch-playlist", "yes")
	if err := client.Initialize(); err != nil {
		return &mpvErrorBackend{err: err}
	}
	return &mpvBackend{
		client: client,
		done:   make(chan struct{}),
		out:    make(chan BackendEvent, 32),
	}
}

func (b *mpvBackend) Load(_ context.Context, uri string) error {
	b.ensureEventLoop()
	if err := b.client.Command([]string{"loadfile", uri, "replace"}); err != nil {
		return err
	}
	b.mu.Lock()
	b.currentURI = uri
	b.preloadedURI = ""
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) Play(_ context.Context) error {
	b.ensureEventLoop()
	return b.client.SetProperty("pause", mpv.FormatFlag, false)
}

func (b *mpvBackend) Pause(_ context.Context) error {
	return b.client.SetProperty("pause", mpv.FormatFlag, true)
}

func (b *mpvBackend) Stop(_ context.Context) error {
	if err := b.client.Command([]string{"stop"}); err != nil {
		return err
	}
	b.mu.Lock()
	b.currentURI = ""
	b.preloadedURI = ""
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) SeekTo(_ context.Context, positionMS int64) error {
	seconds := float64(positionMS) / 1000.0
	return b.client.SetProperty("time-pos", mpv.FormatDouble, seconds)
}

func (b *mpvBackend) SetVolume(_ context.Context, volume int) error {
	return b.client.SetProperty("volume", mpv.FormatDouble, float64(volume))
}

func (b *mpvBackend) PositionMS() (int64, error) {
	value, err := b.client.GetProperty("time-pos", mpv.FormatDouble)
	if err != nil {
		return 0, err
	}
	floatValue, ok := value.(float64)
	if !ok || math.IsNaN(floatValue) || floatValue < 0 {
		return 0, nil
	}
	return int64(math.Round(floatValue * 1000)), nil
}

func (b *mpvBackend) DurationMS() (*int64, error) {
	value, err := b.client.GetProperty("duration", mpv.FormatDouble)
	if err != nil {
		if err == mpv.ErrPropertyUnavailable || err == mpv.ErrPropertyNotFound {
			return nil, nil
		}
		return nil, err
	}
	floatValue, ok := value.(float64)
	if !ok || math.IsNaN(floatValue) || floatValue < 0 {
		return nil, nil
	}
	result := int64(math.Round(floatValue * 1000))
	return &result, nil
}

func (b *mpvBackend) Events() <-chan BackendEvent {
	return b.out
}

func (b *mpvBackend) SupportsPreload() bool {
	return true
}

func (b *mpvBackend) PreloadNext(_ context.Context, uri string) error {
	b.mu.Lock()
	if uri == "" || uri == b.currentURI || uri == b.preloadedURI {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if err := b.client.Command([]string{"loadfile", uri, "append"}); err != nil {
		return err
	}
	b.mu.Lock()
	b.preloadedURI = uri
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) ClearPreloaded(context.Context) error {
	b.mu.Lock()
	b.preloadedURI = ""
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) Close() error {
	b.once.Do(func() {
		if b.client != nil {
			close(b.done)
			b.client.Wakeup()
			b.wg.Wait()
			b.client.TerminateDestroy()
		}
		close(b.out)
	})
	return nil
}

func (b *mpvBackend) ensureEventLoop() {
	b.events.Do(func() {
		b.wg.Add(1)
		go b.runEvents()
	})
}

func (b *mpvBackend) runEvents() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		default:
		}

		ev := b.client.WaitEvent(0.5)
		if ev == nil {
			continue
		}
		if ev.Error != nil {
			b.pushEvent(BackendEvent{Type: BackendEventError, Err: ev.Error})
		}
		switch ev.EventID {
		case mpv.EventEnd:
			end := ev.EndFile()
			if end.Reason == mpv.EndFileEOF {
				b.mu.Lock()
				if b.preloadedURI != "" {
					b.currentURI = b.preloadedURI
					b.preloadedURI = ""
				} else {
					b.currentURI = ""
				}
				b.mu.Unlock()
			}
			b.pushEvent(BackendEvent{
				Type:   BackendEventTrackEnd,
				Reason: mapEndReason(end.Reason),
				Err:    end.Error,
			})
		case mpv.EventShutdown:
			b.pushEvent(BackendEvent{Type: BackendEventShutdown})
			return
		}
	}
}

func (b *mpvBackend) pushEvent(event BackendEvent) {
	select {
	case <-b.done:
		return
	case b.out <- event:
	default:
	}
}

func mapEndReason(reason mpv.Reason) string {
	switch reason {
	case mpv.EndFileEOF:
		return TrackEndReasonEOF
	case mpv.EndFileStop:
		return TrackEndReasonStop
	case mpv.EndFileQuit:
		return TrackEndReasonQuit
	case mpv.EndFileError:
		return TrackEndReasonError
	case mpv.EndFileRedirect:
		return TrackEndReasonRedirect
	default:
		return ""
	}
}

type mpvErrorBackend struct {
	err error
}

func (b *mpvErrorBackend) Load(context.Context, string) error        { return b.err }
func (b *mpvErrorBackend) Play(context.Context) error                { return b.err }
func (b *mpvErrorBackend) Pause(context.Context) error               { return b.err }
func (b *mpvErrorBackend) Stop(context.Context) error                { return b.err }
func (b *mpvErrorBackend) SeekTo(context.Context, int64) error       { return b.err }
func (b *mpvErrorBackend) SetVolume(context.Context, int) error      { return b.err }
func (b *mpvErrorBackend) PositionMS() (int64, error)                { return 0, b.err }
func (b *mpvErrorBackend) DurationMS() (*int64, error)               { return nil, b.err }
func (b *mpvErrorBackend) Events() <-chan BackendEvent               { return nil }
func (b *mpvErrorBackend) SupportsPreload() bool                     { return false }
func (b *mpvErrorBackend) PreloadNext(context.Context, string) error { return b.err }
func (b *mpvErrorBackend) ClearPreloaded(context.Context) error      { return b.err }
func (b *mpvErrorBackend) Close() error                              { return nil }
