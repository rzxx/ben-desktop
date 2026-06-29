package dynamictheme

import "math"

var compatibilityTones = []int{50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950}
var compatibilityLightness = []float64{.985, .97, .922, .87, .708, .556, .439, .371, .269, .205, .145}

func resolveTheme(identity ArtworkIdentity) Theme {
	atmosphere := fromPublic(identity.Atmosphere)
	accent := atmosphere
	if identity.Accent != nil {
		accent = fromPublic(*identity.Accent)
	}
	if identity.Class == ArtworkClassNeutral {
		atmosphere.c = 0
		accent.c = 0
	}
	return Theme{Version: 1, Artwork: identity, Light: resolveScheme(false, atmosphere, accent), Dark: resolveScheme(true, atmosphere, accent), Compatibility: resolveCompatibility(atmosphere, accent)}
}

func resolveScheme(dark bool, atmosphere, accentSeed perceptualColor) Scheme {
	h := atmosphere.h
	surfaceChroma := minPositive(atmosphere.c*.12, .012)
	accentChroma := minPositive(accentSeed.c, .17)
	if dark {
		surfaceChroma = minPositive(atmosphere.c*.10, .010)
		accentChroma = minPositive(accentSeed.c, .145)
	}
	makeSurface := func(l, cScale float64) Color { return gamutMappedOKLCH(l, surfaceChroma*cScale, h).public() }
	makeContent := func(l, c float64) Color { return gamutMappedOKLCH(l, minPositive(atmosphere.c*c, .018), h).public() }
	makeAccent := func(l, cScale float64) perceptualColor { return gamutMappedOKLCH(l, accentChroma*cScale, accentSeed.h) }
	var scheme Scheme
	if !dark {
		scheme.Surface = SurfaceRoles{Canvas: makeSurface(.965, .75), Recessed: makeSurface(.945, .9), Default: makeSurface(.98, .65), Raised: makeSurface(.99, .45), Overlay: makeSurface(.995, .3)}
		scheme.Content = ContentRoles{Primary: makeContent(.18, .18), Secondary: makeContent(.42, .14), Disabled: makeContent(.62, .08), Inverse: makeContent(.97, .05)}
		scheme.Border = BorderRoles{Subtle: makeSurface(.86, 1), Default: makeSurface(.75, 1), Strong: makeSurface(.58, 1)}
		a, hover, pressed, subtle := makeAccent(.48, 1), makeAccent(.44, .95), makeAccent(.40, .9), makeAccent(.92, .28)
		on := chooseOnColor([]perceptualColor{a, hover, pressed})
		scheme.Accent = AccentRoles{Default: a.public(), Hover: hover.public(), Pressed: pressed.public(), Subtle: subtle.public(), OnAccent: on.public()}
		scheme.Border.Focus = a.public()
		scheme.Status = resolveStatuses(false)
	} else {
		scheme.Surface = SurfaceRoles{Canvas: makeSurface(.13, .8), Recessed: makeSurface(.105, .65), Default: makeSurface(.17, .9), Raised: makeSurface(.205, 1), Overlay: makeSurface(.24, 1)}
		scheme.Content = ContentRoles{Primary: makeContent(.94, .12), Secondary: makeContent(.72, .12), Disabled: makeContent(.49, .08), Inverse: makeContent(.16, .08)}
		scheme.Border = BorderRoles{Subtle: makeSurface(.28, 1), Default: makeSurface(.39, 1), Strong: makeSurface(.56, 1)}
		a, hover, pressed, subtle := makeAccent(.74, 1), makeAccent(.78, .95), makeAccent(.82, .9), makeAccent(.27, .3)
		on := chooseOnColor([]perceptualColor{a, hover, pressed})
		scheme.Accent = AccentRoles{Default: a.public(), Hover: hover.public(), Pressed: pressed.public(), Subtle: subtle.public(), OnAccent: on.public()}
		scheme.Border.Focus = a.public()
		scheme.Status = resolveStatuses(true)
	}
	return scheme
}

func resolveStatuses(dark bool) StatusRoles {
	makeStatus := func(h float64) StatusRole {
		l, subtleL := .48, .93
		if dark {
			l, subtleL = .72, .25
		}
		base := gamutMappedOKLCH(l, .17, h)
		subtle := gamutMappedOKLCH(subtleL, .045, h)
		on := chooseOnColor([]perceptualColor{base})
		return StatusRole{Default: base.public(), Subtle: subtle.public(), On: on.public()}
	}
	return StatusRoles{Danger: makeStatus(25), Warning: makeStatus(80), Success: makeStatus(145)}
}

func chooseOnColor(backgrounds []perceptualColor) perceptualColor {
	light := gamutMappedOKLCH(.98, .005, 0)
	dark := gamutMappedOKLCH(.12, .005, 0)
	minLight, minDark := 100.0, 100.0
	for _, background := range backgrounds {
		if ratio := contrastRatio(light, background); ratio < minLight {
			minLight = ratio
		}
		if ratio := contrastRatio(dark, background); ratio < minDark {
			minDark = ratio
		}
	}
	if minLight >= minDark {
		return light
	}
	return dark
}

func resolveCompatibility(atmosphere, accent perceptualColor) CompatibilityPalette {
	theme := make([]PaletteTone, 0, len(compatibilityTones))
	accentScale := make([]PaletteTone, 0, len(compatibilityTones))
	for i, tone := range compatibilityTones {
		l := compatibilityLightness[i]
		surfaceC := minPositive(atmosphere.c*.14, .014)
		accentC := minPositive(accent.c, .17)
		edge := 1 - abs(l-.56)/.56
		if edge < .16 {
			edge = .16
		}
		theme = append(theme, PaletteTone{Tone: tone, Color: gamutMappedOKLCH(l, surfaceC, atmosphere.h).public()})
		accentScale = append(accentScale, PaletteTone{Tone: tone, Color: gamutMappedOKLCH(l, accentC*edge, accent.h).public()})
	}
	return CompatibilityPalette{ThemeScale: theme, AccentScale: accentScale}
}

func fromPublic(value Color) perceptualColor {
	c := colorFromRGB(uint8(value.R), uint8(value.G), uint8(value.B), value.Population)
	c.l = value.Lightness
	c.c = value.Chroma
	c.h = value.Hue
	c.a = c.c * math.Cos(c.h*math.Pi/180)
	c.labB = c.c * math.Sin(c.h*math.Pi/180)
	return c
}
func minPositive(value, maximum float64) float64 {
	if value < 0 {
		return 0
	}
	if value > maximum {
		return maximum
	}
	return value
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
