package desktopcore

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	envFFmpegPath  = "BEN_DESKTOP_FFMPEG"
	envFFprobePath = "BEN_DESKTOP_FFPROBE"
)

type mediaRuntimePaths struct {
	FFmpegPath  string
	FFprobePath string
	Source      string
}

var (
	mediaRuntimeExecutable = os.Executable
	mediaRuntimeLookPath   = exec.LookPath
	mediaRuntimeGetenv     = os.Getenv
)

func resolveMediaRuntimePaths(preferredFFmpegPath, preferredFFprobePath string) mediaRuntimePaths {
	preferredFFmpegPath = strings.TrimSpace(preferredFFmpegPath)
	preferredFFprobePath = strings.TrimSpace(preferredFFprobePath)

	envFFmpeg := strings.TrimSpace(mediaRuntimeGetenv(envFFmpegPath))
	envFFprobe := strings.TrimSpace(mediaRuntimeGetenv(envFFprobePath))

	ffmpegPath := firstNonEmpty(envFFmpeg, preferredFFmpegPath)
	ffprobePath := firstNonEmpty(envFFprobe, preferredFFprobePath)

	source := "path"
	if envFFmpeg != "" || envFFprobe != "" {
		source = "environment"
	} else if preferredFFmpegPath != "" || preferredFFprobePath != "" {
		source = "configured"
	}

	if ffmpegPath == "" {
		if path, ok := findPackagedBinary("ffmpeg"); ok {
			ffmpegPath = path
			if source == "path" {
				source = "packaged"
			}
		} else {
			if found, err := mediaRuntimeLookPath("ffmpeg"); err == nil && strings.TrimSpace(found) != "" {
				ffmpegPath = found
			} else {
				ffmpegPath = "ffmpeg"
			}
		}
	}
	if ffprobePath == "" {
		if path, ok := findPackagedBinary("ffprobe"); ok {
			ffprobePath = path
			if source == "path" {
				source = "packaged"
			}
		} else {
			if found, err := mediaRuntimeLookPath("ffprobe"); err == nil && strings.TrimSpace(found) != "" {
				ffprobePath = found
			} else {
				ffprobePath = "ffprobe"
			}
		}
	}

	return completeMediaRuntimePaths(ffmpegPath, ffprobePath, source)
}

func findPackagedBinary(name string) (string, bool) {
	binName := mediaRuntimeBinaryName(name)
	for _, dir := range packagedFFmpegBinDirs() {
		path := filepath.Join(dir, binName)
		if fileExists(path) {
			return path, true
		}
	}
	return "", false
}

func packagedFFmpegBinDirs() []string {
	exePath, err := mediaRuntimeExecutable()
	if err != nil {
		return nil
	}
	exeDir := filepath.Dir(exePath)
	dirs := []string{
		filepath.Join(exeDir, "runtime", "ffmpeg", "bin"),
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, filepath.Clean(filepath.Join(exeDir, "..", "Resources", "runtime", "ffmpeg", "bin")))
	}
	return dirs
}

func mediaRuntimeBinaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func completeMediaRuntimePaths(ffmpegPath, ffprobePath, source string) mediaRuntimePaths {
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	ffprobePath = strings.TrimSpace(ffprobePath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if ffprobePath == "" {
		ffprobePath = companionBinaryPath(ffmpegPath, "ffmpeg", "ffprobe")
	}
	return mediaRuntimePaths{
		FFmpegPath:  ffmpegPath,
		FFprobePath: ffprobePath,
		Source:      strings.TrimSpace(source),
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
