//go:build windows

package platform

import (
	"path"
	"testing"
)

func TestExtractICOImageData(t *testing.T) {
	t.Parallel()

	assets := []string{
		path.Join("thumbbar", "light", "previous.ico"),
		path.Join("thumbbar", "light", "play.ico"),
		path.Join("thumbbar", "light", "pause.ico"),
		path.Join("thumbbar", "light", "next.ico"),
		path.Join("thumbbar", "dark", "previous.ico"),
		path.Join("thumbbar", "dark", "play.ico"),
		path.Join("thumbbar", "dark", "pause.ico"),
		path.Join("thumbbar", "dark", "next.ico"),
	}

	for _, asset := range assets {
		asset := asset
		t.Run(asset, func(t *testing.T) {
			t.Parallel()

			data, err := thumbbarIconFS.ReadFile(asset)
			if err != nil {
				t.Fatalf("read asset: %v", err)
			}

			imageData, width, height, err := extractICOImageData(data, thumbbarIconDesiredSize)
			if err != nil {
				t.Fatalf("extract icon image: %v", err)
			}
			if len(imageData) == 0 {
				t.Fatal("expected icon image bytes")
			}
			if width != thumbbarIconDesiredSize || height != thumbbarIconDesiredSize {
				t.Fatalf("expected %dx%d icon, got %dx%d", thumbbarIconDesiredSize, thumbbarIconDesiredSize, width, height)
			}
		})
	}
}
