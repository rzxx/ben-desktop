//go:build windows

package platform

import (
	"fmt"

	"github.com/zzl/go-win32api/v2/win32"
)

const processAppUserModelID = "io.ben.player"

func initializeProcessIdentity() error {
	hr := win32.SetCurrentProcessExplicitAppUserModelID(win32.StrToPwstr(processAppUserModelID))
	if win32.FAILED(hr) {
		return fmt.Errorf("set process AppUserModelID: %s", win32.HRESULT_ToString(hr))
	}

	return nil
}
