package desktopcore

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	taglib "go.senan.xyz/taglib"
)

type TagReader interface {
	Read(path string) (Tags, error)
}

type Tags struct {
	Title       string
	Album       string
	AlbumArtist string
	Artists     []string
	TrackNo     int
	DiscNo      int
	Year        int
	DurationMS  int64
	Container   string
	Codec       string
	Bitrate     int
	SampleRate  int
	Channels    int
	IsLossless  bool
	QualityRank int
}

type taglibReader struct{}

func NewTagReader() TagReader {
	return taglibReader{}
}

func (taglibReader) Read(path string) (Tags, error) {
	rawTags, err := taglib.ReadTags(path)
	if err != nil {
		return Tags{}, err
	}

	props, err := taglib.ReadProperties(path)
	if err != nil {
		return Tags{}, err
	}

	artists := firstTagList(rawTags, taglib.Artists)
	if len(artists) == 0 {
		artists = splitDelimitedArtists(firstTag(rawTags, taglib.Artist))
	}

	tags := Tags{
		Title:       firstTag(rawTags, taglib.Title),
		Album:       firstTag(rawTags, taglib.Album),
		AlbumArtist: firstTag(rawTags, taglib.AlbumArtist),
		Artists:     artists,
		TrackNo:     parseTagNumber(firstTag(rawTags, taglib.TrackNumber)),
		DiscNo:      parseTagNumber(firstTag(rawTags, taglib.DiscNumber)),
		Year:        parseTagYear(rawTags),
		DurationMS:  props.Length.Milliseconds(),
		Container:   fileContainer(path),
		Codec:       "unknown",
		Bitrate:     int(props.Bitrate) * 1000,
		SampleRate:  int(props.SampleRate),
		Channels:    int(props.Channels),
	}
	if tags.AlbumArtist == "" && len(tags.Artists) > 0 {
		tags.AlbumArtist = tags.Artists[0]
	}
	tags.IsLossless = inferLossless(tags.Container, tags.Codec)
	tags.QualityRank = computeQualityRank(tags)
	return sanitizeTags(path, tags)
}

func fileContainer(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return "unknown"
	}
	return ext
}

func sanitizeTags(path string, tags Tags) (Tags, error) {
	tags.Title = strings.TrimSpace(tags.Title)
	tags.Album = strings.TrimSpace(tags.Album)
	tags.AlbumArtist = strings.TrimSpace(tags.AlbumArtist)
	if tags.Container == "" {
		tags.Container = fileContainer(path)
	}

	if len(tags.Artists) == 0 {
		if tags.AlbumArtist != "" {
			tags.Artists = []string{tags.AlbumArtist}
		} else {
			tags.Artists = []string{"Unknown Artist"}
		}
	}
	if tags.Title == "" {
		base := filepath.Base(path)
		tags.Title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if tags.Album == "" {
		tags.Album = "Unknown Album"
	}
	if tags.DurationMS < 0 {
		return Tags{}, fmt.Errorf("negative duration")
	}
	if tags.DurationMS == 0 {
		return Tags{}, fmt.Errorf("duration missing")
	}
	return tags, nil
}

func firstTag(tags map[string][]string, key string) string {
	values := tags[key]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func firstTagList(tags map[string][]string, key string) []string {
	values := tags[key]
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, splitDelimitedArtists(value)...)
	}
	return compactNonEmptyStrings(out)
}

func splitDelimitedArtists(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ';', ',', '/':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{value}
	}
	return out
}

func parseTagYear(tags map[string][]string) int {
	value := firstTag(tags, taglib.Date)
	if value == "" {
		return 0
	}
	if len(value) >= 4 {
		if year, err := strconv.Atoi(value[:4]); err == nil {
			return year
		}
	}
	return parseTagNumber(value)
}

func parseTagNumber(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if before, _, ok := strings.Cut(value, "/"); ok {
		value = before
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func inferLossless(container, codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "flac", "alac", "wav", "pcm", "aiff":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(container)) {
	case "flac", "wav", "aiff":
		return true
	default:
		return false
	}
}

func computeQualityRank(tags Tags) int {
	score := 0
	if tags.IsLossless {
		score += 1_000_000
	}
	score += tags.SampleRate * 10
	score += tags.Channels * 1_000
	score += tags.Bitrate / 100
	return score
}
