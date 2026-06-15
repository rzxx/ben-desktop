//go:build windows

package winutils

import "golang.org/x/sys/windows"

// IsElevated reports whether the current process token has a high integrity
// level (i.e. is running as Administrator on Windows).
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
