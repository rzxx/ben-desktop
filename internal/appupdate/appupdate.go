package appupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"ben/desktop/internal/buildinfo"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

//go:embed updater-public-key.pem
var embedded embed.FS

func PublicKeyPEM() []byte {
	publicKey, err := embedded.ReadFile("updater-public-key.pem")
	if err != nil {
		return nil
	}
	return bytes.TrimSpace(publicKey)
}

func VerifyEd25519DigestSignature(payload []byte, signature []byte) error {
	publicKey, err := parseEd25519PublicKey(PublicKeyPEM())
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	if !ed25519.Verify(publicKey, digest[:], signature) {
		return errors.New("appupdate: Ed25519 signature did not verify")
	}
	return nil
}

type CheckRunner struct {
	app    *application.App
	logger *slog.Logger

	mu      sync.Mutex
	running bool
}

func Configure(app *application.App, logger *slog.Logger) (*CheckRunner, error) {
	if app == nil {
		return nil, errors.New("appupdate: application is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	publicKey := PublicKeyPEM()
	token := os.Getenv("BEN_DESKTOP_GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	provider, err := NewSignedGitHubProvider(SignedGitHubConfig{
		Repository:       buildinfo.Repository(),
		Token:            token,
		ChecksumAsset:    "SHA256SUMS",
		SignatureAsset:   ".sig",
		RequireSignature: len(publicKey) > 0,
	})
	if err != nil {
		return nil, err
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: buildinfo.Version(),
		Providers:      []updater.Provider{provider},
		PublicKey:      publicKey,
		Window: &updater.BuiltinWindow{
			Options: updater.WindowOptions{
				Title:  "ben-desktop update",
				Width:  780,
				Height: 560,
			},
		},
	}); err != nil {
		return nil, err
	}
	attachUpdaterLogging(app, logger)
	return &CheckRunner{app: app, logger: logger}, nil
}

func attachUpdaterLogging(app *application.App, logger *slog.Logger) {
	if app == nil || logger == nil {
		return
	}

	log := func(level slog.Level, msg string, attrs ...slog.Attr) {
		if !logger.Enabled(context.Background(), level) {
			return
		}
		logger.LogAttrs(context.Background(), level, msg, append(attrs, slog.String("service", "appupdate"))...)
	}

	app.Event.On(updater.EventCheckStarted, func(_ *application.CustomEvent) {
		log(slog.LevelDebug, "update check started")
	})

	app.Event.On(updater.EventUpdateAvailable, func(e *application.CustomEvent) {
		rel, ok := e.Data.(*updater.Release)
		if !ok {
			log(slog.LevelWarn, "update available event payload has unexpected type", slog.Any("type", e.Data))
			return
		}
		log(slog.LevelInfo, "update available",
			slog.String("version", rel.Version),
			slog.String("provider", rel.Provider),
			slog.String("asset", rel.Artifact.Filename),
		)
	})

	app.Event.On(updater.EventNoUpdate, func(_ *application.CustomEvent) {
		log(slog.LevelInfo, "no update available", slog.String("currentVersion", buildinfo.Version()))
	})

	app.Event.On(updater.EventDownloadStarted, func(e *application.CustomEvent) {
		rel, ok := e.Data.(*updater.Release)
		if !ok {
			return
		}
		log(slog.LevelInfo, "update download started",
			slog.String("version", rel.Version),
			slog.String("asset", rel.Artifact.Filename),
		)
	})

	app.Event.On(updater.EventDownloadComplete, func(e *application.CustomEvent) {
		rel, ok := e.Data.(*updater.Release)
		if !ok {
			return
		}
		log(slog.LevelInfo, "update download complete",
			slog.String("version", rel.Version),
			slog.String("asset", rel.Artifact.Filename),
		)
	})

	app.Event.On(updater.EventVerifying, func(e *application.CustomEvent) {
		rel, ok := e.Data.(*updater.Release)
		if !ok {
			return
		}
		log(slog.LevelInfo, "update verifying", slog.String("version", rel.Version))
	})

	app.Event.On(updater.EventInstalling, func(e *application.CustomEvent) {
		rel, ok := e.Data.(*updater.Release)
		if !ok {
			return
		}
		log(slog.LevelInfo, "update installing", slog.String("version", rel.Version))
	})

	app.Event.On(updater.EventUpdateReady, func(e *application.CustomEvent) {
		rel, ok := e.Data.(*updater.Release)
		if !ok {
			return
		}
		log(slog.LevelInfo, "update ready; restart pending", slog.String("version", rel.Version))
	})

	app.Event.On(updater.EventError, func(e *application.CustomEvent) {
		info, ok := e.Data.(updater.ErrorInfo)
		if !ok {
			log(slog.LevelWarn, "update error event payload has unexpected type", slog.Any("type", e.Data))
			return
		}
		log(slog.LevelError, "update error",
			slog.String("stage", string(info.Stage)),
			slog.String("message", info.Message),
			slog.String("provider", info.Provider),
		)
	})

	app.Event.On(updater.EventMeta, func(e *application.CustomEvent) {
		meta, ok := e.Data.(updater.Meta)
		if !ok {
			return
		}
		log(slog.LevelDebug, "update meta",
			slog.String("currentVersion", meta.CurrentVersion),
			slog.String("skippedVersion", meta.SkippedVersion),
		)
	})
}

func parseEd25519PublicKey(raw []byte) (ed25519.PublicKey, error) {
	if len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	block, _ := pem.Decode(raw)
	if block != nil {
		raw = block.Bytes
	}
	pubAny, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("appupdate: parse Ed25519 public key: %w", err)
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("appupdate: public key has wrong type %T", pubAny)
	}
	return pub, nil
}

func (r *CheckRunner) StartManualCheck(onDone func()) bool {
	if r == nil || r.app == nil {
		return false
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return false
	}
	r.running = true
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
			if onDone != nil {
				onDone()
			}
		}()
		if err := r.app.Updater.CheckAndInstall(context.Background()); err != nil {
			r.logger.Error("update check failed", slog.Any("error", err), slog.String("service", "appupdate"))
		}
	}()
	return true
}
