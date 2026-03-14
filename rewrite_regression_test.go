package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopRewriteRegression(t *testing.T) {
	t.Run("no desktop imports from ben core", func(t *testing.T) {
		var violations []string
		err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch filepath.Base(path) {
				case ".git", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".ts", ".tsx":
			default:
				return nil
			}
			if filepath.Base(path) == "rewrite_regression_test.go" {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(raw), "ben/core/") {
				violations = append(violations, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk repo: %v", err)
		}
		if len(violations) > 0 {
			t.Fatalf("found legacy ben/core imports in %v", violations)
		}
	})

	t.Run("legacy runtime names removed", func(t *testing.T) {
		paths := []string{
			"facades.go",
			"corehost.go",
			filepath.Join("internal", "desktopcore"),
			filepath.Join("internal", "playback"),
			"playbackservice.go",
		}
		for _, path := range paths {
			err := filepath.Walk(path, func(current string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() || filepath.Ext(current) != ".go" {
					return nil
				}
				raw, readErr := os.ReadFile(current)
				if readErr != nil {
					return readErr
				}
				text := string(raw)
				if strings.Contains(text, "desktopcore.Runtime") {
					t.Fatalf("found legacy desktopcore.Runtime reference in %s", current)
				}
				if strings.Contains(text, "CorePlaybackBridge") {
					t.Fatalf("found legacy CorePlaybackBridge reference in %s", current)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", path, err)
			}
		}
	})

	t.Run("sharing page uses async connect peer", func(t *testing.T) {
		sharingPage, err := os.ReadFile(filepath.Join("frontend", "src", "features", "sharing", "page.tsx"))
		if err != nil {
			t.Fatalf("read sharing page: %v", err)
		}
		text := string(sharingPage)
		if !strings.Contains(text, "startConnectPeer(") {
			t.Fatalf("sharing page does not call startConnectPeer")
		}
		if strings.Contains(text, "await connectPeer(") {
			t.Fatalf("sharing page still calls blocking connectPeer")
		}

		binding, err := os.ReadFile(filepath.Join("frontend", "bindings", "ben", "desktop", "networkfacade.ts"))
		if err != nil {
			t.Fatalf("read network facade binding: %v", err)
		}
		if !strings.Contains(string(binding), "export function StartConnectPeer") {
			t.Fatalf("network facade binding is missing StartConnectPeer")
		}
	})

	t.Run("legacy corebridge package removed", func(t *testing.T) {
		entries, err := os.ReadDir(filepath.Join("internal", "corebridge"))
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatalf("read internal/corebridge: %v", err)
		}
		if len(entries) > 0 {
			t.Fatalf("internal/corebridge still contains files")
		}
	})
}
