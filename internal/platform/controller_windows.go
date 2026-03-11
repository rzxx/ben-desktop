//go:build windows

package platform

import (
	"context"
	"log"
	"sync"
	"unsafe"

	"ben/desktop/internal/playback"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/zzl/go-win32api/v2/win32"
)

const (
	acceleratorMediaPlayPause = "MEDIA_PLAY_PAUSE"
	acceleratorMediaNextTrack = "MEDIA_NEXT_TRACK"
	acceleratorMediaPrevTrack = "MEDIA_PREV_TRACK"
)

type windowsController struct {
	app     *application.App
	session *playback.Session
	bridge  playback.CorePlaybackBridge

	smtc     *smtcService
	thumbbar *thumbbarService

	accelerators []string

	mu            sync.Mutex
	smtcStarted   bool
	smtcStarting  bool
	thumbStarted  bool
	thumbStarting bool
	hasLastState  bool
	lastState     playback.SessionSnapshot
}

func NewController(app *application.App, session *playback.Session, bridge playback.CorePlaybackBridge) playback.PlatformController {
	return &windowsController{
		app:      app,
		session:  session,
		bridge:   bridge,
		smtc:     newSMTCService(session, bridge),
		thumbbar: newThumbbarService(session),
	}
}

func (s *windowsController) Start() error {
	if s.app == nil || s.session == nil {
		return nil
	}

	if err := initializeProcessIdentity(); err != nil {
		log.Printf("platform app identity setup failed: %v", err)
	}

	if s.app.Window != nil {
		for _, window := range s.app.Window.GetAll() {
			s.watchWindow(window)
		}
		s.app.Window.OnCreate(func(window application.Window) {
			s.watchWindow(window)
		})
	}

	smtcStarted := s.startSMTCIfNeeded()
	s.startThumbbarIfNeeded()
	if !smtcStarted {
		s.registerMediaKeyBindings()
	}
	return nil
}

func (s *windowsController) Stop() error {
	if s.app != nil {
		s.unregisterMediaKeyBindings()
	}

	var shutdownErr error
	if s.smtc != nil {
		s.mu.Lock()
		s.smtcStarted = false
		s.smtcStarting = false
		s.mu.Unlock()
		if err := s.smtc.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	if s.thumbbar != nil {
		s.mu.Lock()
		s.thumbStarted = false
		s.thumbStarting = false
		s.mu.Unlock()
		if err := s.thumbbar.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	return shutdownErr
}

func (s *windowsController) HandlePlaybackSnapshot(snapshot playback.SessionSnapshot) {
	if s.smtc == nil && s.thumbbar == nil {
		return
	}

	s.mu.Lock()
	s.lastState = snapshot
	s.hasLastState = true
	smtcStarted := s.smtcStarted
	thumbStarted := s.thumbStarted
	s.mu.Unlock()

	if s.smtc != nil && !smtcStarted {
		s.startSMTCIfNeeded()
	} else if s.smtc != nil {
		s.smtc.UpdatePlaybackSnapshot(snapshot)
	}

	if s.thumbbar == nil {
		return
	}
	if !thumbStarted {
		s.startThumbbarIfNeeded()
		return
	}
	s.thumbbar.UpdatePlaybackSnapshot(snapshot)
}

func (s *windowsController) startSMTCIfNeeded() bool {
	if s.smtc == nil {
		return false
	}

	s.mu.Lock()
	if s.smtcStarted {
		s.mu.Unlock()
		return true
	}
	if s.smtcStarting {
		s.mu.Unlock()
		return false
	}
	s.smtcStarting = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.smtcStarting = false
		s.mu.Unlock()
	}()

	hwnd, ok := s.resolveWindowHandle()
	if !ok {
		return false
	}
	if err := s.smtc.Start(hwnd); err != nil {
		log.Printf("platform SMTC disabled: %v", err)
		return false
	}

	var pending playback.SessionSnapshot
	hasPending := false

	s.mu.Lock()
	s.smtcStarted = true
	if s.hasLastState {
		pending = s.lastState
		hasPending = true
	}
	s.mu.Unlock()

	s.unregisterMediaKeyBindings()
	if hasPending {
		s.smtc.UpdatePlaybackSnapshot(pending)
	}
	return true
}

func (s *windowsController) startThumbbarIfNeeded() bool {
	if s.thumbbar == nil {
		return false
	}

	s.mu.Lock()
	if s.thumbStarted {
		s.mu.Unlock()
		return true
	}
	if s.thumbStarting {
		s.mu.Unlock()
		return false
	}
	s.thumbStarting = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.thumbStarting = false
		s.mu.Unlock()
	}()

	hwnd, ok := s.resolveWindowHandle()
	if !ok {
		return false
	}
	if err := s.thumbbar.Start(hwnd); err != nil {
		log.Printf("platform thumbnail toolbar disabled: %v", err)
		return false
	}

	var pending playback.SessionSnapshot
	hasPending := false

	s.mu.Lock()
	s.thumbStarted = true
	if s.hasLastState {
		pending = s.lastState
		hasPending = true
	}
	s.mu.Unlock()

	if hasPending {
		s.thumbbar.UpdatePlaybackSnapshot(pending)
	}
	return true
}

func (s *windowsController) watchWindow(window application.Window) {
	if window == nil {
		return
	}

	if s.startSMTCIfNeeded() && s.startThumbbarIfNeeded() {
		return
	}

	var cancel func()
	cancel = window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(_ *application.WindowEvent) {
		if !s.startSMTCIfNeeded() || !s.startThumbbarIfNeeded() {
			return
		}
		if cancel != nil {
			cancel()
			cancel = nil
		}
	})
}

func (s *windowsController) resolveWindowHandle() (win32.HWND, bool) {
	if s.app == nil || s.app.Window == nil {
		return 0, false
	}
	if window := s.app.Window.Current(); window != nil {
		if hwnd, ok := asHWND(window.NativeWindow()); ok {
			return hwnd, true
		}
	}
	for _, window := range s.app.Window.GetAll() {
		if hwnd, ok := asHWND(window.NativeWindow()); ok {
			return hwnd, true
		}
	}
	return 0, false
}

func asHWND(nativeWindow unsafe.Pointer) (win32.HWND, bool) {
	if nativeWindow == nil {
		return 0, false
	}
	hwnd := win32.HWND(uintptr(nativeWindow))
	if hwnd == 0 {
		return 0, false
	}
	return hwnd, true
}

func (s *windowsController) registerBinding(accelerator string, action func()) {
	s.accelerators = append(s.accelerators, accelerator)
	s.app.KeyBinding.Add(accelerator, func(_ application.Window) {
		action()
	})
}

func (s *windowsController) registerMediaKeyBindings() {
	if s.app == nil || s.session == nil || len(s.accelerators) > 0 {
		return
	}

	s.registerBinding(acceleratorMediaPlayPause, func() {
		if _, err := s.session.TogglePlayback(context.Background()); err != nil {
			log.Printf("platform media key toggle failed: %v", err)
		}
	})
	s.registerBinding(acceleratorMediaNextTrack, func() {
		if _, err := s.session.Next(context.Background()); err != nil {
			log.Printf("platform media key next failed: %v", err)
		}
	})
	s.registerBinding(acceleratorMediaPrevTrack, func() {
		if _, err := s.session.Previous(context.Background()); err != nil {
			log.Printf("platform media key previous failed: %v", err)
		}
	})
}

func (s *windowsController) unregisterMediaKeyBindings() {
	if s.app == nil || len(s.accelerators) == 0 {
		return
	}
	for _, accelerator := range s.accelerators {
		s.app.KeyBinding.Remove(accelerator)
	}
	s.accelerators = nil
}
