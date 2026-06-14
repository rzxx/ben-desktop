package main

import (
	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/appupdate"
	"ben/desktop/internal/buildinfo"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/observability"
	"ben/desktop/internal/playback"
	"ben/desktop/internal/winruntimeupdater"
	"embed"
	"log/slog"
	"os"
	"sync"

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
	if winruntimeupdater.IsElevatedRuntimeUpdate(os.Args) {
		if err := winruntimeupdater.RunElevated(); err != nil {
			slog.Default().Error("runtime updater failed", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}

	build := buildinfo.Current()
	obsManager, logger, err := observability.Initialize(observability.Config{
		AppName:     "ben-desktop",
		AppVersion:  build.AppVersion,
		BuildCommit: build.BuildCommit,
		BuildTime:   build.BuildTime,
		LogLevel:    initialObservabilityLogLevel(),
	})
	if err != nil {
		logger = slog.Default()
		logger.Error("observability initialization failed", slog.Any("error", err))
	}

	host := newCoreHost()
	playbackService := NewPlaybackServiceWithHost(host)
	notificationsFacade := NewNotificationsFacade(host, playbackService)
	updateFacade := NewAppUpdateFacade(build)
	windowBackground := initialWindowBackgroundColour()
	app := application.New(application.Options{
		Name:        "ben-desktop",
		Description: "Desktop host for ben playback and core services",
		Logger:      logger,
		LogLevel:    initialObservabilityLogLevel(),
		Services: []application.Service{
			application.NewServiceWithOptions(NewArtworkHTTPService(host), application.ServiceOptions{
				Route: artworkServiceRoute,
			}),
			application.NewService(NewObservabilityFacade(obsManager)),
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
			application.NewService(updateFacade),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	updateRunner, err := appupdate.Configure(app, logger)
	if err != nil {
		logger.Error("app updater configuration failed", slog.Any("error", err), slog.String("service", "appupdate"))
	}
	updateFacade.bindRunner(updateRunner)
	installApplicationMenu(app, updateRunner)

	var mainWindowOnce sync.Once
	createMainWindow := func() {
		mainWindowOnce.Do(func() {
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
		})
	}
	if err := winruntimeupdater.RunIfNeeded(app, createMainWindow); err != nil {
		logger.Error("runtime updater startup check failed", slog.Any("error", err), slog.String("service", "runtime-updater"))
	}
	err = app.Run()
	if err != nil {
		logger.Error("application run failed", slog.Any("error", err), slog.String("service", "app"))
		os.Exit(1)
	}
}

func initialObservabilityLogLevel() slog.Level {
	if state, err := loadSettingsState(); err == nil {
		if level, parseErr := parseSlogLevel(state.Observability.LogLevel); parseErr == nil {
			return level
		}
	}
	return slog.LevelInfo
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
