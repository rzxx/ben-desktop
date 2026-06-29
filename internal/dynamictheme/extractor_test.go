package dynamictheme

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractProducesVersionedSemanticTheme(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 240, 240))
	fillRect(img, img.Bounds(), color.NRGBA{R: 32, G: 91, B: 184, A: 255})
	fillRect(img, image.Rect(70, 70, 170, 170), color.NRGBA{R: 232, G: 130, B: 24, A: 255})

	theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
	if err != nil {
		t.Fatalf("extract theme: %v", err)
	}
	if theme.Version != 1 {
		t.Fatalf("expected theme contract version 1, got %d", theme.Version)
	}
	if theme.Artwork.Atmosphere.Hex == "" {
		t.Fatal("expected atmosphere evidence")
	}
	if theme.Light.Surface.Canvas.Hex == "" || theme.Dark.Surface.Canvas.Hex == "" {
		t.Fatal("expected both semantic schemes")
	}
	if theme.Light.Accent.OnAccent.Hex == "" || theme.Dark.Accent.OnAccent.Hex == "" {
		t.Fatal("expected resolved on-accent roles")
	}
	if len(theme.Compatibility.ThemeScale) != 11 || len(theme.Compatibility.AccentScale) != 11 {
		t.Fatal("expected temporary compatibility scales")
	}
}

func TestPureBlackAndWhiteArtworkAreSuccessfulNeutralThemes(t *testing.T) {
	t.Parallel()
	for name, fill := range map[string]color.NRGBA{
		"black": {R: 0, G: 0, B: 0, A: 255},
		"white": {R: 255, G: 255, B: 255, A: 255},
	} {
		t.Run(name, func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, 48, 48))
			fillRect(img, img.Bounds(), fill)
			theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
			if err != nil {
				t.Fatalf("extract %s theme: %v", name, err)
			}
			if theme.Artwork.Class != ArtworkClassNeutral {
				t.Fatalf("expected neutral class, got %q", theme.Artwork.Class)
			}
			if theme.Artwork.Accent != nil {
				t.Fatal("neutral artwork must not invent an accent")
			}
			if theme.Light.Surface.Canvas.Chroma > .001 || theme.Dark.Accent.Default.Chroma > .001 {
				t.Fatal("neutral artwork must remain neutral")
			}
		})
	}
}

func TestTransparentArtworkIsRejected(t *testing.T) {
	t.Parallel()
	_, err := NewExtractor().ExtractFromImage(image.NewNRGBA(image.Rect(0, 0, 24, 24)), ExtractOptions{})
	if err == nil {
		t.Fatal("expected fully transparent artwork to fail")
	}
}

func TestOversizedArtworkIsRejected(t *testing.T) {
	t.Parallel()
	_, err := NewExtractor().ExtractFromImage(image.NewUniform(color.White), ExtractOptions{})
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum pixel count") {
		t.Fatalf("expected oversized in-memory image error, got %v", err)
	}
}

func TestOversizedWebPIsRejectedBeforeDecode(t *testing.T) {
	t.Parallel()
	data := []byte{
		'R', 'I', 'F', 'F', 22, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', 'X', 10, 0, 0, 0, 1 << 4, 0, 0, 0,
		0xfe, 0xff, 0x00, 0xfe, 0xff, 0x00,
	}
	path := filepath.Join(t.TempDir(), "oversized.webp")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewExtractor().ExtractFromPath(path, ExtractOptions{})
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum pixel count") {
		t.Fatalf("expected oversized path image error, got %v", err)
	}
}

func TestSingleHueArtworkUsesFaithfulAccentFallback(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 180, 180))
	fillRect(img, img.Bounds(), color.NRGBA{R: 31, G: 87, B: 172, A: 255})
	fillRect(img, image.Rect(24, 24, 156, 78), color.NRGBA{R: 80, G: 133, B: 224, A: 255})
	theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
	if err != nil {
		t.Fatal(err)
	}
	if theme.Artwork.Class != ArtworkClassSingleHue {
		t.Fatalf("expected single-hue class, got %q", theme.Artwork.Class)
	}
	if theme.Artwork.Accent != nil {
		t.Fatal("single-hue artwork must not synthesize another hue")
	}
	if hueDistance(theme.Artwork.Atmosphere.Hue, theme.Light.Accent.Default.Hue) > 1 {
		t.Fatal("fallback accent must retain atmosphere hue")
	}
}

func TestSmallCentralArtworkColorCanBecomeAccent(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	fillRect(img, img.Bounds(), color.NRGBA{R: 29, G: 75, B: 164, A: 255})
	fillRect(img, image.Rect(90, 132, 210, 168), color.NRGBA{R: 237, G: 128, B: 18, A: 255})
	theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
	if err != nil {
		t.Fatal(err)
	}
	if theme.Artwork.Class != ArtworkClassMulticolor {
		t.Fatalf("expected multicolor class, got %q", theme.Artwork.Class)
	}
	if theme.Artwork.Accent == nil {
		t.Fatal("expected source-supported central accent")
	}
	if hueDistance(theme.Artwork.Accent.Hue, colorFromRGB(237, 128, 18, 0).h) > 12 {
		t.Fatalf("expected orange accent, got %s", theme.Artwork.Accent.Hex)
	}
}

func TestNeutralCoverWithDeliberateColorMarkKeepsNeutralAtmosphere(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	fillRect(img, img.Bounds(), color.NRGBA{R: 145, G: 145, B: 145, A: 255})
	fillRect(img, image.Rect(126, 126, 174, 174), color.NRGBA{R: 205, G: 37, B: 45, A: 255})
	theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
	if err != nil {
		t.Fatal(err)
	}
	if theme.Artwork.Accent == nil {
		t.Fatal("expected the deliberate red mark to remain available as accent evidence")
	}
	if theme.Artwork.Atmosphere.Chroma > .03 {
		t.Fatalf("expected neutral atmosphere, got chroma %.3f", theme.Artwork.Atmosphere.Chroma)
	}
}

func TestSurfacePolicyIsStableAndRestrainedAcrossArtwork(t *testing.T) {
	t.Parallel()
	makeTheme := func(fill color.NRGBA) Theme {
		img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
		fillRect(img, img.Bounds(), fill)
		theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
		if err != nil {
			t.Fatal(err)
		}
		return theme
	}
	muted := makeTheme(color.NRGBA{R: 126, G: 112, B: 91, A: 255})
	vivid := makeTheme(color.NRGBA{R: 245, G: 18, B: 155, A: 255})
	if math.Abs(muted.Light.Surface.Canvas.Lightness-vivid.Light.Surface.Canvas.Lightness) > .001 {
		t.Fatal("artwork must not own surface tone")
	}
	if math.Abs(muted.Dark.Surface.Raised.Lightness-vivid.Dark.Surface.Raised.Lightness) > .001 {
		t.Fatal("dark surface hierarchy must be stable")
	}
	for _, c := range []Color{muted.Light.Surface.Canvas, vivid.Light.Surface.Canvas, muted.Dark.Surface.Raised, vivid.Dark.Surface.Raised} {
		if c.Chroma > .0121 {
			t.Fatalf("surface chroma exceeds role budget: %.4f", c.Chroma)
		}
	}
}

func TestResolvedInteractiveRolesMeetContrastContract(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	fillRect(img, img.Bounds(), color.NRGBA{R: 219, G: 22, B: 154, A: 255})
	theme, err := NewExtractor().ExtractFromImage(img, ExtractOptions{Quality: 1})
	if err != nil {
		t.Fatal(err)
	}
	for mode, scheme := range map[string]Scheme{"light": theme.Light, "dark": theme.Dark} {
		for role, background := range map[string]Color{"accent": scheme.Accent.Default, "accent-hover": scheme.Accent.Hover, "accent-pressed": scheme.Accent.Pressed} {
			if ratio := contrastRatio(fromPublic(scheme.Accent.OnAccent), fromPublic(background)); ratio < 4.5 {
				t.Fatalf("%s %s contrast %.2f is below 4.5", mode, role, ratio)
			}
		}
		if ratio := contrastRatio(fromPublic(scheme.Content.Primary), fromPublic(scheme.Surface.Canvas)); ratio < 7 {
			t.Fatalf("%s primary content contrast %.2f is below 7", mode, ratio)
		}
		if ratio := contrastRatio(fromPublic(scheme.Content.Secondary), fromPublic(scheme.Surface.Canvas)); ratio < 4.5 {
			t.Fatalf("%s secondary content contrast %.2f is below 4.5", mode, ratio)
		}
		if ratio := contrastRatio(fromPublic(scheme.Border.Focus), fromPublic(scheme.Surface.Canvas)); ratio < 3 {
			t.Fatalf("%s focus contrast %.2f is below 3", mode, ratio)
		}
		for status, role := range map[string]StatusRole{"danger": scheme.Status.Danger, "warning": scheme.Status.Warning, "success": scheme.Status.Success} {
			if ratio := contrastRatio(fromPublic(role.On), fromPublic(role.Default)); ratio < 4.5 {
				t.Fatalf("%s %s contrast %.2f is below 4.5", mode, status, ratio)
			}
		}
	}
}

func TestGamutMappingReducesChromaWithoutChangingToneOrHue(t *testing.T) {
	t.Parallel()
	color := gamutMappedOKLCH(.72, .5, 145)
	if color.c >= .5 {
		t.Fatal("expected out-of-gamut chroma to be reduced")
	}
	if math.Abs(color.l-.72) > 1e-9 || hueDistance(color.h, 145) > 1e-9 {
		t.Fatal("gamut mapping must preserve requested tone and hue")
	}
	if !oklchInSRGB(color.l, color.c, color.h) {
		t.Fatal("mapped color must be inside sRGB")
	}
}

func TestNormalizeExtractOptionsBoundsOperationalInputs(t *testing.T) {
	t.Parallel()
	defaults := NormalizeExtractOptions(ExtractOptions{})
	if defaults.AlphaThreshold != defaultExtractOptions.AlphaThreshold {
		t.Fatalf("expected default alpha threshold %d, got %d", defaultExtractOptions.AlphaThreshold, defaults.AlphaThreshold)
	}
	options := NormalizeExtractOptions(ExtractOptions{Quality: 999, CandidateCount: 999, QuantizationBits: 99, AlphaThreshold: 999, WorkerCount: 999})
	if options.Quality > 12 || options.CandidateCount > 64 || options.QuantizationBits > 6 || options.AlphaThreshold > 254 || options.WorkerCount > maxWorkerCount {
		t.Fatalf("unexpected normalized options: %#v", options)
	}
}

func fillRect(img *image.NRGBA, rect image.Rectangle, fill color.NRGBA) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
}
