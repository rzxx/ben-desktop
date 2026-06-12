package desktopcore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMediaRuntimePrefersExplicitPath(t *testing.T) {
	restore := stubMediaRuntime(t, filepath.Join(t.TempDir(), "app.exe"), nil, nil)
	defer restore()

	ffmpeg := filepath.Join("C:", "tools", "ffmpeg.exe")
	ffprobe := filepath.Join("C:", "tools", "ffprobe.exe")
	paths := resolveMediaRuntimePaths(ffmpeg, ffprobe)
	if paths.FFmpegPath != ffmpeg {
		t.Fatalf("FFmpegPath = %q, want %q", paths.FFmpegPath, ffmpeg)
	}
	if paths.FFprobePath != ffprobe {
		t.Fatalf("FFprobePath = %q, want %q", paths.FFprobePath, ffprobe)
	}
	if paths.Source != "configured" {
		t.Fatalf("Source = %q, want configured", paths.Source)
	}
}

func TestResolveMediaRuntimeUsesPackagedRuntime(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ben-desktop.exe")
	bin := filepath.Join(root, "runtime", "ffmpeg", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir packaged ffmpeg dir: %v", err)
	}
	writeEmptyFile(t, filepath.Join(bin, mediaRuntimeBinaryName("ffmpeg")))
	writeEmptyFile(t, filepath.Join(bin, mediaRuntimeBinaryName("ffprobe")))
	restore := stubMediaRuntime(t, exe, nil, nil)
	defer restore()

	paths := resolveMediaRuntimePaths("", "")
	if paths.Source != "packaged" {
		t.Fatalf("Source = %q, want packaged", paths.Source)
	}
	if paths.FFmpegPath != filepath.Join(bin, mediaRuntimeBinaryName("ffmpeg")) {
		t.Fatalf("FFmpegPath = %q", paths.FFmpegPath)
	}
	if paths.FFprobePath != filepath.Join(bin, mediaRuntimeBinaryName("ffprobe")) {
		t.Fatalf("FFprobePath = %q", paths.FFprobePath)
	}
}

func TestResolveMediaRuntimeFallsBackToPathNames(t *testing.T) {
	restore := stubMediaRuntime(t, filepath.Join(t.TempDir(), "app.exe"), nil, nil)
	defer restore()

	paths := resolveMediaRuntimePaths("", "")
	if paths.FFmpegPath != "ffmpeg" {
		t.Fatalf("FFmpegPath = %q, want ffmpeg", paths.FFmpegPath)
	}
	if paths.FFprobePath != "ffprobe" {
		t.Fatalf("FFprobePath = %q, want ffprobe", paths.FFprobePath)
	}
}

func TestResolveMediaRuntimePrefersEnvOverConfigured(t *testing.T) {
	env := map[string]string{
		envFFmpegPath:  "/env/ffmpeg",
		envFFprobePath: "/env/ffprobe",
	}
	restore := stubMediaRuntime(t, filepath.Join(t.TempDir(), "app.exe"), env, nil)
	defer restore()

	ffmpeg := filepath.Join("C:", "tools", "ffmpeg.exe")
	ffprobe := filepath.Join("C:", "tools", "ffprobe.exe")
	paths := resolveMediaRuntimePaths(ffmpeg, ffprobe)
	if paths.FFmpegPath != "/env/ffmpeg" {
		t.Fatalf("FFmpegPath = %q, want %q", paths.FFmpegPath, "/env/ffmpeg")
	}
	if paths.FFprobePath != "/env/ffprobe" {
		t.Fatalf("FFprobePath = %q, want %q", paths.FFprobePath, "/env/ffprobe")
	}
	if paths.Source != "environment" {
		t.Fatalf("Source = %q, want environment", paths.Source)
	}
}

func TestResolveMediaRuntimeUsesLookPathResults(t *testing.T) {
	lookPath := map[string]string{
		"ffmpeg":  "/usr/bin/ffmpeg",
		"ffprobe": "/usr/bin/ffprobe",
	}
	restore := stubMediaRuntime(t, filepath.Join(t.TempDir(), "app.exe"), nil, lookPath)
	defer restore()

	paths := resolveMediaRuntimePaths("", "")
	if paths.FFmpegPath != "/usr/bin/ffmpeg" {
		t.Fatalf("FFmpegPath = %q, want %q", paths.FFmpegPath, "/usr/bin/ffmpeg")
	}
	if paths.FFprobePath != "/usr/bin/ffprobe" {
		t.Fatalf("FFprobePath = %q, want %q", paths.FFprobePath, "/usr/bin/ffprobe")
	}
	if paths.Source != "path" {
		t.Fatalf("Source = %q, want path", paths.Source)
	}
}

func stubMediaRuntime(t *testing.T, exePath string, env map[string]string, lookPath map[string]string) func() {
	t.Helper()
	oldExecutable := mediaRuntimeExecutable
	oldLookPath := mediaRuntimeLookPath
	oldGetenv := mediaRuntimeGetenv
	mediaRuntimeExecutable = func() (string, error) { return exePath, nil }
	mediaRuntimeLookPath = func(file string) (string, error) {
		if value := lookPath[file]; value != "" {
			return value, nil
		}
		return "", os.ErrNotExist
	}
	mediaRuntimeGetenv = func(key string) string { return env[key] }
	return func() {
		mediaRuntimeExecutable = oldExecutable
		mediaRuntimeLookPath = oldLookPath
		mediaRuntimeGetenv = oldGetenv
	}
}

func writeEmptyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
