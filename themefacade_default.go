//go:build !windows

package main

import (
	apitypes "ben/desktop/api/types"
)

func detectSystemTheme() apitypes.ResolvedTheme {
	return apitypes.ResolvedThemeLight
}

func (s *ThemeFacade) startSystemThemeMonitor() {
	s.mu.Lock()
	s.stopThemeMonitoring = nil
	s.mu.Unlock()
}
