//go:build !windows

package winappupdater

import "log/slog"

// MaybeHandle is a no-op stub on non-Windows platforms.
func MaybeHandle(_ *slog.Logger) {}
