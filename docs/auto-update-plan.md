# Auto-update plan

This document describes the two-stage plan for adding self-updates to ben-desktop.

- **Stage 1** ships updates for the main application binary only. Runtime dependency changes (ffmpeg, libmpv, codec DLLs) require a full reinstall.
- **Stage 2** adds a Windows-only runtime updater with a small Wails splash window that keeps the implementation out of `main.go`.

Stage 2 must not start until Stage 1 is fully working on Windows, macOS, and Linux.

---

## Stage 1 — Basic Wails self-updates

Goal: the application can check, download, verify, and swap its own binary from GitHub Releases.

### 1. Upgrade Wails v3

- `go get github.com/wailsapp/wails/v3@latest` so `app.Updater` is available.
- Bump `@wailsio/runtime` in `frontend/` to the matching alpha.
- Regenerate bindings and run the full check suite (`go test ./...`, `bun typecheck`, `bun run test:run`, etc.).

### 2. Inject version and build metadata

- Add `-ldflags` values for `appVersion`, `buildCommit`, and `buildTime`.
- Use the git tag as the source of truth in CI (e.g. `v0.1.0` becomes `"0.1.0"`).
- Pass the version to both the Wails updater config and the observability manager.

### 3. Set up updater signing

- Generate an Ed25519 keypair.
- Embed `updater-public-key.pem` in the binary with `//go:embed`.
- Store the private key as a GitHub secret for the release workflow.

### 4. Wire `app.Updater` in `main.go`

- Configure the GitHub Releases provider pointing at `yourorg/ben-desktop`.
- Enable signature verification with the embedded public key.
- For the first iteration, use the built-in updater window.

### 5. Add an update trigger

- Either a background check on startup or a "Check for updates" button in Settings.
- Support skip / remind later.

### 6. Build the release workflow

Use a CI matrix for Windows, macOS, and Linux.

Produce Wails-updater-compatible assets:

- macOS: `ben-desktop-darwin-universal.zip` containing the signed `.app` bundle.
- Windows: `ben-desktop-windows-amd64.exe` single binary.
- Linux: `ben-desktop-linux-amd64` single binary.

Also keep building first-install packages (NSIS installer, AppImage, `.dmg`).

Attach `SHA256SUMS` and signature files to the GitHub Release.

### 7. Test the swap

- Verify `CheckAndInstall` → `Restart` replaces the binary on each OS.
- Confirm macOS code-signing and Gatekeeper survive the swap.
- Confirm Windows can rename the running `.exe` aside and relaunch.

---

## Stage 2 — Windows runtime updater with a Wails splash window

Goal: update ffmpeg, libmpv, codec DLLs, and license files automatically on Windows without forcing a full reinstall. macOS and Linux are unaffected.

### Design overview

The runtime updater is implemented in a Windows-only package. `main.go` calls a single function after `application.New()` but before the main window is created. Because `PlaybackService.ServiceStartup` runs at `app.Run()`, `libmpv.dll` is not yet loaded, so the files can be replaced safely.

If the install directory is not writable, the same binary relaunches itself in an elevated "runtime updater mode", shows the splash window, updates the files, then relaunches the app normally and exits.

```text
main.go
  ├── app := application.New(...)
  ├── if windows: winruntimeupdater.RunIfNeeded(app)
  │       ├── check if runtime update is needed
  │       ├── if writable: show Wails splash window → download/verify/install → close → return
  │       └── if not writable: explain UAC → relaunch same .exe with --runtime-update-elevated → exit
  ├── app.Window.NewWithOptions(...)   // main window
  └── app.Run()
```

Elevated mode:

```text
ben-desktop.exe --runtime-update-elevated
  ├── create minimal Wails app
  ├── show the same splash window
  ├── download/verify/install runtime files
  ├── relaunch ben-desktop.exe normally (unelevated)
  └── exit
```

There is no separate helper executable. The same binary runs in a special mode only when elevation is required.

### 1. Define the runtime release format

- Add `build/windows/runtime/version.txt` with a monotonic runtime version.
- CI produces two extra assets:
  - `ben-desktop-runtime-windows-amd64.zip` containing the runtime tree.
  - `runtime-manifest.json` listing every file, relative path, and SHA-256 hash.
- Sign both assets with the updater key.

### 2. Create the Windows-only package

- `internal/winruntimeupdater/updater.go` with `//go:build windows`.
- A no-op stub for non-Windows platforms so `main.go` can call it unconditionally.
- Expose `RunIfNeeded(app *application.App) error`.
- Keep all UI, HTTP, hash, and UAC logic out of `main.go`.

### 3. Build the Wails splash window

Use `application.WebviewWindowOptions`:

- `Frameless: true`
- `AlwaysOnTop: true`
- `BackgroundType: application.BackgroundTypeTransparent`
- `HTML: splashHTML` with inline HTML/CSS/JS so no frontend build is required

The splash shows:

- "Checking media components..."
- "Downloading media components..."
- "Verifying downloaded files..."
- "Installing media components..."
- "Starting ben-desktop..."

Include a progress bar and a "Skip this update" / "Cancel" action.

### 4. Implement the sync logic

- Read local `runtime/version.txt`.
- Fetch `runtime-manifest.json` for the same release the current binary came from.
- If the remote runtime version is newer:
  - Download `ben-desktop-runtime-windows-amd64.zip` to a temp directory.
  - Verify every extracted file against the manifest hashes.
  - Replace files next to `ben-desktop.exe`.
  - Write the new `runtime/version.txt`.
- Surface network and verification errors in the splash UI.

### 5. Implement UAC handling

- Before writing, check whether the install directory is writable.
- If writable: proceed without any prompt.
- If not writable:
  - Show an explanation in the splash window:
    > ben-desktop needs to update its media playback components. The current install location is protected, so administrator permission is required. You can skip this update, but playback may not work correctly.
  - Buttons: **"Update with administrator"** / **"Skip this update"**.
  - If accepted:
    - Get the current executable path with `os.Executable()`.
    - Launch it with `ShellExecuteW` using the `runas` verb and the `--runtime-update-elevated` flag.
    - Close the current Wails app and exit.
  - If skipped:
    - Close the splash and return to normal startup, logging a warning.

### 6. Implement the elevated mode

- In `main.go`, parse arguments very early:
  ```go
  if runtime.GOOS == "windows" && slices.Contains(os.Args, "--runtime-update-elevated") {
      return winruntimeupdater.RunElevated()
  }
  ```
- `RunElevated()` creates a minimal Wails app, shows the splash window, performs the update with administrator rights, then relaunches the same executable normally (without the flag and without `runas`) and exits.
- The application never keeps running as administrator.

### 7. Wire it into normal startup

In `main.go`:

```go
app := application.New(...)
if runtime.GOOS == "windows" {
    if err := winruntimeupdater.RunIfNeeded(app); err != nil {
        // log and decide whether to continue or exit
    }
}
// ... create main window, app.Run()
```

### 8. Update CI

- Build and sign the runtime bundle and manifest as part of the Windows release job.
- Attach them to the GitHub Release alongside the main application assets.

### 9. Test

- Clean install: runtime installs on first run.
- Outdated runtime: update happens before the main window appears.
- `%LOCALAPPDATA%` install: no UAC prompt.
- `C:\Program Files` install: UAC prompt appears; update succeeds if accepted, app continues if skipped.
- Corrupt download: verification fails, files are not modified, and an error is shown.

---

## Ordering rule

Stage 2 must not begin until Stage 1 is complete and tested on all three platforms. The runtime updater depends on the versioned release pipeline, signed assets, and Wails window basics that Stage 1 establishes.
