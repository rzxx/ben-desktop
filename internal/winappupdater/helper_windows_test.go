//go:build windows

package winappupdater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestReplaceTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "app.exe")
	newPath := filepath.Join(dir, "new.exe")

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}

	if err := replaceTarget(target, newPath); err != nil {
		t.Fatalf("replaceTarget failed: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("target was not replaced: %q", got)
	}

	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new path should have been removed/moved: %v", err)
	}
}

func TestClearHelperEnv(t *testing.T) {
	for _, k := range []string{envHelperMode, envHelperTarget, envHelperNew, envHelperPID, envHelperLog} {
		t.Setenv(k, "x")
	}
	clearHelperEnv()
	for _, k := range []string{envHelperMode, envHelperTarget, envHelperNew, envHelperPID, envHelperLog} {
		if os.Getenv(k) != "" {
			t.Fatalf("environment variable %s was not cleared", k)
		}
	}
}
