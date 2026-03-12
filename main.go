package main

import (
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/playback"
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[desktopcore.JobSnapshot](desktopcore.EventJobSnapshotChanged)
	application.RegisterEvent[playback.SessionSnapshot](playback.EventSnapshotChanged)
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	host := newCoreHost()
	playbackService := NewPlaybackServiceWithHost(host)
	app := application.New(application.Options{
		Name:        "ben-desktop",
		Description: "Desktop host for ben playback and core services",
		Services: []application.Service{
			application.NewService(NewLibraryFacade(host)),
			application.NewService(NewNetworkFacade(host)),
			application.NewService(NewJobsFacade(host)),
			application.NewService(NewCatalogFacade(host)),
			application.NewService(NewInviteFacade(host)),
			application.NewService(NewPlaybackFacade(host)),
			application.NewService(NewCacheFacade(host)),
			application.NewService(playbackService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "ben",
		Frameless: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
