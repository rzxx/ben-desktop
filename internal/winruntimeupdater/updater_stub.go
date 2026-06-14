//go:build !windows

package winruntimeupdater

import "github.com/wailsapp/wails/v3/pkg/application"

func IsElevatedRuntimeUpdate([]string) bool {
	return false
}

func RunElevated() error {
	return nil
}

func RunIfNeeded(_ *application.App, continueStartup func()) error {
	if continueStartup != nil {
		continueStartup()
	}
	return nil
}
