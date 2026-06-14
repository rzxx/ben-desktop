package desktopcore

import (
	"context"
	"strings"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/observability"
)

func (a *App) recordNetworkEvent(entry apitypes.NetworkTraceEvent) {
	if a == nil {
		return
	}
	attrs := []observability.Attr{
		observability.String("service", "network"),
		observability.String("component", "desktopcore"),
	}
	if entry.Level != "" {
		attrs = append(attrs, observability.String("level", entry.Level))
	}
	if entry.Message != "" {
		attrs = append(attrs, observability.String("message", entry.Message))
	}
	if entry.LibraryID != "" {
		attrs = append(attrs, observability.String("ben.library_id", entry.LibraryID))
	}
	if entry.DeviceID != "" {
		attrs = append(attrs, observability.String("ben.device_id", entry.DeviceID))
	}
	if entry.PeerID != "" {
		attrs = append(attrs, observability.String("ben.peer_id", entry.PeerID))
	}
	if entry.Address != "" {
		attrs = append(attrs, observability.String("net.peer.address", entry.Address))
	}
	if entry.ConnectionKind != "" {
		attrs = append(attrs, observability.String("ben.connection_kind", entry.ConnectionKind))
	}
	if entry.DirectUpgradeState != "" {
		attrs = append(attrs, observability.String("ben.direct_upgrade_state", entry.DirectUpgradeState))
	}
	if entry.Reason != "" {
		attrs = append(attrs, observability.String("reason", entry.Reason))
	}
	if entry.Error != "" {
		attrs = append(attrs, observability.String("error", entry.Error))
	}

	name := strings.TrimSpace(entry.Kind)
	if name == "" {
		name = "network.event"
	} else if !strings.HasPrefix(name, "network.") && !strings.HasPrefix(name, "transport.") {
		name = "network." + name
	}
	observability.Default().Event(context.Background(), name, attrs...)
}
