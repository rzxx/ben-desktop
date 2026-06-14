//go:build windows

package winruntimeupdater

import (
	"context"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type runtimeSplash struct {
	app     *application.App
	window  *application.WebviewWindow
	actions chan string

	mu      sync.Mutex
	cancels []func()
}

type splashPayload struct {
	Title       string `json:"title"`
	Message     string `json:"message,omitempty"`
	Progress    int    `json:"progress"`
	Mode        string `json:"mode"`
	PrimaryText string `json:"primaryText,omitempty"`
	SkipText    string `json:"skipText,omitempty"`
}

func newSplash(app *application.App) *runtimeSplash {
	s := &runtimeSplash{
		app:     app,
		actions: make(chan string, 1),
	}
	s.cancels = append(s.cancels,
		app.Event.On(eventSkip, func(*application.CustomEvent) { s.action(eventSkip) }),
		app.Event.On(eventCancel, func(*application.CustomEvent) { s.action(eventCancel) }),
		app.Event.On(eventElevate, func(*application.CustomEvent) { s.action(eventElevate) }),
	)
	s.window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                 "runtime-updater",
		Title:                "ben-desktop media components",
		Width:                480,
		Height:               280,
		Frameless:            true,
		AlwaysOnTop:          true,
		DisableResize:        true,
		BackgroundType:       application.BackgroundTypeTransparent,
		HTML:                 splashHTML,
		AllowSimpleEventEmit: true,
	})
	s.window.Show()
	s.State("Checking media components...", 0)
	return s
}

func (s *runtimeSplash) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancels := s.cancels
	s.cancels = nil
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if s.window != nil {
		s.window.Close()
	}
}

func (s *runtimeSplash) State(title string, progress int) {
	s.emit(splashPayload{
		Title:    title,
		Progress: clampProgress(progress),
		Mode:     "working",
		SkipText: "Skip this update",
	})
}

func (s *runtimeSplash) Progress(title string, start int, end int, written int64, total int64) {
	progress := start
	if total > 0 {
		span := end - start
		if span < 0 {
			span = 0
		}
		progress = start + int(float64(span)*(float64(written)/float64(total)))
	}
	s.State(title, progress)
}

func (s *runtimeSplash) ElevationRequired() {
	s.emit(splashPayload{
		Title:       "Administrator permission required",
		Message:     "ben-desktop needs to update its media playback components. The current install location is protected, so administrator permission is required. You can skip this update, but playback may not work correctly.",
		Progress:    0,
		Mode:        "elevation",
		PrimaryText: "Update with administrator",
		SkipText:    "Skip this update",
	})
}

func (s *runtimeSplash) Error(title string, message string, canContinue bool) {
	mode := "error"
	primary := "Cancel"
	if canContinue {
		mode = "continue"
		primary = "Continue"
	}
	s.emit(splashPayload{
		Title:       title,
		Message:     message,
		Progress:    100,
		Mode:        mode,
		PrimaryText: primary,
		SkipText:    "Skip this update",
	})
}

func (s *runtimeSplash) WaitForAction(ctx context.Context) (string, error) {
	select {
	case action := <-s.actions:
		return action, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *runtimeSplash) action(name string) {
	select {
	case s.actions <- name:
	default:
	}
}

func (s *runtimeSplash) emit(payload splashPayload) {
	if s == nil || s.window == nil {
		return
	}
	s.window.EmitEvent(eventState, payload)
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

const splashHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    :root {
      color-scheme: light dark;
      font-family: "Segoe UI", system-ui, sans-serif;
      background: transparent;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background: transparent;
      color: #101214;
    }
    .shell {
      width: 440px;
      border: 1px solid rgba(15, 23, 42, 0.14);
      border-radius: 8px;
      background: rgba(248, 250, 252, 0.96);
      box-shadow: 0 18px 48px rgba(15, 23, 42, 0.2);
      padding: 22px;
    }
    .label {
      margin: 0;
      color: #64748b;
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.18em;
      text-transform: uppercase;
    }
    h1 {
      margin: 10px 0 8px;
      font-size: 20px;
      line-height: 1.25;
      font-weight: 650;
    }
    p {
      margin: 0;
      color: #475569;
      font-size: 13px;
      line-height: 1.5;
    }
    .progress {
      margin-top: 18px;
      height: 8px;
      overflow: hidden;
      border-radius: 999px;
      background: rgba(100, 116, 139, 0.18);
    }
    .bar {
      height: 100%;
      width: 0%;
      border-radius: inherit;
      background: #0f172a;
      transition: width 160ms ease;
    }
    .actions {
      margin-top: 18px;
      display: flex;
      justify-content: flex-end;
      gap: 10px;
    }
    button {
      min-width: 0;
      border: 1px solid rgba(15, 23, 42, 0.2);
      border-radius: 6px;
      background: rgba(255, 255, 255, 0.78);
      color: #0f172a;
      padding: 8px 11px;
      font: inherit;
      font-size: 13px;
    }
    button.primary {
      border-color: #0f172a;
      background: #0f172a;
      color: white;
    }
    button[hidden] { display: none; }
    @media (prefers-color-scheme: dark) {
      body { color: #f8fafc; }
      .shell {
        border-color: rgba(255, 255, 255, 0.12);
        background: rgba(24, 24, 27, 0.96);
        box-shadow: 0 18px 48px rgba(0, 0, 0, 0.38);
      }
      .label { color: #94a3b8; }
      p { color: #cbd5e1; }
      .progress { background: rgba(255, 255, 255, 0.14); }
      .bar { background: #f8fafc; }
      button {
        border-color: rgba(255, 255, 255, 0.16);
        background: rgba(255, 255, 255, 0.06);
        color: #f8fafc;
      }
      button.primary {
        border-color: #f8fafc;
        background: #f8fafc;
        color: #09090b;
      }
    }
  </style>
</head>
<body>
  <main class="shell">
    <p class="label">Media runtime</p>
    <h1 id="title">Checking media components...</h1>
    <p id="message">Keeping playback components aligned with this release.</p>
    <div class="progress" aria-hidden="true"><div class="bar" id="bar"></div></div>
    <div class="actions">
      <button id="skip" type="button">Skip this update</button>
      <button id="primary" class="primary" type="button" hidden>Cancel</button>
    </div>
  </main>
  <script>
    (function () {
      var title = document.getElementById("title");
      var message = document.getElementById("message");
      var bar = document.getElementById("bar");
      var skip = document.getElementById("skip");
      var primary = document.getElementById("primary");
      function emit(name) {
        if (window.wails && window.wails.Events) {
          window.wails.Events.Emit(name);
        }
      }
      skip.addEventListener("click", function () { emit("` + eventSkip + `"); });
      primary.addEventListener("click", function () {
        if (primary.dataset.mode === "elevation") emit("` + eventElevate + `");
        else emit("` + eventCancel + `");
      });
      window.wails.Events.On("` + eventState + `", function (event) {
        var data = event.data || {};
        title.textContent = data.title || "Checking media components...";
        message.textContent = data.message || "Keeping playback components aligned with this release.";
        bar.style.width = Math.max(0, Math.min(100, data.progress || 0)) + "%";
        skip.textContent = data.skipText || "Skip this update";
        var showPrimary = data.mode === "elevation" || data.mode === "error" || data.mode === "continue";
        primary.hidden = !showPrimary;
        primary.textContent = data.primaryText || "Cancel";
        primary.dataset.mode = data.mode || "";
      });
    })();
  </script>
</body>
</html>`

func (p splashPayload) String() string {
	return fmt.Sprintf("%s %d%%", p.Title, p.Progress)
}
