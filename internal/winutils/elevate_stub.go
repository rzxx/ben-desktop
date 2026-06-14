//go:build !windows

package winutils

// IsElevated always returns false on non-Windows platforms.
func IsElevated() bool {
	return false
}
