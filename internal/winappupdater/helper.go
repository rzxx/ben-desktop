//go:build windows

// Package winappupdater handles the Wails updater's helper-mode swap on
// Windows when the application is installed in a protected directory such as
// Program Files. The built-in Wails helper runs non-elevated and cannot write
// its backup file there; this package detects that situation, requests UAC
// elevation, performs the swap from the elevated helper, and then relaunches
// the application at the normal integrity level.
package winappupdater

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ben/desktop/internal/winutils"
)

const (
	envHelperMode   = "WAILS_UPDATER_HELPER"
	envHelperTarget = "WAILS_UPDATER_HELPER_TARGET"
	envHelperNew    = "WAILS_UPDATER_HELPER_NEW"
	envHelperPID    = "WAILS_UPDATER_HELPER_PID"
	envHelperLog    = "WAILS_UPDATER_HELPER_LOG"
)

// MaybeHandle intercepts Wails updater helper mode. If the current process was
// not spawned as a helper it returns immediately. If the install directory is
// writable it also returns, allowing the built-in Wails helper to run. For
// protected directories it either requests elevation (when non-elevated) or
// performs the binary swap and relaunch (when elevated) and then terminates
// the process.
func MaybeHandle(logger *slog.Logger) {
	if os.Getenv(envHelperMode) != "1" {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	target := os.Getenv(envHelperTarget)
	newPath := os.Getenv(envHelperNew)
	if target == "" || newPath == "" {
		logger.Error("app update helper missing target or new path")
		os.Exit(2)
	}

	pidStr := os.Getenv(envHelperPID)
	logPath := os.Getenv(envHelperLog)
	lg := newHelperLogger(logPath)
	defer lg.Close()

	installDir := filepath.Dir(target)
	writable, err := winutils.IsWritableDir(installDir)
	if err != nil {
		lg.logf("cannot check install dir %s: %v", installDir, err)
		if !winutils.IsElevated() {
			relaunchElevated(lg)
		}
		os.Exit(22)
	}
	if writable {
		lg.logf("install dir is writable; letting Wails updater handle the swap")
		return
	}

	if !winutils.IsElevated() {
		lg.logf("install dir not writable; requesting elevation")
		relaunchElevated(lg)
	}

	lg.logf("elevated app update helper start: target=%s new=%s pid=%s", target, newPath, pidStr)
	pid, _ := strconv.Atoi(pidStr)
	if pid > 0 {
		if err := waitForPID(pid, 30*time.Second); err != nil {
			lg.logf("parent did not exit within timeout: %v", err)
			os.Exit(17)
		}
	}

	backupPath, err := backupTarget(target)
	if err != nil {
		lg.logf("backup failed: %v", err)
		os.Exit(12)
	}
	lg.logf("backed up %s -> %s", target, backupPath)

	swapped := false
	for i := 0; i < 20; i++ {
		if err := replaceTarget(target, newPath); err != nil {
			lg.logf("replace attempt %d: %v", i+1, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		swapped = true
		lg.logf("swap succeeded on attempt %d", i+1)
		break
	}

	if !swapped {
		lg.logf("all swap attempts exhausted; restoring backup")
		if err := copyFile(backupPath, target); err != nil {
			lg.logf("restore failed: %v", err)
			os.Exit(14)
		}
		_ = winutils.RelaunchNormally(target)
		os.Exit(13)
	}

	clearHelperEnv()

	if err := winutils.RelaunchNormally(target); err != nil {
		lg.logf("relaunch failed: %v; restoring backup", err)
		if err := copyFile(backupPath, target); err != nil {
			lg.logf("restore failed: %v", err)
			os.Exit(16)
		}
		_ = winutils.RelaunchNormally(target)
		os.Exit(15)
	}

	stagingDir := filepath.Dir(newPath)
	if strings.HasPrefix(filepath.Base(stagingDir), "wails-update-") {
		_ = os.RemoveAll(stagingDir)
	}
	_ = os.Remove(backupPath)

	lg.logf("helper done")
	os.Exit(0)
}

func relaunchElevated(lg *helperLogger) {
	exe, err := os.Executable()
	if err != nil {
		lg.logf("cannot resolve executable: %v", err)
		os.Exit(20)
	}
	lg.logf("relaunching elevated: %s", exe)
	if err := winutils.ShellExecute("runas", exe, ""); err != nil {
		lg.logf("elevation relaunch failed: %v", err)
		os.Exit(20)
	}
	os.Exit(0)
}

func backupTarget(target string) (string, error) {
	dir, err := backupDir()
	if err != nil {
		return "", err
	}
	backup := filepath.Join(dir, filepath.Base(target)+".bak")
	_ = os.Remove(backup)
	if err := copyFile(target, backup); err != nil {
		return "", err
	}
	return backup, nil
}

func backupDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "ben-desktop", "update-backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func replaceTarget(target, newPath string) error {
	if err := os.RemoveAll(target); err == nil {
		return os.Rename(newPath, target)
	}
	aside := fmt.Sprintf("%s.old.%d", target, time.Now().UnixNano())
	if err := os.Rename(target, aside); err != nil {
		return fmt.Errorf("rename-aside %s -> %s: %w", target, aside, err)
	}
	if err := os.Rename(newPath, target); err != nil {
		_ = os.Rename(aside, target)
		return err
	}
	_ = os.Remove(aside)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func clearHelperEnv() {
	for _, k := range []string{envHelperMode, envHelperTarget, envHelperNew, envHelperPID, envHelperLog} {
		_ = os.Unsetenv(k)
	}
}

func waitForPID(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("pid %d still alive after %s", pid, timeout)
}

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

func isAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

type helperLogger struct {
	w    io.Writer
	file *os.File
}

func newHelperLogger(path string) *helperLogger {
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("ben-app-update-%d.log", os.Getpid()))
	}
	f, err := os.Create(path)
	if err != nil {
		return &helperLogger{w: os.Stderr}
	}
	return &helperLogger{w: io.MultiWriter(os.Stderr, f), file: f}
}

func (h *helperLogger) logf(format string, args ...any) {
	if h == nil || h.w == nil {
		return
	}
	_, _ = fmt.Fprintf(h.w, "%s: %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func (h *helperLogger) Close() {
	if h != nil && h.file != nil {
		_ = h.file.Close()
	}
}
