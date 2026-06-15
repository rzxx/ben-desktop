//go:build windows

package winutils

import "os/exec"

// RelaunchNormally starts the program at path as a normal (non-elevated)
// process. On Windows the preferred route is through explorer.exe, which
// launches at the user's standard integrity level even if the caller is
// elevated.
func RelaunchNormally(path string) error {
	if err := exec.Command("explorer.exe", path).Start(); err == nil {
		return nil
	}
	return ShellExecute("open", path, "")
}
