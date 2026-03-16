package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/palette"
)

type themePlaybackStub struct {
	*desktopcore.UnavailableCore
	result          apitypes.RecordingArtworkResult
	err             error
	lastRecordingID string
	lastVariant     string
}

func (s *themePlaybackStub) ResolveRecordingArtwork(_ context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
	s.lastRecordingID = recordingID
	s.lastVariant = variant
	if s.err != nil {
		return apitypes.RecordingArtworkResult{}, s.err
	}
	return s.result, nil
}

type themeExtractorStub struct {
	callCount int
	lastPath  string
	palette   palette.ThemePalette
	err       error
}

func (s *themeExtractorStub) ExtractFromPath(path string, _ palette.ExtractOptions) (palette.ThemePalette, error) {
	s.callCount++
	s.lastPath = path
	if s.err != nil {
		return palette.ThemePalette{}, s.err
	}
	return s.palette, nil
}

func TestThemeFacadeGenerateRecordingThemeUsesResolved320ArtworkAndCaches(t *testing.T) {
	t.Parallel()

	coverPath := filepath.Join(t.TempDir(), "cover.webp")
	if err := os.WriteFile(coverPath, []byte("cover"), 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}

	playbackStub := &themePlaybackStub{
		UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unavailable")),
		result: apitypes.RecordingArtworkResult{
			RecordingID: "rec-1",
			Available:   true,
			LocalPath:   coverPath,
		},
	}
	extractorStub := &themeExtractorStub{
		palette: palette.ThemePalette{
			ThemeScale: []palette.PaletteTone{{Tone: 50}, {Tone: 100}},
		},
	}

	facade := NewThemeFacade(&coreHost{
		unavailable: desktopcore.NewUnavailableCore(errors.New("unavailable")),
		playback:    playbackStub,
	})
	facade.extractor = extractorStub

	if _, err := facade.GenerateRecordingTheme(context.Background(), "rec-1"); err != nil {
		t.Fatalf("generate recording theme: %v", err)
	}
	if extractorStub.callCount != 1 {
		t.Fatalf("extractor calls = %d, want 1", extractorStub.callCount)
	}
	if playbackStub.lastRecordingID != "rec-1" {
		t.Fatalf("recording id = %q, want rec-1", playbackStub.lastRecordingID)
	}
	if playbackStub.lastVariant != themeArtworkVariant {
		t.Fatalf("variant = %q, want %q", playbackStub.lastVariant, themeArtworkVariant)
	}

	if _, err := facade.GenerateRecordingTheme(context.Background(), "rec-1"); err != nil {
		t.Fatalf("generate cached recording theme: %v", err)
	}
	if extractorStub.callCount != 1 {
		t.Fatalf("extractor calls after cache hit = %d, want 1", extractorStub.callCount)
	}

	nextModTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(coverPath, nextModTime, nextModTime); err != nil {
		t.Fatalf("chtimes cover: %v", err)
	}

	if _, err := facade.GenerateRecordingTheme(context.Background(), "rec-1"); err != nil {
		t.Fatalf("generate invalidated recording theme: %v", err)
	}
	if extractorStub.callCount != 2 {
		t.Fatalf("extractor calls after mtime change = %d, want 2", extractorStub.callCount)
	}
}

func TestThemeFacadeGenerateRecordingThemeRejectsUnavailableArtwork(t *testing.T) {
	t.Parallel()

	facade := NewThemeFacade(&coreHost{
		unavailable: desktopcore.NewUnavailableCore(errors.New("unavailable")),
		playback: &themePlaybackStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unavailable")),
			result: apitypes.RecordingArtworkResult{
				RecordingID: "rec-2",
				Available:   false,
			},
		},
	})
	facade.extractor = &themeExtractorStub{}

	_, err := facade.GenerateRecordingTheme(context.Background(), "rec-2")
	if err == nil || err.Error() != errThemeArtworkAbsent {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestThemeFacadeGenerateRecordingThemeRequiresRecordingID(t *testing.T) {
	t.Parallel()

	facade := NewThemeFacade(nil)
	facade.extractor = &themeExtractorStub{}

	_, err := facade.GenerateRecordingTheme(context.Background(), "   ")
	if err == nil || err.Error() != "recording id is required" {
		t.Fatalf("unexpected error = %v", err)
	}
}
