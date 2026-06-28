package dynamictheme

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"runtime"
	"sort"
	"sync"

	"golang.org/x/image/webp"
)

const (
	defaultCandidateCount = 32
	maxWorkerCount        = 8
)

type ExtractOptions struct {
	Quality          int `json:"quality"`
	CandidateCount   int `json:"candidateCount"`
	QuantizationBits int `json:"quantizationBits"`
	AlphaThreshold   int `json:"alphaThreshold"`
	WorkerCount      int `json:"workerCount"`
}

var defaultExtractOptions = ExtractOptions{Quality: 2, CandidateCount: defaultCandidateCount, QuantizationBits: 5, AlphaThreshold: 16}

type Extractor struct{}

func NewExtractor() *Extractor                                      { return &Extractor{} }
func DefaultExtractOptions() ExtractOptions                         { return defaultExtractOptions }
func NormalizeExtractOptions(options ExtractOptions) ExtractOptions { return options.normalized() }

func (e *Extractor) ExtractFromPath(path string, options ExtractOptions) (Theme, error) {
	file, err := os.Open(path)
	if err != nil {
		return Theme{}, fmt.Errorf("open image: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoded, err := webp.Decode(file)
	if err != nil {
		return Theme{}, fmt.Errorf("decode image: %w", err)
	}
	return e.ExtractFromImage(decoded, options)
}

func (e *Extractor) ExtractFromImage(img image.Image, options ExtractOptions) (Theme, error) {
	if img == nil || img.Bounds().Empty() {
		return Theme{}, errors.New("image has no pixels")
	}
	candidates, err := observeArtwork(toNRGBA(img), options.normalized())
	if err != nil {
		return Theme{}, err
	}
	identity := selectArtworkIdentity(candidates)
	return resolveTheme(identity), nil
}

func (o ExtractOptions) normalized() ExtractOptions {
	if o.Quality <= 0 {
		o.Quality = defaultExtractOptions.Quality
	}
	if o.CandidateCount <= 0 {
		o.CandidateCount = defaultExtractOptions.CandidateCount
	}
	if o.QuantizationBits <= 0 {
		o.QuantizationBits = defaultExtractOptions.QuantizationBits
	}
	o.Quality = int(clamp(float64(o.Quality), 1, 12))
	o.CandidateCount = int(clamp(float64(o.CandidateCount), 8, 64))
	o.QuantizationBits = int(clamp(float64(o.QuantizationBits), 4, 6))
	o.AlphaThreshold = int(clamp(float64(o.AlphaThreshold), 0, 254))
	if o.WorkerCount <= 0 {
		o.WorkerCount = runtime.GOMAXPROCS(0) - 1
	}
	o.WorkerCount = int(clamp(float64(o.WorkerCount), 1, float64(maxWorkerCount)))
	return o
}

type histogramBin struct {
	count, rSum, gSum, bSum int
	centralitySum           float64
}

type quantizedBin struct {
	key, count int
	r, g, b    uint8
	centrality float64
}

type colorBox struct {
	bins               []quantizedBin
	population, volume int
}

func toNRGBA(img image.Image) *image.NRGBA {
	bounds := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), img, bounds.Min, draw.Src)
	return dst
}

func observeArtwork(img *image.NRGBA, options ExtractOptions) ([]perceptualColor, error) {
	bins, total, err := buildHistogram(img, options)
	if err != nil {
		return nil, err
	}
	boxes := quantize(bins, options.CandidateCount, options.QuantizationBits)
	colors := make([]perceptualColor, 0, len(boxes))
	for _, box := range boxes {
		if box.population == 0 {
			continue
		}
		var r, g, b, central float64
		for _, bin := range box.bins {
			weight := float64(bin.count)
			r += float64(bin.r) * weight
			g += float64(bin.g) * weight
			b += float64(bin.b) * weight
			central += bin.centrality * weight
		}
		color := colorFromRGB(uint8(math.Round(r/float64(box.population))), uint8(math.Round(g/float64(box.population))), uint8(math.Round(b/float64(box.population))), box.population)
		color.centrality = central / float64(box.population)
		colors = append(colors, color)
	}
	sort.Slice(colors, func(i, j int) bool { return colors[i].population > colors[j].population })
	return deduplicate(colors, total), nil
}

func buildHistogram(img *image.NRGBA, options ExtractOptions) ([]quantizedBin, int, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	workers := options.WorkerCount
	if workers > h {
		workers = h
	}
	size := 1 << (options.QuantizationBits * 3)
	locals := make([][]histogramBin, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start, end := h*worker/workers, h*(worker+1)/workers
		wg.Add(1)
		go func(index, yStart, yEnd int) {
			defer wg.Done()
			local := make([]histogramBin, size)
			for y := yStart; y < yEnd; y++ {
				if y%options.Quality != 0 {
					continue
				}
				for x := 0; x < w; x += options.Quality {
					offset := y*img.Stride + x*4
					if int(img.Pix[offset+3]) <= options.AlphaThreshold {
						continue
					}
					r, g, b := int(img.Pix[offset]), int(img.Pix[offset+1]), int(img.Pix[offset+2])
					shift := 8 - options.QuantizationBits
					key := ((r >> shift) << (options.QuantizationBits * 2)) | ((g >> shift) << options.QuantizationBits) | (b >> shift)
					dx, dy := (float64(x)+0.5)/float64(w)-0.5, (float64(y)+0.5)/float64(h)-0.5
					centrality := 1 - clamp(math.Hypot(dx, dy)/0.70710678, 0, 1)
					entry := &local[key]
					entry.count++
					entry.rSum += r
					entry.gSum += g
					entry.bSum += b
					entry.centralitySum += centrality
				}
			}
			locals[index] = local
		}(worker, start, end)
	}
	wg.Wait()
	merged := make([]histogramBin, size)
	total := 0
	for _, local := range locals {
		for i, value := range local {
			if value.count == 0 {
				continue
			}
			merged[i].count += value.count
			merged[i].rSum += value.rSum
			merged[i].gSum += value.gSum
			merged[i].bSum += value.bSum
			merged[i].centralitySum += value.centralitySum
			total += value.count
		}
	}
	if total == 0 {
		return nil, 0, errors.New("image has no visible pixels")
	}
	result := make([]quantizedBin, 0)
	for key, value := range merged {
		if value.count > 0 {
			result = append(result, quantizedBin{key: key, count: value.count, r: uint8(value.rSum / value.count), g: uint8(value.gSum / value.count), b: uint8(value.bSum / value.count), centrality: value.centralitySum / float64(value.count)})
		}
	}
	return result, total, nil
}

func quantize(bins []quantizedBin, target, bits int) []colorBox {
	boxes := []colorBox{newColorBox(bins, bits)}
	for len(boxes) < target {
		best := -1
		bestScore := -1.0
		for i, box := range boxes {
			if len(box.bins) > 1 {
				score := float64(box.population) * math.Log1p(float64(box.volume))
				if score > bestScore {
					best, bestScore = i, score
				}
			}
		}
		if best < 0 {
			break
		}
		left, right, ok := splitBox(boxes[best], bits)
		if !ok {
			break
		}
		boxes[best] = boxes[len(boxes)-1]
		boxes = boxes[:len(boxes)-1]
		boxes = append(boxes, left, right)
	}
	return boxes
}

func newColorBox(bins []quantizedBin, bits int) colorBox {
	box := colorBox{bins: bins}
	if len(bins) == 0 {
		return box
	}
	minR, maxR, minG, maxG, minB, maxB := 1<<bits, 0, 1<<bits, 0, 1<<bits, 0
	mask := (1 << bits) - 1
	for _, bin := range bins {
		r := (bin.key >> (bits * 2)) & mask
		g := (bin.key >> bits) & mask
		b := bin.key & mask
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
		if g < minG {
			minG = g
		}
		if g > maxG {
			maxG = g
		}
		if b < minB {
			minB = b
		}
		if b > maxB {
			maxB = b
		}
		box.population += bin.count
	}
	box.volume = (maxR - minR + 1) * (maxG - minG + 1) * (maxB - minB + 1)
	return box
}

func splitBox(box colorBox, bits int) (colorBox, colorBox, bool) {
	if len(box.bins) < 2 {
		return colorBox{}, colorBox{}, false
	}
	mask := (1 << bits) - 1
	rangeFor := func(axis int) int {
		min, max := 1<<bits, 0
		for _, bin := range box.bins {
			v := 0
			if axis == 0 {
				v = (bin.key >> (bits * 2)) & mask
			} else if axis == 1 {
				v = (bin.key >> bits) & mask
			} else {
				v = bin.key & mask
			}
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		return max - min
	}
	axis := 0
	if rangeFor(1) > rangeFor(axis) {
		axis = 1
	}
	if rangeFor(2) > rangeFor(axis) {
		axis = 2
	}
	ordered := append([]quantizedBin(nil), box.bins...)
	sort.Slice(ordered, func(i, j int) bool {
		value := func(bin quantizedBin) int {
			if axis == 0 {
				return (bin.key >> (bits * 2)) & mask
			}
			if axis == 1 {
				return (bin.key >> bits) & mask
			}
			return bin.key & mask
		}
		return value(ordered[i]) < value(ordered[j])
	})
	half, cumulative, at := box.population/2, 0, 0
	for i, bin := range ordered {
		cumulative += bin.count
		if cumulative >= half {
			at = i + 1
			break
		}
	}
	if at <= 0 || at >= len(ordered) {
		at = len(ordered) / 2
	}
	if at <= 0 || at >= len(ordered) {
		return colorBox{}, colorBox{}, false
	}
	return newColorBox(append([]quantizedBin(nil), ordered[:at]...), bits), newColorBox(append([]quantizedBin(nil), ordered[at:]...), bits), true
}

func deduplicate(colors []perceptualColor, total int) []perceptualColor {
	result := make([]perceptualColor, 0, len(colors))
	for _, candidate := range colors {
		merged := false
		for i := range result {
			if colorDistance(candidate, result[i]) < 0.025 {
				combined := candidate.population + result[i].population
				result[i].centrality = (result[i].centrality*float64(result[i].population) + candidate.centrality*float64(candidate.population)) / float64(combined)
				result[i].population = combined
				merged = true
				break
			}
		}
		if !merged {
			result = append(result, candidate)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].population > result[j].population })
	_ = total
	return result
}

func selectArtworkIdentity(candidates []perceptualColor) ArtworkIdentity {
	total := 0
	for _, candidate := range candidates {
		total += candidate.population
	}
	atmosphere := candidates[0]
	best := -1.0
	for _, candidate := range candidates {
		share := float64(candidate.population) / float64(total)
		toneFit := 1 - clamp(math.Abs(candidate.l-0.58)/0.58, 0, 1)
		chromaFit := 1 - clamp(math.Abs(candidate.c-0.10)/0.22, 0, 1)
		score := 0.68*math.Sqrt(share) + 0.14*candidate.centrality + 0.10*toneFit + 0.08*chromaFit
		if score > best {
			best = score
			atmosphere = candidate
		}
	}
	class := classifyArtwork(candidates, total)
	if class == ArtworkClassNeutral {
		atmosphere = gamutMappedOKLCH(atmosphere.l, 0, 0)
		atmosphere.population = candidates[0].population
	}
	accent, hasAccent := selectAccent(atmosphere, candidates, total, class)
	publicCandidates := make([]ArtworkCandidate, 0, len(candidates))
	for i, candidate := range candidates {
		if i >= 12 {
			break
		}
		publicCandidates = append(publicCandidates, ArtworkCandidate{Color: candidate.public(), Share: float64(candidate.population) / float64(total), Centrality: candidate.centrality})
	}
	identity := ArtworkIdentity{Class: class, Atmosphere: atmosphere.public(), Candidates: publicCandidates}
	if hasAccent {
		value := accent.public()
		identity.Accent = &value
	}
	return identity
}

func classifyArtwork(candidates []perceptualColor, total int) ArtworkClass {
	chromatic := make([]perceptualColor, 0)
	weightedChroma, colorShare := 0.0, 0.0
	for _, candidate := range candidates {
		share := float64(candidate.population) / float64(total)
		weightedChroma += candidate.c * share
		if candidate.c >= 0.04 && (share >= 0.002 || candidate.centrality >= 0.55) {
			chromatic = append(chromatic, candidate)
			colorShare += share
		}
	}
	if weightedChroma < 0.025 && colorShare < 0.002 {
		return ArtworkClassNeutral
	}
	// A deliberate chromatic mark on a predominantly neutral cover is a real
	// two-family composition even though only one chromatic hue is present.
	if colorShare >= 0.002 && colorShare <= 0.45 {
		return ArtworkClassMulticolor
	}
	for i := range chromatic {
		for j := i + 1; j < len(chromatic); j++ {
			if hueDistance(chromatic[i].h, chromatic[j].h) >= 30 {
				return ArtworkClassMulticolor
			}
		}
	}
	return ArtworkClassSingleHue
}

func selectAccent(atmosphere perceptualColor, candidates []perceptualColor, total int, class ArtworkClass) (perceptualColor, bool) {
	if class == ArtworkClassNeutral {
		return perceptualColor{}, false
	}
	if class == ArtworkClassSingleHue {
		return perceptualColor{}, false
	}
	bestScore := -1.0
	var best perceptualColor
	for _, candidate := range candidates {
		share := float64(candidate.population) / float64(total)
		if candidate.c < 0.035 || share < 0.001 {
			continue
		}
		distance := colorDistance(atmosphere, candidate)
		hueDelta := hueDistance(atmosphere.h, candidate.h)
		if distance < 0.055 || hueDelta < 24 {
			continue
		}
		support := clamp(math.Sqrt(share/0.12), 0, 1)
		distinct := clamp(distance/0.22, 0, 1)
		chroma := clamp(candidate.c/0.18, 0, 1)
		tone := 1 - clamp(math.Abs(candidate.l-0.60)/0.45, 0, 1)
		score := 0.26*support + 0.27*distinct + 0.20*chroma + 0.19*candidate.centrality + 0.08*tone
		if candidate.centrality < 0.08 && share < 0.02 {
			score *= 0.65
		}
		if score > bestScore {
			bestScore = score
			best = candidate
		}
	}
	return best, bestScore >= 0.39
}
