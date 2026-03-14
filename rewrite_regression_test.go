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
		sharingPage, err := os.ReadFile(filepath.Join("frontend", "src", "routes", "sharing", "page.tsx"))
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

	t.Run("corehost does not fall back to app runtimes", func(t *testing.T) {
		corehost, err := os.ReadFile("corehost.go")
		if err != nil {
			t.Fatalf("read corehost.go: %v", err)
		}
		if strings.Contains(string(corehost), "return h.App") {
			t.Fatalf("corehost still falls back to h.App")
		}
	})

	t.Run("blocking long running APIs stay removed", func(t *testing.T) {
		type fileCheck struct {
			path      string
			disallows []string
		}

		checks := []fileCheck{
			{
				path: filepath.Join("internal", "desktopcore", "runtime.go"),
				disallows: []string{
					"\tRescanNow(ctx context.Context)",
					"\tRescanRoot(ctx context.Context",
					"\tSyncNow(ctx context.Context)",
					"\tConnectPeer(ctx context.Context",
					"\tPublishCheckpoint(ctx context.Context)",
					"\tCompactCheckpoint(ctx context.Context",
					"\tFinalizeJoinSession(ctx context.Context",
					"\tEnsureRecordingEncoding(ctx context.Context",
					"\tEnsureAlbumEncodings(ctx context.Context",
					"\tEnsurePlaylistEncodings(ctx context.Context",
					"\tPreparePlaybackRecording(ctx context.Context",
				},
			},
			{
				path: "facades.go",
				disallows: []string{
					"func (s *LibraryFacade) RescanNow(",
					"func (s *LibraryFacade) RescanRoot(",
					"func (s *NetworkFacade) SyncNow(",
					"func (s *NetworkFacade) ConnectPeer(",
					"func (s *NetworkFacade) PublishCheckpoint(",
					"func (s *NetworkFacade) CompactCheckpoint(",
					"func (s *InviteFacade) FinalizeJoinSession(",
					"func (s *PlaybackFacade) EnsureRecordingEncoding(",
					"func (s *PlaybackFacade) EnsureAlbumEncodings(",
					"func (s *PlaybackFacade) EnsurePlaylistEncodings(",
					"func (s *PlaybackFacade) PreparePlaybackRecording(",
				},
			},
			{
				path: filepath.Join("internal", "desktopcore", "unavailable.go"),
				disallows: []string{
					"func (c *UnavailableCore) RescanNow(",
					"func (c *UnavailableCore) RescanRoot(",
					"func (c *UnavailableCore) SyncNow(",
					"func (c *UnavailableCore) ConnectPeer(",
					"func (c *UnavailableCore) PublishCheckpoint(",
					"func (c *UnavailableCore) CompactCheckpoint(",
					"func (c *UnavailableCore) FinalizeJoinSession(",
					"func (c *UnavailableCore) EnsureRecordingEncoding(",
					"func (c *UnavailableCore) EnsureAlbumEncodings(",
					"func (c *UnavailableCore) EnsurePlaylistEncodings(",
					"func (c *UnavailableCore) PreparePlaybackRecording(",
				},
			},
			{
				path: filepath.Join("frontend", "src", "shared", "lib", "desktop.ts"),
				disallows: []string{
					"export function connectPeer(",
				},
			},
			{
				path: filepath.Join("frontend", "bindings", "ben", "desktop", "libraryfacade.ts"),
				disallows: []string{
					"export function RescanNow",
					"export function RescanRoot",
				},
			},
			{
				path: filepath.Join("frontend", "bindings", "ben", "desktop", "networkfacade.ts"),
				disallows: []string{
					"export function SyncNow",
					"export function ConnectPeer",
					"export function PublishCheckpoint",
					"export function CompactCheckpoint",
				},
			},
			{
				path: filepath.Join("frontend", "bindings", "ben", "desktop", "invitefacade.ts"),
				disallows: []string{
					"export function FinalizeJoinSession",
				},
			},
			{
				path: filepath.Join("frontend", "bindings", "ben", "desktop", "playbackfacade.ts"),
				disallows: []string{
					"export function EnsureRecordingEncoding",
					"export function EnsureAlbumEncodings",
					"export function EnsurePlaylistEncodings",
					"export function PreparePlaybackRecording",
				},
			},
		}

		for _, check := range checks {
			raw, err := os.ReadFile(check.path)
			if err != nil {
				t.Fatalf("read %s: %v", check.path, err)
			}
			text := string(raw)
			for _, disallowed := range check.disallows {
				if strings.Contains(text, disallowed) {
					t.Fatalf("%s still contains %q", check.path, disallowed)
				}
			}
		}
	})

	t.Run("extracted service files do not keep app receivers", func(t *testing.T) {
		paths := []string{
			filepath.Join("internal", "desktopcore", "service_operator.go"),
			filepath.Join("internal", "desktopcore", "service_checkpoint.go"),
			filepath.Join("internal", "desktopcore", "service_sync.go"),
			filepath.Join("internal", "desktopcore", "service_transport.go"),
			filepath.Join("internal", "desktopcore", "membership_auth.go"),
			filepath.Join("internal", "desktopcore", "membership_runtime.go"),
			filepath.Join("internal", "desktopcore", "watcher.go"),
		}

		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if strings.Contains(string(raw), "func (a *App)") {
				t.Fatalf("%s still defines App receiver methods", path)
			}
		}
	})

	t.Run("active runtime owns watcher and transport state", func(t *testing.T) {
		checks := []struct {
			path      string
			disallows []string
		}{
			{
				path: filepath.Join("internal", "desktopcore", "app.go"),
				disallows: []string{
					"watcherMu",
					"scanWatcher   *activeScanWatcher",
				},
			},
			{
				path: filepath.Join("internal", "desktopcore", "service_transport.go"),
				disallows: []string{
					"current *activeTransportRuntime",
					"func (s *TransportService) setCurrent(",
					"func (s *TransportService) clearCurrent(",
				},
			},
		}

		for _, check := range checks {
			raw, err := os.ReadFile(check.path)
			if err != nil {
				t.Fatalf("read %s: %v", check.path, err)
			}
			text := string(raw)
			for _, disallowed := range check.disallows {
				if strings.Contains(text, disallowed) {
					t.Fatalf("%s still contains %q", check.path, disallowed)
				}
			}
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
