package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validateRuntimeLoadable(source string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}

	var validationErrs []error
	if err := validateLibraryLoad(source, "libmpv.dll"); err != nil {
		validationErrs = append(validationErrs, err)
	}
	for _, name := range []string{"ffmpeg.exe", "ffprobe.exe"} {
		path := filepath.Join(source, "ffmpeg", "bin", name)
		if err := validateProgramRuns(path); err != nil {
			validationErrs = append(validationErrs, err)
		}
	}
	return errors.Join(validationErrs...)
}

func validateLibraryLoad(dir string, name string) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return err
	}

	restorePath := withMinimalPath()
	defer restorePath()

	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	if err := setDLLDirectory(dirPtr); err != nil {
		return err
	}
	defer func() {
		_ = setDLLDirectory(nil)
	}()

	handle, err := windows.LoadLibrary(path)
	if err != nil {
		return fmt.Errorf("load %s with isolated DLL search path: %w", path, err)
	}
	if err := windows.FreeLibrary(handle); err != nil {
		return fmt.Errorf("free %s: %w", path, err)
	}
	return nil
}

func validateProgramRuns(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cmd := exec.Command(path, "-version")
	cmd.Dir = filepath.Dir(path)
	cmd.Env = minimalWindowsEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("%s -version failed: %w: %s", filepath.Base(path), err, text)
		}
		return fmt.Errorf("%s -version failed: %w", filepath.Base(path), err)
	}
	return nil
}

func setDLLDirectory(path *uint16) error {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("SetDllDirectoryW")
	ret, _, err := proc.Call(uintptr(unsafe.Pointer(path)))
	if ret == 0 {
		if err != windows.ERROR_SUCCESS {
			return err
		}
		return windows.GetLastError()
	}
	return nil
}

func withMinimalPath() func() {
	oldPath, hadPath := os.LookupEnv("PATH")
	_ = os.Setenv("PATH", minimalWindowsPath())
	return func() {
		if hadPath {
			_ = os.Setenv("PATH", oldPath)
			return
		}
		_ = os.Unsetenv("PATH")
	}
}

func minimalWindowsEnv() []string {
	systemRoot := windowsRoot()
	return []string{
		"PATH=" + minimalWindowsPath(),
		"SystemRoot=" + systemRoot,
		"WINDIR=" + systemRoot,
	}
}

func minimalWindowsPath() string {
	systemRoot := windowsRoot()
	return filepath.Join(systemRoot, "System32") + string(os.PathListSeparator) + systemRoot
}

func windowsRoot() string {
	if root := strings.TrimSpace(os.Getenv("SystemRoot")); root != "" {
		return root
	}
	return `C:\Windows`
}
