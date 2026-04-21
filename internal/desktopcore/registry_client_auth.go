package desktopcore

import (
	"ben/registryauth"
	"context"
	"fmt"
	"strings"
)

type registryMembershipAuth struct {
	RootPublicKey string
	Auth          registryauth.TransportPeerAuth
}

func (a *App) libraryRootPublicKey(ctx context.Context, libraryID string) (string, error) {
	return a.identity.libraryRootPublicKey(ctx, libraryID)
}

func (a *App) registryMembershipAuth(ctx context.Context, libraryID, deviceID, transportPeerID string) (registryMembershipAuth, error) {
	if a == nil {
		return registryMembershipAuth{}, fmt.Errorf("app is nil")
	}
	local, err := a.EnsureLocalContext(ctx)
	if err != nil {
		return registryMembershipAuth{}, err
	}
	if strings.TrimSpace(local.LibraryID) != strings.TrimSpace(libraryID) || strings.TrimSpace(local.DeviceID) != strings.TrimSpace(deviceID) {
		return registryMembershipAuth{}, fmt.Errorf("local context does not match active transport runtime")
	}
	local.PeerID = strings.TrimSpace(transportPeerID)
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, transportPeerID)
	if err != nil {
		return registryMembershipAuth{}, err
	}
	rootPublicKey, err := a.libraryRootPublicKey(ctx, libraryID)
	if err != nil {
		return registryMembershipAuth{}, err
	}
	return registryMembershipAuth{
		RootPublicKey: strings.TrimSpace(rootPublicKey),
		Auth:          auth,
	}, nil
}
