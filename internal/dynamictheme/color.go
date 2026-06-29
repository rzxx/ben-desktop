package dynamictheme

import (
	"fmt"
	"math"
)

type perceptualColor struct {
	r, g, b    uint8
	population int
	l, c, h    float64
	a, labB    float64
	centrality float64
}

func colorFromRGB(r, g, b uint8, population int) perceptualColor {
	l, a, labB := rgbToOKLab(r, g, b)
	h := math.Atan2(labB, a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return perceptualColor{
		r: r, g: g, b: b, population: population,
		l: l, c: math.Hypot(a, labB), h: h, a: a, labB: labB,
	}
}

func gamutMappedOKLCH(lightness, chroma, hue float64) perceptualColor {
	lightness = clamp(lightness, 0, 1)
	chroma = math.Max(0, chroma)
	hue = normalizeHue(hue)

	lo, hi := 0.0, chroma
	for i := 0; i < 18; i++ {
		mid := (lo + hi) / 2
		if oklchInSRGB(lightness, mid, hue) {
			lo = mid
		} else {
			hi = mid
		}
	}

	a := lo * math.Cos(hue*math.Pi/180)
	b := lo * math.Sin(hue*math.Pi/180)
	r, g, blue := okLabToSRGB(lightness, a, b)
	result := colorFromRGB(toByte(r), toByte(g), toByte(blue), 0)
	// Preserve the requested perceptual coordinates after 8-bit conversion.
	result.l, result.c, result.h = lightness, lo, hue
	result.a, result.labB = a, b
	return result
}

func (c perceptualColor) public() Color {
	return Color{
		Hex: fmt.Sprintf("#%02X%02X%02X", c.r, c.g, c.b),
		R:   int(c.r), G: int(c.g), B: int(c.b), Population: c.population,
		Lightness: c.l, Chroma: c.c, Hue: c.h,
	}
}

func oklchInSRGB(l, c, h float64) bool {
	a := c * math.Cos(h*math.Pi/180)
	b := c * math.Sin(h*math.Pi/180)
	r, g, blue := okLabToSRGB(l, a, b)
	return r >= 0 && r <= 1 && g >= 0 && g <= 1 && blue >= 0 && blue <= 1
}

func rgbToOKLab(red, green, blue uint8) (float64, float64, float64) {
	r := srgbToLinear(float64(red) / 255)
	g := srgbToLinear(float64(green) / 255)
	b := srgbToLinear(float64(blue) / 255)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return 0.2104542553*l + 0.793617785*m - 0.0040720468*s,
		1.9779984951*l - 2.428592205*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.808675766*s
}

func okLabToSRGB(l, a, b float64) (float64, float64, float64) {
	lRoot := l + 0.3963377774*a + 0.2158037573*b
	mRoot := l - 0.1055613458*a - 0.0638541728*b
	sRoot := l - 0.0894841775*a - 1.291485548*b
	lLinear, mLinear, sLinear := lRoot*lRoot*lRoot, mRoot*mRoot*mRoot, sRoot*sRoot*sRoot
	r := 4.0767416621*lLinear - 3.3077115913*mLinear + 0.2309699292*sLinear
	g := -1.2684380046*lLinear + 2.6097574011*mLinear - 0.3413193965*sLinear
	blue := -0.0041960863*lLinear - 0.7034186147*mLinear + 1.707614701*sLinear
	return linearToSRGB(r), linearToSRGB(g), linearToSRGB(blue)
}

func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func toByte(v float64) uint8             { return uint8(math.Round(clamp(v, 0, 1) * 255)) }
func clamp(v, low, high float64) float64 { return math.Max(low, math.Min(high, v)) }
func normalizeHue(h float64) float64 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	return h
}
func hueDistance(a, b float64) float64 {
	d := math.Abs(normalizeHue(a) - normalizeHue(b))
	if d > 180 {
		return 360 - d
	}
	return d
}
func colorDistance(a, b perceptualColor) float64 {
	return math.Sqrt((a.l-b.l)*(a.l-b.l) + (a.a-b.a)*(a.a-b.a) + (a.labB-b.labB)*(a.labB-b.labB))
}

func relativeLuminance(c perceptualColor) float64 {
	return 0.2126*srgbToLinear(float64(c.r)/255) + 0.7152*srgbToLinear(float64(c.g)/255) + 0.0722*srgbToLinear(float64(c.b)/255)
}

func contrastRatio(a, b perceptualColor) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
