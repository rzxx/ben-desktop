package palette

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"
)

func TestExtractFromImageGeneratesPalette(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	fillRect(img, image.Rect(0, 0, 128, 128), color.NRGBA{R: 198, G: 48, B: 59, A: 255})
	fillRect(img, image.Rect(128, 0, 256, 128), color.NRGBA{R: 24, G: 144, B: 242, A: 255})
	fillRect(img, image.Rect(0, 128, 128, 256), color.NRGBA{R: 242, G: 188, B: 12, A: 255})
	fillRect(img, image.Rect(128, 128, 256, 256), color.NRGBA{R: 36, G: 184, B: 92, A: 255})

	extractor := NewExtractor()
	palette, err := extractor.ExtractFromImage(img, ExtractOptions{
		ColorCount:       5,
		CandidateCount:   24,
		Quality:          1,
		QuantizationBits: 5,
	})
	if err != nil {
		t.Fatalf("extract palette: %v", err)
	}

	if palette.Primary == nil {
		t.Fatal("expected primary color")
	}
	if palette.Accent == nil {
		t.Fatal("expected accent color")
	}
	if len(palette.ThemeScale) != 11 {
		t.Fatalf("expected 11 theme scale colors, got %d", len(palette.ThemeScale))
	}
	if len(palette.AccentScale) != 11 {
		t.Fatalf("expected 11 accent scale colors, got %d", len(palette.AccentScale))
	}
	if palette.ThemeScale[0].Tone != 50 || palette.ThemeScale[1].Tone != 100 || palette.ThemeScale[len(palette.ThemeScale)-1].Tone != 950 {
		t.Fatalf("unexpected theme scale tone anchors: %#v", palette.ThemeScale)
	}
}

func TestExtractFromImageRejectsTransparentImages(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	extractor := NewExtractor()

	_, err := extractor.ExtractFromImage(img, ExtractOptions{})
	if err == nil {
		t.Fatal("expected error for fully transparent image")
	}
}

func TestExtractFromImageCapturesDarkAndLightOnHighContrastCover(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 320, 320))
	fillRect(img, img.Bounds(), color.NRGBA{R: 70, G: 216, B: 92, A: 255})

	fillRect(img, image.Rect(24, 32, 296, 78), color.NRGBA{R: 8, G: 8, B: 8, A: 255})
	fillRect(img, image.Rect(24, 96, 280, 136), color.NRGBA{R: 8, G: 8, B: 8, A: 255})
	fillRect(img, image.Rect(24, 154, 304, 188), color.NRGBA{R: 8, G: 8, B: 8, A: 255})
	fillRect(img, image.Rect(24, 238, 296, 274), color.NRGBA{R: 8, G: 8, B: 8, A: 255})

	extractor := NewExtractor()
	palette, err := extractor.ExtractFromImage(img, ExtractOptions{})
	if err != nil {
		t.Fatalf("extract palette: %v", err)
	}

	if palette.Dark == nil {
		t.Fatal("expected dark color")
	}
	if palette.Light == nil {
		t.Fatal("expected light color")
	}
	if palette.Dark.Lightness >= palette.Light.Lightness {
		t.Fatalf("expected dark color to be darker than light color: dark=%0.3f light=%0.3f", palette.Dark.Lightness, palette.Light.Lightness)
	}
	if len(palette.ThemeScale) != 11 {
		t.Fatalf("expected 11 theme scale colors, got %d", len(palette.ThemeScale))
	}
	if palette.ThemeScale[1].Color.Hex != palette.Light.Hex {
		t.Fatalf("expected theme-100 to anchor light color: scale=%s light=%s", palette.ThemeScale[1].Color.Hex, palette.Light.Hex)
	}
	if palette.ThemeScale[len(palette.ThemeScale)-1].Color.Hex != palette.Dark.Hex {
		t.Fatalf("expected theme-950 to anchor dark color: scale=%s dark=%s", palette.ThemeScale[len(palette.ThemeScale)-1].Color.Hex, palette.Dark.Hex)
	}
	if len(palette.AccentScale) != 11 {
		t.Fatalf("expected 11 accent scale colors, got %d", len(palette.AccentScale))
	}
}

func TestExtractFromImageAnchorsDarkAndLightAroundNeutralBases(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 320, 320))
	fillRect(img, img.Bounds(), color.NRGBA{R: 70, G: 126, B: 226, A: 255})
	fillRect(img, image.Rect(24, 32, 292, 100), color.NRGBA{R: 36, G: 92, B: 192, A: 255})
	fillRect(img, image.Rect(48, 224, 304, 288), color.NRGBA{R: 114, G: 164, B: 242, A: 255})

	extractor := NewExtractor()
	palette, err := extractor.ExtractFromImage(img, ExtractOptions{})
	if err != nil {
		t.Fatalf("extract palette: %v", err)
	}

	if palette.Primary == nil {
		t.Fatal("expected primary color")
	}
	if palette.Dark == nil {
		t.Fatal("expected dark color")
	}
	if palette.Light == nil {
		t.Fatal("expected light color")
	}

	normalized := NormalizeExtractOptions(ExtractOptions{})
	darkTolerance := normalized.DarkLightnessDeviation + 0.02
	if math.Abs(palette.Dark.Lightness-normalized.DarkBaseLightness) > darkTolerance {
		t.Fatalf(
			"expected dark lightness near base %0.3f (+/-%0.3f), got %0.3f",
			normalized.DarkBaseLightness,
			darkTolerance,
			palette.Dark.Lightness,
		)
	}

	lightTolerance := normalized.LightLightnessDeviation + 0.02
	if math.Abs(palette.Light.Lightness-normalized.LightBaseLightness) > lightTolerance {
		t.Fatalf(
			"expected light lightness near base %0.3f (+/-%0.3f), got %0.3f",
			normalized.LightBaseLightness,
			lightTolerance,
			palette.Light.Lightness,
		)
	}

	darkHueDistance := hueDistanceDegrees(palette.Primary.Hue, palette.Dark.Hue)
	if darkHueDistance > 35 {
		t.Fatalf("expected dark hue to stay near primary hue, got delta=%0.2f", darkHueDistance)
	}

	lightHueDistance := hueDistanceDegrees(palette.Primary.Hue, palette.Light.Hue)
	if lightHueDistance > 35 {
		t.Fatalf("expected light hue to stay near primary hue, got delta=%0.2f", lightHueDistance)
	}
	if len(palette.ThemeScale) != 11 {
		t.Fatalf("expected 11 theme scale colors, got %d", len(palette.ThemeScale))
	}
	if palette.ThemeScale[0].Tone != 50 || palette.ThemeScale[1].Tone != 100 || palette.ThemeScale[10].Tone != 950 {
		t.Fatalf("unexpected theme scale tones: %#v", palette.ThemeScale)
	}
	if palette.ThemeScale[1].Color.Hex != palette.Light.Hex {
		t.Fatalf("expected theme-100 to anchor light color: scale=%s light=%s", palette.ThemeScale[1].Color.Hex, palette.Light.Hex)
	}
	if palette.ThemeScale[10].Color.Hex != palette.Dark.Hex {
		t.Fatalf("expected theme-950 to anchor dark color: scale=%s dark=%s", palette.ThemeScale[10].Color.Hex, palette.Dark.Hex)
	}
	if len(palette.AccentScale) != 11 {
		t.Fatalf("expected 11 accent scale colors, got %d", len(palette.AccentScale))
	}
}

func TestThemeScaleAlwaysUsesPrimaryHue(t *testing.T) {
	t.Parallel()

	primary := makeTestSwatch(220, 58, 64, 900)
	accent := makeTestSwatch(46, 216, 116, 500)
	dark := makeTestSwatch(22, 16, 18, 400)
	light := makeTestSwatch(244, 243, 246, 400)

	scale := buildThemeScaleSwatches(themeSelection{
		primary: swatchPointer(primary),
		accent:  swatchPointer(accent),
		dark:    swatchPointer(dark),
		light:   swatchPointer(light),
	}, DefaultExtractOptions())

	if len(scale) != 11 {
		t.Fatalf("expected 11 theme scale swatches, got %d", len(scale))
	}

	mid := scale[5]
	if hueDistanceDegrees(mid.hue, primary.hue) > 20 {
		t.Fatalf("expected theme scale hue to follow primary hue, got primary=%0.2f scale=%0.2f", primary.hue, mid.hue)
	}
	if hueDistanceDegrees(mid.hue, accent.hue) < 25 {
		t.Fatalf("expected theme scale hue to avoid accent hue override, got accent=%0.2f scale=%0.2f", accent.hue, mid.hue)
	}
}

func TestAccentFallsBackToPrimaryForMonochromeCover(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 196, 196))
	fillRect(img, img.Bounds(), color.NRGBA{R: 62, G: 110, B: 214, A: 255})

	extractor := NewExtractor()
	palette, err := extractor.ExtractFromImage(img, ExtractOptions{})
	if err != nil {
		t.Fatalf("extract palette: %v", err)
	}

	if palette.Primary == nil {
		t.Fatal("expected primary color")
	}
	if palette.Accent == nil {
		t.Fatal("expected accent color")
	}
	if palette.Accent.Hex != palette.Primary.Hex {
		t.Fatalf("expected accent fallback to primary for monochrome cover: primary=%s accent=%s", palette.Primary.Hex, palette.Accent.Hex)
	}
}

func TestMonochromeCoverKeepsAccentScaleNearNeutral(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 220, 220))
	fillRect(img, img.Bounds(), color.NRGBA{R: 182, G: 178, B: 168, A: 255})
	fillRect(img, image.Rect(18, 22, 200, 84), color.NRGBA{R: 206, G: 202, B: 192, A: 255})
	fillRect(img, image.Rect(28, 118, 210, 188), color.NRGBA{R: 132, G: 128, B: 119, A: 255})

	extractor := NewExtractor()
	palette, err := extractor.ExtractFromImage(img, ExtractOptions{})
	if err != nil {
		t.Fatalf("extract palette: %v", err)
	}

	if palette.Primary == nil {
		t.Fatal("expected primary color")
	}
	if palette.Accent == nil {
		t.Fatal("expected accent color")
	}
	if palette.Accent.Hex != palette.Primary.Hex {
		t.Fatalf("expected monochrome accent to match primary: primary=%s accent=%s", palette.Primary.Hex, palette.Accent.Hex)
	}
	if len(palette.ThemeScale) != 11 || len(palette.AccentScale) != 11 {
		t.Fatalf("expected 11-tone scales, got theme=%d accent=%d", len(palette.ThemeScale), len(palette.AccentScale))
	}

	for index := range palette.ThemeScale {
		if palette.ThemeScale[index].Color.Hex != palette.AccentScale[index].Color.Hex {
			t.Fatalf(
				"expected monochrome accent scale to mirror theme scale at tone %d: theme=%s accent=%s",
				palette.ThemeScale[index].Tone,
				palette.ThemeScale[index].Color.Hex,
				palette.AccentScale[index].Color.Hex,
			)
		}
	}
}

func TestChooseAccentSwatchPrefersContrastiveHue(t *testing.T) {
	t.Parallel()

	primary := makeTestSwatch(198, 44, 52, 1200)
	orange := makeTestSwatch(230, 124, 42, 920)
	yellow := makeTestSwatch(222, 190, 40, 650)

	accent, ok := chooseAccentSwatch(primary, []swatch{primary, orange, yellow}, DefaultExtractOptions())
	if !ok {
		t.Fatal("expected accent candidate")
	}
	if !sameRGB(accent, yellow) {
		t.Fatalf("expected accent to prefer broader hue contrast: got %#v want %#v", accent, yellow)
	}
}

func TestChooseAccentSwatchSkipsLowChromaMutedOutlier(t *testing.T) {
	t.Parallel()

	primary := makeTestSwatch(68, 134, 157, 1200)   // #44869D
	mutedTeal := makeTestSwatch(132, 155, 146, 980) // #849B92
	vividGold := makeTestSwatch(222, 190, 40, 840)

	accent, ok := chooseAccentSwatch(primary, []swatch{primary, mutedTeal, vividGold}, DefaultExtractOptions())
	if !ok {
		t.Fatal("expected accent candidate")
	}
	if !sameRGB(accent, vividGold) {
		t.Fatalf("expected accent to reject low-chroma muted outlier: got %#v want %#v", accent, vividGold)
	}
}

func TestChooseAccentSwatchPenalizesLowChromaFarHue(t *testing.T) {
	t.Parallel()

	primary := oklchToSwatch(0.58, 0.076, 223, 1200)
	lowChromaFarHue := oklchToSwatch(0.62, 0.045, 170, 1200)
	balanced := oklchToSwatch(0.62, 0.06, 84, 1200)

	accent, ok := chooseAccentSwatch(primary, []swatch{primary, lowChromaFarHue, balanced}, DefaultExtractOptions())
	if !ok {
		t.Fatal("expected accent candidate")
	}
	if !sameRGB(accent, balanced) {
		t.Fatalf("expected low-chroma far-hue accent penalty to avoid surprising muted hue shift: got %#v want %#v", accent, balanced)
	}
}

func TestChooseAccentSwatchAllowsMutedFarHueWhenPrimaryIsNearNeutral(t *testing.T) {
	t.Parallel()

	options := DefaultExtractOptions()
	options.MinDelta = 0.08

	primary := makeTestSwatch(0xE3, 0xDB, 0xC4, 1200)
	brown := makeTestSwatch(0x9F, 0x6B, 0x3C, 980)
	blue := makeTestSwatch(0x6B, 0x98, 0x9A, 980)

	accent, ok := chooseAccentSwatch(primary, []swatch{primary, brown, blue}, options)
	if !ok {
		t.Fatal("expected accent candidate")
	}
	if !sameRGB(accent, blue) {
		t.Fatalf("expected near-neutral primary to allow muted far-hue accent: got %#v want %#v", accent, blue)
	}
}

func TestSelectPaletteSwatchesDemotesVeryDarkPrimaryWhenColorfulAlternativeExists(t *testing.T) {
	t.Parallel()

	options := DefaultExtractOptions()
	options.ColorCount = 1

	dark := makeTestSwatch(4, 12, 28, 1200)     // #040C1C
	orange := makeTestSwatch(219, 121, 22, 520) // #DB7916
	blue := makeTestSwatch(57, 137, 196, 430)   // #3989C4

	selected := selectPaletteSwatches([]swatch{dark, orange, blue}, options)
	if len(selected) != 1 {
		t.Fatalf("expected 1 selected swatch, got %d", len(selected))
	}
	if sameRGB(selected[0], dark) {
		t.Fatalf("expected dark background color to be demoted when a substantial colorful alternative exists: selected=%#v", selected[0])
	}
}

func TestSelectPaletteSwatchesKeepsVeryDarkPrimaryWhenNoColorfulAlternativeExists(t *testing.T) {
	t.Parallel()

	options := DefaultExtractOptions()
	options.ColorCount = 1

	dark := makeTestSwatch(4, 12, 28, 1200)          // #040C1C
	mutedMid := makeTestSwatch(101, 108, 114, 520)   // low-chroma mid tone
	mutedLight := makeTestSwatch(148, 152, 158, 430) // low-chroma light tone

	selected := selectPaletteSwatches([]swatch{dark, mutedMid, mutedLight}, options)
	if len(selected) != 1 {
		t.Fatalf("expected 1 selected swatch, got %d", len(selected))
	}
	if !sameRGB(selected[0], dark) {
		t.Fatalf("expected dark color to stay primary when no substantial colorful alternative exists: got %#v want %#v", selected[0], dark)
	}
}

func TestNormalizeExtractOptionsCapsWorkerCount(t *testing.T) {
	t.Parallel()

	normalized := NormalizeExtractOptions(ExtractOptions{WorkerCount: 10_000})
	maxWorkers := runtime.GOMAXPROCS(0)
	if maxWorkers > maxWorkerCap {
		maxWorkers = maxWorkerCap
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	if normalized.WorkerCount < 1 || normalized.WorkerCount > maxWorkers {
		t.Fatalf("expected worker count in [1,%d], got %d", maxWorkers, normalized.WorkerCount)
	}
	if normalized.DarkBaseLightness <= 0 || normalized.LightBaseLightness <= 0 {
		t.Fatalf("expected normalized neutral base lightness values, got dark=%0.3f light=%0.3f", normalized.DarkBaseLightness, normalized.LightBaseLightness)
	}
	if normalized.DarkLightnessDeviation <= 0 || normalized.LightLightnessDeviation <= 0 {
		t.Fatalf("expected normalized neutral deviation values, got dark=%0.3f light=%0.3f", normalized.DarkLightnessDeviation, normalized.LightLightnessDeviation)
	}
	if normalized.DarkChromaScale <= 0 || normalized.LightChromaScale <= 0 {
		t.Fatalf("expected normalized neutral chroma scales, got dark=%0.3f light=%0.3f", normalized.DarkChromaScale, normalized.LightChromaScale)
	}
}

func TestNormalizeExtractOptionsClampsNeutralAnchorControls(t *testing.T) {
	t.Parallel()

	normalized := NormalizeExtractOptions(ExtractOptions{
		DarkBaseLightness:       0.9,
		LightBaseLightness:      0.1,
		DarkLightnessDeviation:  2,
		LightLightnessDeviation: 2,
		DarkChromaScale:         4,
		LightChromaScale:        4,
	})

	if normalized.DarkBaseLightness > 0.35 {
		t.Fatalf("expected dark base lightness to clamp <= 0.35, got %0.3f", normalized.DarkBaseLightness)
	}
	if normalized.LightBaseLightness < 0.75 {
		t.Fatalf("expected light base lightness to clamp >= 0.75, got %0.3f", normalized.LightBaseLightness)
	}
	if normalized.LightBaseLightness < normalized.DarkBaseLightness+0.2 {
		t.Fatalf(
			"expected light base lightness to stay at least 0.2 above dark base: dark=%0.3f light=%0.3f",
			normalized.DarkBaseLightness,
			normalized.LightBaseLightness,
		)
	}
	if normalized.DarkLightnessDeviation > 0.3 || normalized.LightLightnessDeviation > 0.2 {
		t.Fatalf(
			"expected neutral deviations to clamp, got dark=%0.3f light=%0.3f",
			normalized.DarkLightnessDeviation,
			normalized.LightLightnessDeviation,
		)
	}
	if normalized.DarkChromaScale > 1.4 || normalized.LightChromaScale > 1.2 {
		t.Fatalf(
			"expected neutral chroma scales to clamp, got dark=%0.3f light=%0.3f",
			normalized.DarkChromaScale,
			normalized.LightChromaScale,
		)
	}
}

func fillRect(img *image.NRGBA, rect image.Rectangle, fill color.NRGBA) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
}

func makeTestSwatch(red uint8, green uint8, blue uint8, population int) swatch {
	okL, okA, okB := rgbToOKLab(red, green, blue)
	hue := math.Atan2(okB, okA) * (180 / math.Pi)
	if hue < 0 {
		hue += 360
	}
	return swatch{
		r:          red,
		g:          green,
		b:          blue,
		population: population,
		lightness:  okL,
		chroma:     math.Sqrt(okA*okA + okB*okB),
		hue:        hue,
		okL:        okL,
		okA:        okA,
		okB:        okB,
	}
}

func sameRGB(left swatch, right swatch) bool {
	return left.r == right.r && left.g == right.g && left.b == right.b
}

func hueDistanceDegrees(left float64, right float64) float64 {
	delta := math.Abs(left - right)
	if delta > 180 {
		delta = 360 - delta
	}
	return delta
}
