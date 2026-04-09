//go:build windows

package platform

import (
	"strings"
	"unsafe"

	apitypes "ben/desktop/api/types"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/zzl/go-win32api/v2/win32"
)

func CurrentSystemTheme() apitypes.ResolvedTheme {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return apitypes.ResolvedThemeDark
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return apitypes.ResolvedThemeDark
	}
	if value != 0 {
		return apitypes.ResolvedThemeLight
	}
	return apitypes.ResolvedThemeDark
}

func IsSystemThemeChangeMessage(msg uint32, lParam win32.LPARAM) bool {
	switch msg {
	case win32.WM_THEMECHANGED, win32.WM_SYSCOLORCHANGE, win32.WM_DWMCOLORIZATIONCOLORCHANGED:
		return true
	case win32.WM_SETTINGCHANGE:
		if lParam == 0 {
			return true
		}
		setting := strings.TrimSpace(windows.UTF16PtrToString((*uint16)(unsafe.Pointer(lParam))))
		return setting == "" || strings.EqualFold(setting, "ImmersiveColorSet")
	default:
		return false
	}
}
