//go:build windows

package desktopcore

import (
	"os/exec"
	"syscall"
)

const windowsCreateNoWindow = 0x08000000

func configureBackgroundProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsCreateNoWindow,
	}
}
