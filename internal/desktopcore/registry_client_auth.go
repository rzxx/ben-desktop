package desktopcore

import (
	"ben/registryauth"
	"context"
	"fmt"
	"strings"
	"time"
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

func (a *App) syncMembershipRevocations(ctx context.Context, libraryID string) error {
	if a == nil {
		return nil
	}
	locator := a.peerLocator(a.cfg.RegistryURL)
	if locator == nil {
		return nil
	}
	var library Library
	if err := a.storage.WithContext(ctx).Where("library_id = ?", strings.TrimSpace(libraryID)).Take(&library).Error; err != nil {
		return err
	}
	if strings.TrimSpace(library.RootPrivateKey) == "" {
		return nil
	}
	var rows []MembershipCertRevocation
	if err := a.storage.WithContext(ctx).Where("library_id = ?", strings.TrimSpace(libraryID)).Find(&rows).Error; err != nil {
		return err
	}
	revocations := make([]registryauth.MembershipRevocation, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.DeviceID) == "" || row.Serial <= 0 {
			continue
		}
		revocations = append(revocations, registryauth.MembershipRevocation{
			DeviceID:  row.DeviceID,
			MaxSerial: row.Serial,
		})
	}
	if len(revocations) == 0 {
		return nil
	}
	req, err := registryauth.SignRevocationSync(registryauth.RevocationSyncRequest{
		LibraryID:             library.LibraryID,
		RootPublicKey:         library.RootPublicKey,
		Revision:              time.Now().UTC().UnixNano(),
		MembershipRevocations: revocations,
	}, library.RootPrivateKey)
	if err != nil {
		return fmt.Errorf("sign membership revocation sync: %w", err)
	}
	return locator.SyncRevocations(ctx, req)
}
