//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/platform"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/zzl/go-win32api/v2/win32"
)

const themeFacadeSubclassID uintptr = 2

var (
	themeFacadeSubclassProc = win32.SUBCLASSPROC(syscall.NewCallback(themeFacadeWindowProc))

	themeFacadesByWindowMu sync.RWMutex
	themeFacadesByWindow   = map[win32.HWND]*ThemeFacade{}
)

func detectSystemTheme() apitypes.ResolvedTheme {
	return platform.CurrentSystemTheme()
}

func (s *ThemeFacade) startSystemThemeMonitor() {
	s.mu.Lock()
	app := s.app
	if app == nil || app.Window == nil {
		s.stopThemeMonitoring = nil
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	tracked := map[win32.HWND]struct{}{}
	var trackedMu sync.Mutex

	trackWindow := func(hwnd win32.HWND) {
		if hwnd == 0 {
			return
		}
		trackedMu.Lock()
		tracked[hwnd] = struct{}{}
		trackedMu.Unlock()
	}

	attachToWindow := func(window application.Window) bool {
		if window == nil {
			return false
		}
		hwnd, ok := themeFacadeAsHWND(window.NativeWindow())
		if !ok {
			return false
		}

		if err := application.InvokeSyncWithError(func() error {
			themeFacadesByWindowMu.Lock()
			if existing := themeFacadesByWindow[hwnd]; existing == s {
				themeFacadesByWindowMu.Unlock()
				return nil
			}
			themeFacadesByWindow[hwnd] = s
			themeFacadesByWindowMu.Unlock()

			if win32.SetWindowSubclass(hwnd, themeFacadeSubclassProc, themeFacadeSubclassID, 0) == 0 {
				themeFacadesByWindowMu.Lock()
				delete(themeFacadesByWindow, hwnd)
				themeFacadesByWindowMu.Unlock()
				return syscall.EINVAL
			}
			return nil
		}); err != nil {
			return false
		}

		trackWindow(hwnd)
		return true
	}

	watchWindow := func(window application.Window) {
		if attachToWindow(window) {
			return
		}

		var cancel func()
		cancel = window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(_ *application.WindowEvent) {
			if !attachToWindow(window) {
				return
			}
			if cancel != nil {
				cancel()
				cancel = nil
			}
		})
	}

	for _, window := range app.Window.GetAll() {
		watchWindow(window)
	}
	app.Window.OnCreate(func(window application.Window) {
		watchWindow(window)
	})

	stop := func() {
		_ = application.InvokeSyncWithError(func() error {
			trackedMu.Lock()
			defer trackedMu.Unlock()
			for hwnd := range tracked {
				themeFacadesByWindowMu.Lock()
				delete(themeFacadesByWindow, hwnd)
				themeFacadesByWindowMu.Unlock()
				win32.RemoveWindowSubclass(hwnd, themeFacadeSubclassProc, themeFacadeSubclassID)
			}
			return nil
		})
	}

	s.mu.Lock()
	s.stopThemeMonitoring = stop
	s.mu.Unlock()
}

func themeFacadeWindowProc(
	hwnd win32.HWND,
	msg uint32,
	wParam win32.WPARAM,
	lParam win32.LPARAM,
	uIdSubclass uintptr,
	dwRefData uintptr,
) win32.LRESULT {
	_ = uIdSubclass
	_ = dwRefData

	if platform.IsSystemThemeChangeMessage(msg, lParam) {
		themeFacadesByWindowMu.RLock()
		service := themeFacadesByWindow[hwnd]
		themeFacadesByWindowMu.RUnlock()
		if service != nil {
			go service.refreshSystemTheme()
		}
	}

	return win32.DefSubclassProc(hwnd, msg, wParam, lParam)
}

func themeFacadeAsHWND(nativeWindow unsafe.Pointer) (win32.HWND, bool) {
	if nativeWindow == nil {
		return 0, false
	}
	hwnd := win32.HWND(uintptr(nativeWindow))
	if hwnd == 0 {
		return 0, false
	}
	return hwnd, true
}
