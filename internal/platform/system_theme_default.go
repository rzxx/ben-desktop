//go:build !windows

package platform

import apitypes "ben/desktop/api/types"

func CurrentSystemTheme() apitypes.ResolvedTheme {
	return apitypes.ResolvedThemeLight
}
