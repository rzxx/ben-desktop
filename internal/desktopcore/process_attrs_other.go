//go:build !windows

package desktopcore

import "os/exec"

func configureBackgroundProcess(cmd *exec.Cmd) {
}
