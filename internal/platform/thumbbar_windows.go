//go:build windows

package platform

import (
	"context"
	"fmt"
	"log"
	"sync"
	"syscall"
	"unsafe"

	"ben/desktop/internal/playback"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/zzl/go-win32api/v2/win32"
)

const (
	thumbButtonPrevID      uint32  = 1001
	thumbButtonPlayPauseID uint32  = 1002
	thumbButtonNextID      uint32  = 1003
	thumbbarSubclassID     uintptr = 1
)

var (
	clsidTaskbarList = syscall.GUID{0x56FDF344, 0xFD6D, 0x11D0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}

	thumbbarSubclassProc = win32.SUBCLASSPROC(syscall.NewCallback(thumbbarWindowProc))

	servicesByWindowMu sync.RWMutex
	servicesByWindow   = map[win32.HWND]*thumbbarService{}
)

type thumbbarService struct {
	mu sync.Mutex

	session *playback.Session

	hwnd      win32.HWND
	taskbar   *win32.ITaskbarList3
	started   bool
	installed bool

	taskbarButtonCreatedMsg uint32

	hasLastState bool
	lastState    playback.SessionSnapshot

	lastIsPlaying bool
	lastHasQueue  bool
	lastHasTrack  bool
}

func newThumbbarService(session *playback.Session) *thumbbarService {
	return &thumbbarService{session: session}
}

func (s *thumbbarService) Start(hwnd win32.HWND) error {
	if hwnd == 0 {
		return fmt.Errorf("thumbnail toolbar requires a valid window handle")
	}

	return application.InvokeSyncWithError(func() error {
		s.mu.Lock()
		if s.started {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()

		var taskbar *win32.ITaskbarList3
		hr := win32.CoCreateInstance(
			&clsidTaskbarList,
			nil,
			win32.CLSCTX_INPROC_SERVER,
			&win32.IID_ITaskbarList3,
			unsafe.Pointer(&taskbar),
		)
		if win32.FAILED(hr) {
			return fmt.Errorf("create ITaskbarList3: %s", win32.HRESULT_ToString(hr))
		}
		if taskbar == nil {
			return fmt.Errorf("create ITaskbarList3: returned nil")
		}

		hr = taskbar.HrInit()
		if win32.FAILED(hr) {
			taskbar.Release()
			return fmt.Errorf("taskbar HrInit: %s", win32.HRESULT_ToString(hr))
		}

		taskbarButtonCreatedMsg, _ := win32.RegisterWindowMessage(win32.StrToPwstr("TaskbarButtonCreated"))

		servicesByWindowMu.Lock()
		servicesByWindow[hwnd] = s
		servicesByWindowMu.Unlock()

		if win32.SetWindowSubclass(hwnd, thumbbarSubclassProc, thumbbarSubclassID, 0) == 0 {
			servicesByWindowMu.Lock()
			delete(servicesByWindow, hwnd)
			servicesByWindowMu.Unlock()
			taskbar.Release()
			return fmt.Errorf("set window subclass for thumbnail toolbar")
		}

		s.mu.Lock()
		s.hwnd = hwnd
		s.taskbar = taskbar
		s.installed = true
		s.started = true
		s.taskbarButtonCreatedMsg = taskbarButtonCreatedMsg
		hasState := s.hasLastState
		state := s.lastState
		s.mu.Unlock()

		if err := s.addButtons(); err != nil {
			log.Printf("thumbnail toolbar add buttons failed: %v", err)
		}
		if hasState {
			s.applyState(state)
		}
		return nil
	})
}

func (s *thumbbarService) Close() error {
	return application.InvokeSyncWithError(func() error {
		s.mu.Lock()
		hwnd := s.hwnd
		taskbar := s.taskbar
		installed := s.installed
		s.started = false
		s.installed = false
		s.taskbar = nil
		s.hwnd = 0
		s.mu.Unlock()

		if hwnd != 0 {
			servicesByWindowMu.Lock()
			delete(servicesByWindow, hwnd)
			servicesByWindowMu.Unlock()
		}
		if installed && hwnd != 0 {
			win32.RemoveWindowSubclass(hwnd, thumbbarSubclassProc, thumbbarSubclassID)
		}
		if taskbar != nil {
			taskbar.Release()
		}
		return nil
	})
}

func (s *thumbbarService) UpdatePlaybackSnapshot(snapshot playback.SessionSnapshot) {
	s.mu.Lock()
	s.lastState = snapshot
	s.hasLastState = true
	started := s.started
	s.mu.Unlock()
	if !started {
		return
	}

	application.InvokeAsync(func() {
		s.applyState(snapshot)
	})
}

func (s *thumbbarService) addButtons() error {
	s.mu.Lock()
	taskbar := s.taskbar
	hwnd := s.hwnd
	started := s.started
	s.mu.Unlock()

	if !started || taskbar == nil || hwnd == 0 {
		return nil
	}

	prevIcon, _ := win32.LoadIcon(0, win32.IDI_HAND)
	playIcon, _ := win32.LoadIcon(0, win32.IDI_APPLICATION)
	nextIcon, _ := win32.LoadIcon(0, win32.IDI_INFORMATION)

	buttons := []win32.THUMBBUTTON{
		newThumbButton(thumbButtonPrevID, prevIcon, "Previous", true),
		newThumbButton(thumbButtonPlayPauseID, playIcon, "Play", true),
		newThumbButton(thumbButtonNextID, nextIcon, "Next", true),
	}

	hr := taskbar.ThumbBarAddButtons(hwnd, uint32(len(buttons)), &buttons[0])
	if win32.FAILED(hr) {
		return fmt.Errorf("ThumbBarAddButtons: %s", win32.HRESULT_ToString(hr))
	}
	return nil
}

func (s *thumbbarService) applyState(snapshot playback.SessionSnapshot) {
	s.mu.Lock()
	taskbar := s.taskbar
	hwnd := s.hwnd
	started := s.started
	s.mu.Unlock()
	if !started || taskbar == nil || hwnd == 0 {
		return
	}

	hasQueue := snapshot.QueueLength > 0
	hasTrack := snapshot.CurrentItem != nil
	isPlaying := snapshot.Status == playback.StatusPlaying

	s.mu.Lock()
	changed := isPlaying != s.lastIsPlaying || hasQueue != s.lastHasQueue || hasTrack != s.lastHasTrack
	if changed {
		s.lastIsPlaying = isPlaying
		s.lastHasQueue = hasQueue
		s.lastHasTrack = hasTrack
	}
	s.mu.Unlock()

	if !changed {
		return
	}

	prevIcon, _ := win32.LoadIcon(0, win32.IDI_HAND)
	playIcon, _ := win32.LoadIcon(0, win32.IDI_APPLICATION)
	pauseIcon, _ := win32.LoadIcon(0, win32.IDI_ASTERISK)
	nextIcon, _ := win32.LoadIcon(0, win32.IDI_INFORMATION)

	playPauseIcon := playIcon
	playPauseTip := "Play"
	if isPlaying {
		playPauseIcon = pauseIcon
		playPauseTip = "Pause"
	}

	buttons := []win32.THUMBBUTTON{
		newThumbButton(thumbButtonPrevID, prevIcon, "Previous", hasTrack),
		newThumbButton(thumbButtonPlayPauseID, playPauseIcon, playPauseTip, hasQueue),
		newThumbButton(thumbButtonNextID, nextIcon, "Next", hasQueue),
	}

	hr := taskbar.ThumbBarUpdateButtons(hwnd, uint32(len(buttons)), &buttons[0])
	if win32.FAILED(hr) {
		log.Printf("thumbnail toolbar update failed: %s", win32.HRESULT_ToString(hr))
	}
}

func (s *thumbbarService) handleWindowMessage(hwnd win32.HWND, msg uint32, wParam win32.WPARAM, lParam win32.LPARAM) win32.LRESULT {
	if msg == win32.WM_COMMAND {
		notifyCode := uint32(win32.HIWORD(uint32(wParam)))
		if notifyCode == win32.THBN_CLICKED {
			buttonID := uint32(win32.LOWORD(uint32(wParam)))
			s.handleThumbbarButton(buttonID)
			return 0
		}
	}

	s.mu.Lock()
	taskbarButtonCreatedMsg := s.taskbarButtonCreatedMsg
	hasState := s.hasLastState
	state := s.lastState
	s.mu.Unlock()

	if taskbarButtonCreatedMsg != 0 && msg == taskbarButtonCreatedMsg {
		if err := s.addButtons(); err != nil {
			log.Printf("thumbnail toolbar re-add buttons failed: %v", err)
		}
		if hasState {
			s.applyState(state)
		}
	}

	return win32.DefSubclassProc(hwnd, msg, wParam, lParam)
}

func (s *thumbbarService) handleThumbbarButton(buttonID uint32) {
	if s.session == nil {
		return
	}

	switch buttonID {
	case thumbButtonPrevID:
		go s.runAction("previous", func() error {
			_, err := s.session.Previous(context.Background())
			return err
		})
	case thumbButtonPlayPauseID:
		go s.runAction("toggle", func() error {
			_, err := s.session.TogglePlayback(context.Background())
			return err
		})
	case thumbButtonNextID:
		go s.runAction("next", func() error {
			_, err := s.session.Next(context.Background())
			return err
		})
	}
}

func (s *thumbbarService) runAction(name string, action func() error) {
	if err := action(); err != nil {
		log.Printf("thumbnail toolbar %s action failed: %v", name, err)
	}
}

func newThumbButton(id uint32, icon win32.HICON, tooltip string, enabled bool) win32.THUMBBUTTON {
	button := win32.THUMBBUTTON{
		DwMask: win32.THB_ICON | win32.THB_TOOLTIP | win32.THB_FLAGS,
		IId:    id,
		HIcon:  icon,
		DwFlags: func() win32.THUMBBUTTONFLAGS {
			if enabled {
				return win32.THBF_ENABLED
			}
			return win32.THBF_DISABLED
		}(),
	}
	copyTooltip(&button.SzTip, tooltip)
	return button
}

func copyTooltip(dst *[260]uint16, text string) {
	if dst == nil {
		return
	}

	utf16Data, err := syscall.UTF16FromString(text)
	if err != nil {
		utf16Data = nil
	}
	if len(utf16Data) > len(dst)-1 {
		utf16Data = utf16Data[:len(dst)-1]
	}

	copy(dst[:], utf16Data)
	if len(utf16Data) < len(dst) {
		dst[len(utf16Data)] = 0
	}
}

func thumbbarWindowProc(
	hwnd win32.HWND,
	msg uint32,
	wParam win32.WPARAM,
	lParam win32.LPARAM,
	uIdSubclass uintptr,
	dwRefData uintptr,
) win32.LRESULT {
	_ = uIdSubclass
	_ = dwRefData

	servicesByWindowMu.RLock()
	service := servicesByWindow[hwnd]
	servicesByWindowMu.RUnlock()

	if service == nil {
		return win32.DefSubclassProc(hwnd, msg, wParam, lParam)
	}
	return service.handleWindowMessage(hwnd, msg, wParam, lParam)
}
