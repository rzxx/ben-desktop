//go:build windows

package winruntimeupdater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ben/desktop/internal/appupdate"
	"ben/desktop/internal/buildinfo"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

const (
	elevatedFlag = "--runtime-update-elevated"

	manifestAssetName = "runtime-manifest.json"
	runtimeZipName    = "ben-desktop-runtime-windows-amd64.zip"

	eventState   = "ben:runtime-update:state"
	eventSkip    = "ben:runtime-update:skip"
	eventCancel  = "ben:runtime-update:cancel"
	eventElevate = "ben:runtime-update:elevate"
)

type runtimeManifest struct {
	SchemaVersion  int                   `json:"schemaVersion"`
	Version        string                `json:"version"`
	RuntimeVersion string                `json:"runtimeVersion,omitempty"`
	Platform       string                `json:"platform,omitempty"`
	Arch           string                `json:"arch,omitempty"`
	Asset          string                `json:"asset"`
	AssetURL       string                `json:"assetUrl,omitempty"`
	AssetSHA256    string                `json:"assetSha256,omitempty"`
	Files          []runtimeManifestFile `json:"files"`
	GeneratedAt    string                `json:"generatedAt,omitempty"`
}

type runtimeManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type runtimePlan struct {
	installDir string
	manifest   runtimeManifest
	zipURL     string
}

func IsElevatedRuntimeUpdate(args []string) bool {
	for _, arg := range args {
		if arg == elevatedFlag {
			return true
		}
	}
	return false
}

func RunIfNeeded(app *application.App, continueStartup func()) error {
	if app == nil {
		callContinue(continueStartup)
		return errors.New("winruntimeupdater: application is nil")
	}
	if !runtimeUpdatesEnabled() {
		callContinue(continueStartup)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	plan, err := checkForUpdate(ctx)
	cancel()
	if err != nil {
		callContinue(continueStartup)
		return err
	}
	if plan == nil {
		callContinue(continueStartup)
		return nil
	}

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go runNormalUpdate(app, plan, continueStartup)
	})
	return nil
}

func RunElevated() error {
	logger := slog.Default()
	app := application.New(application.Options{
		Name:        "ben-desktop runtime updater",
		Description: "Updates ben-desktop media playback components",
		Logger:      logger,
		LogLevel:    slog.LevelInfo,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			exitCode := 0
			defer func() {
				relaunchErr := relaunchNormal()
				if relaunchErr != nil {
					logger.Error("runtime updater relaunch failed", slog.Any("error", relaunchErr), slog.String("service", "runtime-updater"))
					exitCode = 1
				}
				app.Quit()
				os.Exit(exitCode)
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			plan, err := checkForUpdate(ctx)
			if err != nil || plan == nil {
				if err != nil {
					logger.Error("runtime update check failed", slog.Any("error", err), slog.String("service", "runtime-updater"))
				}
				return
			}
			splash := newSplash(app)
			defer splash.Close()
			if err := installRuntimeWithSkip(ctx, plan, splash); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				logger.Error("runtime update failed", slog.Any("error", err), slog.String("service", "runtime-updater"))
				splash.Error("Media component update failed", err.Error(), true)
				_, _ = splash.WaitForAction(ctx)
			}
		}()
	})
	return app.Run()
}

func runNormalUpdate(app *application.App, plan *runtimePlan, continueStartup func()) {
	var once sync.Once
	continueOnce := func() {
		once.Do(func() {
			callContinue(continueStartup)
		})
	}
	splash := newSplash(app)
	defer splash.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	writable, err := installDirWritable(plan.installDir)
	if err != nil {
		splash.Error("Media component update failed", err.Error(), true)
		_, _ = splash.WaitForAction(ctx)
		continueOnce()
		return
	}
	if !writable {
		splash.ElevationRequired()
		action, waitErr := splash.WaitForAction(ctx)
		if waitErr != nil || action != eventElevate {
			continueOnce()
			return
		}
		if err := launchElevated(); err != nil {
			splash.Error("Administrator launch failed", err.Error(), true)
			_, _ = splash.WaitForAction(ctx)
			continueOnce()
			return
		}
		app.Quit()
		os.Exit(0)
	}

	if err := installRuntimeWithSkip(ctx, plan, splash); err != nil {
		if errors.Is(err, context.Canceled) {
			continueOnce()
			return
		}
		splash.Error("Media component update failed", err.Error(), true)
		_, _ = splash.WaitForAction(ctx)
		continueOnce()
		return
	}
	continueOnce()
}

func installRuntimeWithSkip(ctx context.Context, plan *runtimePlan, splash *runtimeSplash) error {
	installCtx, cancelInstall := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			action, err := splash.WaitForAction(installCtx)
			if err != nil {
				return
			}
			if action == eventSkip || action == eventCancel {
				cancelInstall()
				return
			}
		}
	}()
	err := installRuntime(installCtx, plan, splash)
	cancelInstall()
	<-done
	return err
}

func checkForUpdate(ctx context.Context) (*runtimePlan, error) {
	installDir, err := currentInstallDir()
	if err != nil {
		return nil, err
	}
	localVersion := readLocalRuntimeVersion(installDir)
	body, err := fetchBytes(ctx, manifestURL(), true)
	if err != nil {
		return nil, err
	}
	if err := verifyManifestSignature(ctx, body); err != nil {
		return nil, err
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("winruntimeupdater: decode manifest: %w", err)
	}
	remoteVersion := manifest.runtimeVersion()
	if remoteVersion == "" {
		return nil, errors.New("winruntimeupdater: runtime manifest missing version")
	}
	if compareRuntimeVersion(remoteVersion, localVersion) <= 0 {
		return nil, nil
	}
	if strings.TrimSpace(manifest.Asset) == "" {
		manifest.Asset = runtimeZipName
	}
	zipURL := strings.TrimSpace(manifest.AssetURL)
	if zipURL == "" {
		zipURL = releaseDownloadURL(manifest.Asset)
	}
	return &runtimePlan{installDir: installDir, manifest: manifest, zipURL: zipURL}, nil
}

func installRuntime(ctx context.Context, plan *runtimePlan, splash *runtimeSplash) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	splash.State("Downloading media components...", 0)
	tempDir, err := os.MkdirTemp("", "ben-runtime-update-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	zipPath := filepath.Join(tempDir, "runtime.zip")
	if err := downloadFile(ctx, plan.zipURL, zipPath, plan.manifest.AssetSHA256, splash); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	extractDir := filepath.Join(tempDir, "extract")
	splash.State("Verifying downloaded files...", 66)
	if err := extractZip(zipPath, extractDir); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyExtractedManifest(extractDir, plan.manifest); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	splash.State("Installing media components...", 84)
	if err := replaceRuntimeFiles(extractDir, plan.installDir, plan.manifest); err != nil {
		return err
	}
	versionPath := filepath.Join(plan.installDir, "runtime", "version.txt")
	if err := os.MkdirAll(filepath.Dir(versionPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(versionPath, []byte(plan.manifest.runtimeVersion()+"\n"), 0o644); err != nil {
		return err
	}
	splash.State("Starting ben-desktop...", 100)
	return nil
}

func downloadFile(ctx context.Context, sourceURL string, dest string, expectedSHA256 string, splash *runtimeSplash) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download runtime zip: HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	hash := sha256.New()
	total := resp.ContentLength
	written := int64(0)
	lastUpdate := time.Now()
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, err := out.Write(chunk); err != nil {
				return err
			}
			if _, err := hash.Write(chunk); err != nil {
				return err
			}
			written += int64(n)
			if time.Since(lastUpdate) > 100*time.Millisecond {
				splash.Progress("Downloading media components...", 8, 62, written, total)
				lastUpdate = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := out.Sync(); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if expected := strings.TrimSpace(expectedSHA256); expected != "" && !strings.EqualFold(actual, expected) {
		return fmt.Errorf("runtime zip SHA-256 mismatch: got %s want %s", actual, expected)
	}
	splash.Progress("Downloading media components...", 8, 62, written, total)
	return nil
}

func verifyManifestSignature(ctx context.Context, manifestBody []byte) error {
	publicKey := appupdate.PublicKeyPEM()
	if len(publicKey) == 0 {
		return errors.New("winruntimeupdater: updater public key is empty")
	}
	body, err := fetchBytes(ctx, manifestURL()+".sig", true)
	if err != nil {
		return err
	}
	signature, err := appupdate.DecodeSignature(body)
	if err != nil {
		return err
	}
	return appupdate.VerifyEd25519DigestSignature(manifestBody, signature)
}

func extractZip(zipPath string, destDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = reader.Close()
	}()
	for _, file := range reader.File {
		rel, err := cleanManifestPath(file.Name)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, rel)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
	}
	return nil
}

func verifyExtractedManifest(extractDir string, manifest runtimeManifest) error {
	if len(manifest.Files) == 0 {
		return errors.New("runtime manifest has no files")
	}
	for _, entry := range manifest.Files {
		rel, err := cleanManifestPath(entry.Path)
		if err != nil {
			return err
		}
		path := filepath.Join(extractDir, rel)
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if !strings.EqualFold(sum, strings.TrimSpace(entry.SHA256)) {
			return fmt.Errorf("runtime file SHA-256 mismatch for %s", entry.Path)
		}
	}
	return nil
}

func replaceRuntimeFiles(extractDir string, installDir string, manifest runtimeManifest) error {
	for _, entry := range manifest.Files {
		rel, err := cleanManifestPath(entry.Path)
		if err != nil {
			return err
		}
		src := filepath.Join(extractDir, rel)
		dst := filepath.Join(installDir, rel)
		if err := replaceFile(src, dst); err != nil {
			return fmt.Errorf("replace %s: %w", entry.Path, err)
		}
	}
	return nil
}

func replaceFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), ".ben-update-"+filepath.Base(dst)+".tmp")
	backup := filepath.Join(filepath.Dir(dst), ".ben-update-"+filepath.Base(dst)+".old")
	_ = os.Remove(tmp)
	_ = os.Remove(backup)
	if err := copyFile(src, tmp); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, backup); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(backup, dst)
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func cleanManifestPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("empty runtime manifest path")
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return "", fmt.Errorf("unsafe runtime manifest path %q", path)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe runtime manifest path %q", path)
	}
	return clean, nil
}

func installDirWritable(dir string) (bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	probe, err := os.CreateTemp(dir, ".ben-runtime-write-test-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return false, nil
		}
		return false, err
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return false, closeErr
	}
	if removeErr != nil {
		return false, removeErr
	}
	return true, nil
}

func readLocalRuntimeVersion(installDir string) string {
	body, err := os.ReadFile(filepath.Join(installDir, "runtime", "version.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func currentInstallDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("BEN_DESKTOP_RUNTIME_INSTALL_DIR")); dir != "" {
		return dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func runtimeUpdatesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BEN_DESKTOP_RUNTIME_UPDATE"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	if strings.TrimSpace(os.Getenv("BEN_DESKTOP_RUNTIME_MANIFEST_URL")) != "" {
		return true
	}
	return buildinfo.IsReleaseVersion()
}

func manifestURL() string {
	if override := strings.TrimSpace(os.Getenv("BEN_DESKTOP_RUNTIME_MANIFEST_URL")); override != "" {
		return override
	}
	return releaseDownloadURL(manifestAssetName)
}

func releaseDownloadURL(asset string) string {
	escapedAsset := url.PathEscape(asset)
	return "https://github.com/" + buildinfo.Repository() + "/releases/download/" + url.PathEscape(buildinfo.ReleaseTag()) + "/" + escapedAsset
}

func fetchBytes(ctx context.Context, sourceURL string, auth bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	if auth {
		if token := githubToken(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("runtime update asset not found: %s", sourceURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: HTTP %d", sourceURL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
}

func githubToken() string {
	if token := strings.TrimSpace(os.Getenv("BEN_DESKTOP_GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func compareRuntimeVersion(remote string, local string) int {
	remote = strings.TrimSpace(remote)
	local = strings.TrimSpace(local)
	if remote == local {
		return 0
	}
	if local == "" {
		return 1
	}
	rparts := versionParts(remote)
	lparts := versionParts(local)
	max := len(rparts)
	if len(lparts) > max {
		max = len(lparts)
	}
	for i := 0; i < max; i++ {
		r := 0
		l := 0
		if i < len(rparts) {
			r = rparts[i]
		}
		if i < len(lparts) {
			l = lparts[i]
		}
		if r > l {
			return 1
		}
		if r < l {
			return -1
		}
	}
	return 0
}

func versionParts(version string) []int {
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r < '0' || r > '9'
	})
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		value := 0
		for _, r := range field {
			value = value*10 + int(r-'0')
		}
		parts = append(parts, value)
	}
	return parts
}

func launchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return shellExecute("runas", exe, elevatedFlag)
}

func relaunchNormal() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := exec.Command("explorer.exe", exe).Start(); err == nil {
		return nil
	}
	return shellExecute("open", exe, "")
}

func shellExecute(verb string, exe string, params string) error {
	verbPtr, err := windows.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	var paramsPtr *uint16
	if strings.TrimSpace(params) != "" {
		paramsPtr, err = windows.UTF16PtrFromString(params)
		if err != nil {
			return err
		}
	}
	err = windows.ShellExecute(0, verbPtr, exePtr, paramsPtr, nil, windows.SW_SHOWNORMAL)
	if err != nil {
		return err
	}
	return nil
}

func (m runtimeManifest) runtimeVersion() string {
	if strings.TrimSpace(m.RuntimeVersion) != "" {
		return strings.TrimSpace(m.RuntimeVersion)
	}
	return strings.TrimSpace(m.Version)
}

func callContinue(continueStartup func()) {
	if continueStartup != nil {
		continueStartup()
	}
}
