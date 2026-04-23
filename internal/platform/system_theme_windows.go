//go:build windows

package platform

import (
	"unsafe"

	apitypes "ben/desktop/api/types"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/zzl/go-win32api/v2/win32"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procLstrcmpiW        = kernel32.NewProc("lstrcmpiW")
	procLstrlenW         = kernel32.NewProc("lstrlenW")
	immersiveColorSetPtr = windows.StringToUTF16Ptr("ImmersiveColorSet")
)

func CurrentSystemTheme() apitypes.ResolvedTheme {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return apitypes.ResolvedThemeDark
	}
	defer func() { _ = key.Close() }()

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
		return settingChangeNameEmpty(lParam) || settingChangeNameEqual(lParam, immersiveColorSetPtr)
	default:
		return false
	}
}

func settingChangeNameEmpty(lParam win32.LPARAM) bool {
	length, _, _ := procLstrlenW.Call(lParam)
	return length == 0
}

func settingChangeNameEqual(lParam win32.LPARAM, name *uint16) bool {
	result, _, _ := procLstrcmpiW.Call(lParam, uintptr(unsafe.Pointer(name)))
	return result == 0
}
