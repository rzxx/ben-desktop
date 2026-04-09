package apitypes

type AppThemeMode string

const (
	AppThemeModeSystem AppThemeMode = "system"
	AppThemeModeLight  AppThemeMode = "light"
	AppThemeModeDark   AppThemeMode = "dark"
)

type ResolvedTheme string

const (
	ResolvedThemeLight ResolvedTheme = "light"
	ResolvedThemeDark  ResolvedTheme = "dark"
)

type ThemePreferences struct {
	Mode      AppThemeMode  `json:"mode"`
	System    ResolvedTheme `json:"system"`
	Effective ResolvedTheme `json:"effective"`
}

func NormalizeAppThemeMode(value AppThemeMode) AppThemeMode {
	switch value {
	case AppThemeModeSystem, AppThemeModeLight, AppThemeModeDark:
		return value
	default:
		return AppThemeModeSystem
	}
}

func NormalizeResolvedTheme(value ResolvedTheme) ResolvedTheme {
	switch value {
	case ResolvedThemeDark:
		return ResolvedThemeDark
	default:
		return ResolvedThemeLight
	}
}

func ResolveTheme(mode AppThemeMode, system ResolvedTheme) ResolvedTheme {
	mode = NormalizeAppThemeMode(mode)
	system = NormalizeResolvedTheme(system)

	switch mode {
	case AppThemeModeDark:
		return ResolvedThemeDark
	case AppThemeModeLight:
		return ResolvedThemeLight
	default:
		return system
	}
}
