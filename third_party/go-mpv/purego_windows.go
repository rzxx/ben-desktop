//go:build (!cgo || nocgo) && windows

package mpv

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const (
	libname = "libmpv.dll"
)

// loadLibrary loads the dll.
func loadLibrary() (uintptr, error) {
	handle, err := windows.LoadLibrary(libname)
	if err != nil {
		return 0, fmt.Errorf("cannot load library %s: %w", libname, err)
	}

	return uintptr(handle), nil
}
