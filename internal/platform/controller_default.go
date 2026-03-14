//go:build !windows

package platform

import (
	"ben/desktop/internal/playback"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type noopController struct{}

func NewController(_ *application.App, _ *playback.Session, _ playback.PlaybackCore) playback.PlatformController {
	return &noopController{}
}

func (c *noopController) Start() error {
	return nil
}

func (c *noopController) Stop() error {
	return nil
}

func (c *noopController) HandlePlaybackSnapshot(playback.SessionSnapshot) {}
