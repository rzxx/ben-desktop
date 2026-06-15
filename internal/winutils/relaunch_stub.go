//go:build !windows

package winutils

import "errors"

// RelaunchNormally is a no-op stub on non-Windows platforms.
func RelaunchNormally(_ string) error {
	return errors.New("winutils: RelaunchNormally is only available on Windows")
}
