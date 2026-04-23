//go:build windows

package platform

import (
	"embed"
	"encoding/binary"
	"fmt"
	"path"

	"github.com/zzl/go-win32api/v2/win32"
)

const (
	thumbbarIconDesiredSize = 16
	thumbbarIconVersion     = 0x00030000
	thumbbarIconThemeDark   = "dark"
	thumbbarIconThemeLight  = "light"
)

//go:embed thumbbar/light/*.ico thumbbar/dark/*.ico
var thumbbarIconFS embed.FS

type thumbbarIconSet struct {
	previous win32.HICON
	play     win32.HICON
	pause    win32.HICON
	next     win32.HICON
}

func loadThumbbarIconSet() (thumbbarIconSet, error) {
	theme := preferredThumbbarIconTheme()
	icons, err := loadThumbbarIconTheme(theme)
	if err == nil {
		return icons, nil
	}

	if theme == thumbbarIconThemeDark {
		return thumbbarIconSet{}, err
	}

	fallback, fallbackErr := loadThumbbarIconTheme(thumbbarIconThemeDark)
	if fallbackErr == nil {
		return fallback, nil
	}
	return thumbbarIconSet{}, fmt.Errorf("%v; dark fallback failed: %w", err, fallbackErr)
}

func preferredThumbbarIconTheme() string {
	if CurrentSystemTheme() == "light" {
		return thumbbarIconThemeLight
	}
	return thumbbarIconThemeDark
}

func loadThumbbarIconTheme(theme string) (thumbbarIconSet, error) {
	icons := thumbbarIconSet{}

	var err error
	if icons.previous, err = loadEmbeddedThumbbarIcon(theme, "previous.ico"); err != nil {
		return thumbbarIconSet{}, err
	}
	if icons.play, err = loadEmbeddedThumbbarIcon(theme, "play.ico"); err != nil {
		destroyThumbbarIconSet(&icons)
		return thumbbarIconSet{}, err
	}
	if icons.pause, err = loadEmbeddedThumbbarIcon(theme, "pause.ico"); err != nil {
		destroyThumbbarIconSet(&icons)
		return thumbbarIconSet{}, err
	}
	if icons.next, err = loadEmbeddedThumbbarIcon(theme, "next.ico"); err != nil {
		destroyThumbbarIconSet(&icons)
		return thumbbarIconSet{}, err
	}

	return icons, nil
}

func loadEmbeddedThumbbarIcon(theme string, name string) (win32.HICON, error) {
	assetPath := path.Join("thumbbar", theme, name)
	data, err := thumbbarIconFS.ReadFile(assetPath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", assetPath, err)
	}

	imageData, width, height, err := extractICOImageData(data, thumbbarIconDesiredSize)
	if err != nil {
		return 0, fmt.Errorf("decode %s: %w", assetPath, err)
	}

	icon, winErr := win32.CreateIconFromResourceEx(
		&imageData[0],
		uint32(len(imageData)),
		win32.BOOL(1),
		thumbbarIconVersion,
		int32(width),
		int32(height),
		0,
	)
	if icon == 0 {
		if winErr != 0 {
			return 0, fmt.Errorf("CreateIconFromResourceEx %s: %v", assetPath, winErr)
		}
		return 0, fmt.Errorf("CreateIconFromResourceEx %s: returned nil icon", assetPath)
	}
	return icon, nil
}

func destroyThumbbarIconSet(icons *thumbbarIconSet) {
	if icons == nil {
		return
	}

	if icons.previous != 0 {
		_, _ = win32.DestroyIcon(icons.previous)
		icons.previous = 0
	}
	if icons.play != 0 {
		_, _ = win32.DestroyIcon(icons.play)
		icons.play = 0
	}
	if icons.pause != 0 {
		_, _ = win32.DestroyIcon(icons.pause)
		icons.pause = 0
	}
	if icons.next != 0 {
		_, _ = win32.DestroyIcon(icons.next)
		icons.next = 0
	}
}

func extractICOImageData(data []byte, desiredSize int) ([]byte, int, int, error) {
	if len(data) < 6 {
		return nil, 0, 0, fmt.Errorf("icon file is too small")
	}

	if binary.LittleEndian.Uint16(data[0:2]) != 0 {
		return nil, 0, 0, fmt.Errorf("unexpected reserved header")
	}
	if binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, 0, 0, fmt.Errorf("not an icon file")
	}

	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count <= 0 {
		return nil, 0, 0, fmt.Errorf("icon file has no images")
	}

	dirSize := 6 + count*16
	if len(data) < dirSize {
		return nil, 0, 0, fmt.Errorf("icon directory is truncated")
	}

	bestOffset := uint32(0)
	bestSize := uint32(0)
	bestWidth := 0
	bestHeight := 0
	bestScore := 0
	found := false

	for i := 0; i < count; i++ {
		entryOffset := 6 + i*16
		width := iconDimension(data[entryOffset])
		height := iconDimension(data[entryOffset+1])
		imageSize := binary.LittleEndian.Uint32(data[entryOffset+8 : entryOffset+12])
		imageOffset := binary.LittleEndian.Uint32(data[entryOffset+12 : entryOffset+16])

		endOffset := int(imageOffset + imageSize)
		if imageSize == 0 || endOffset > len(data) {
			return nil, 0, 0, fmt.Errorf("icon image %d is truncated", i)
		}

		score := absInt(width-desiredSize) + absInt(height-desiredSize)
		if !found || score < bestScore || (score == bestScore && width*height > bestWidth*bestHeight) {
			bestOffset = imageOffset
			bestSize = imageSize
			bestWidth = width
			bestHeight = height
			bestScore = score
			found = true
		}
	}

	if !found {
		return nil, 0, 0, fmt.Errorf("icon file has no usable images")
	}

	imageData := data[int(bestOffset):int(bestOffset+bestSize)]
	if len(imageData) == 0 {
		return nil, 0, 0, fmt.Errorf("icon image is empty")
	}
	return imageData, bestWidth, bestHeight, nil
}

func iconDimension(value byte) int {
	if value == 0 {
		return 256
	}
	return int(value)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
