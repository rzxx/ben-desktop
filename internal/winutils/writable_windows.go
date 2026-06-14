//go:build windows

package winutils

import "os"

// IsWritableDir reports whether the caller can create and remove a temporary
// file inside dir. It creates the directory if it does not exist.
func IsWritableDir(dir string) (bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	probe, err := os.CreateTemp(dir, ".ben-write-test-*")
	if err != nil {
		if os.IsPermission(err) {
			return false, nil
		}
		return false, err
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return false, closeErr
	}
	if removeErr != nil {
		return false, removeErr
	}
	return true, nil
}
