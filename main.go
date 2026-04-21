package main

import (
	apitypes "ben/desktop/api/types"
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
	application.RegisterEvent[apitypes.CatalogChangeEvent](desktopcore.EventCatalogChanged)
	application.RegisterEvent[apitypes.PinChangeEvent](desktopcore.EventPinChanged)
	application.RegisterEvent[apitypes.NotificationSnapshot](EventNotificationChanged)
	application.RegisterEvent[apitypes.ThemePreferences](EventThemePreferencesChanged)
	application.RegisterEvent[playback.TransportEventSnapshot](playback.EventTransportChanged)
	application.RegisterEvent[playback.QueueEventSnapshot](playback.EventQueueChanged)
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	host := newCoreHost()
	playbackService := NewPlaybackServiceWithHost(host)
	notificationsFacade := NewNotificationsFacade(host, playbackService)
	windowBackground := initialWindowBackgroundColour()
	app := application.New(application.Options{
		Name:        "ben-desktop",
		Description: "Desktop host for ben playback and core services",
		Services: []application.Service{
			application.NewServiceWithOptions(NewArtworkHTTPService(host), application.ServiceOptions{
				Route: artworkServiceRoute,
			}),
			application.NewService(NewLibraryFacade(host)),
			application.NewService(NewNetworkFacade(host)),
			application.NewService(NewJobsFacade(host)),
			application.NewService(NewCatalogFacade(host)),
			application.NewService(NewPinFacade(host)),
			application.NewService(NewInviteFacade(host)),
			application.NewService(NewPlaybackFacade(host)),
			application.NewService(NewThemeFacade(host)),
			application.NewService(NewCacheFacade(host)),
			application.NewService(playbackService),
			application.NewService(notificationsFacade),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     appWindowBaseTitle,
		Frameless: true,
		MinWidth:  1280,
		MinHeight: 720,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: windowBackground,
		URL:              "/",
	})
	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func initialWindowBackgroundColour() application.RGBA {
	mode := apitypes.AppThemeModeSystem
	if state, err := loadSettingsState(); err == nil {
		mode = apitypes.NormalizeAppThemeMode(apitypes.AppThemeMode(state.Theme.Mode))
	}

	if apitypes.ResolveTheme(mode, detectSystemTheme()) == apitypes.ResolvedThemeDark {
		return application.NewRGB(10, 10, 10)
	}
	return application.NewRGB(247, 248, 250)
}
