//go:build windows

package winutils

import (
	"golang.org/x/sys/windows"
)

// ShellExecute calls the Windows ShellExecute API with the supplied verb,
// executable path and parameter string. It is used for operations that need
// elevation (verb "runas") or to launch a program in a way that drops back
// to the normal user integrity level (verb "open" via explorer.exe).
func ShellExecute(verb, file, params string) error {
	verbPtr, err := windows.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	filePtr, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var paramsPtr *uint16
	if params != "" {
		paramsPtr, err = windows.UTF16PtrFromString(params)
		if err != nil {
			return err
		}
	}
	return windows.ShellExecute(0, verbPtr, filePtr, paramsPtr, nil, windows.SW_SHOWNORMAL)
}
