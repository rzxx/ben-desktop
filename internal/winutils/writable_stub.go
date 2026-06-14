//go:build !windows

package winutils

// IsWritableDir is a stub on non-Windows platforms and always reports true.
func IsWritableDir(_ string) (bool, error) {
	return true, nil
}
