package dynamictheme

// Theme is the versioned, component-facing result of artwork analysis.
// Foundations are intentionally kept private; callers consume semantic roles.
type Theme struct {
	Version       int                  `json:"version"`
	Artwork       ArtworkIdentity      `json:"artwork"`
	Light         Scheme               `json:"light"`
	Dark          Scheme               `json:"dark"`
	Compatibility CompatibilityPalette `json:"compatibility"`
}

type ArtworkClass string

const (
	ArtworkClassNeutral    ArtworkClass = "neutral"
	ArtworkClassSingleHue  ArtworkClass = "single-hue"
	ArtworkClassMulticolor ArtworkClass = "multicolor"
)

type ArtworkIdentity struct {
	Class      ArtworkClass       `json:"class"`
	Atmosphere Color              `json:"atmosphere"`
	Accent     *Color             `json:"accent,omitempty"`
	Candidates []ArtworkCandidate `json:"candidates"`
}

type ArtworkCandidate struct {
	Color      Color   `json:"color"`
	Share      float64 `json:"share"`
	Centrality float64 `json:"centrality"`
}

type Scheme struct {
	Surface SurfaceRoles `json:"surface"`
	Content ContentRoles `json:"content"`
	Border  BorderRoles  `json:"border"`
	Accent  AccentRoles  `json:"accent"`
	Status  StatusRoles  `json:"status"`
}

type SurfaceRoles struct {
	Canvas   Color `json:"canvas"`
	Recessed Color `json:"recessed"`
	Default  Color `json:"default"`
	Raised   Color `json:"raised"`
	Overlay  Color `json:"overlay"`
}

type ContentRoles struct {
	Primary   Color `json:"primary"`
	Secondary Color `json:"secondary"`
	Disabled  Color `json:"disabled"`
	Inverse   Color `json:"inverse"`
}

type BorderRoles struct {
	Subtle  Color `json:"subtle"`
	Default Color `json:"default"`
	Strong  Color `json:"strong"`
	Focus   Color `json:"focus"`
}

type AccentRoles struct {
	Default  Color `json:"default"`
	Hover    Color `json:"hover"`
	Pressed  Color `json:"pressed"`
	Subtle   Color `json:"subtle"`
	OnAccent Color `json:"onAccent"`
}

type StatusRoles struct {
	Danger  StatusRole `json:"danger"`
	Warning StatusRole `json:"warning"`
	Success StatusRole `json:"success"`
}

type StatusRole struct {
	Default Color `json:"default"`
	Subtle  Color `json:"subtle"`
	On      Color `json:"on"`
}

// CompatibilityPalette is a temporary bridge for the pre-token UI. New UI
// must not consume it.
type CompatibilityPalette struct {
	ThemeScale  []PaletteTone `json:"themeScale"`
	AccentScale []PaletteTone `json:"accentScale"`
}

type PaletteTone struct {
	Tone  int   `json:"tone"`
	Color Color `json:"color"`
}

type Color struct {
	Hex        string  `json:"hex"`
	R          int     `json:"r"`
	G          int     `json:"g"`
	B          int     `json:"b"`
	Population int     `json:"population,omitempty"`
	Lightness  float64 `json:"lightness"`
	Chroma     float64 `json:"chroma"`
	Hue        float64 `json:"hue"`
}
