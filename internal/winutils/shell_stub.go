//go:build !windows

package winutils

import "errors"

// ShellExecute is a no-op stub on non-Windows platforms.
func ShellExecute(_, _, _ string) error {
	return errors.New("winutils: ShellExecute is only available on Windows")
}
