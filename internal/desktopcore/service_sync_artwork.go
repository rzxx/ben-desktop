package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
)

func (a *App) buildArtworkBlobResponse(ctx context.Context, req ArtworkBlobRequest) (ArtworkBlobResponse, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return ArtworkBlobResponse{}, err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return ArtworkBlobResponse{}, err
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return ArtworkBlobResponse{}, fmt.Errorf("remote library mismatch")
	}

	resp := ArtworkBlobResponse{
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    local.PeerID,
		Artwork: ArtworkBlobTransfer{
			ScopeType: strings.TrimSpace(req.ScopeType),
			ScopeID:   strings.TrimSpace(req.ScopeID),
			Variant:   strings.TrimSpace(req.Variant),
			BlobID:    strings.TrimSpace(req.BlobID),
			MIME:      strings.TrimSpace(req.MIME),
			FileExt:   normalizeArtworkFileExt(req.FileExt, req.MIME),
		},
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return ArtworkBlobResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	resp.Auth = auth

	row, found, err := a.lookupArtworkBlobRecord(ctx, local.LibraryID, strings.TrimSpace(req.ScopeType), strings.TrimSpace(req.ScopeID), strings.TrimSpace(req.Variant))
	if err != nil {
		return ArtworkBlobResponse{}, err
	}
	if !found || row.BlobID == "" || row.FileExt == "" {
		return resp, nil
	}
	if want := strings.TrimSpace(req.BlobID); want != "" && want != row.BlobID {
		return resp, nil
	}
	if want := normalizeArtworkFileExt(req.FileExt, req.MIME); want != "" && want != row.FileExt {
		return resp, nil
	}

	path, ok, err := a.blobs.ArtworkFilePath(row.BlobID, row.FileExt)
	if err != nil {
		return ArtworkBlobResponse{}, err
	}
	if !ok {
		return resp, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ArtworkBlobResponse{}, err
	}
	if err := a.blobs.VerifyID(row.BlobID, data); err != nil {
		return ArtworkBlobResponse{}, err
	}

	resp.Available = true
	resp.Artwork = ArtworkBlobTransfer{
		ScopeType: row.ScopeType,
		ScopeID:   row.ScopeID,
		Variant:   row.Variant,
		BlobID:    row.BlobID,
		MIME:      row.MIME,
		FileExt:   row.FileExt,
		Data:      data,
	}
	return resp, nil
}

func (a *SyncService) syncMissingArtworkBlobsFromPeer(ctx context.Context, local apitypes.LocalContext, peer SyncPeer) (int, error) {
	if a == nil || a.App == nil || peer == nil {
		return 0, nil
	}
	local, err := a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return 0, err
	}
	missing, err := a.listMissingArtworkVariants(ctx, local.LibraryID)
	if err != nil || len(missing) == 0 {
		return 0, err
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return 0, fmt.Errorf("build artwork blob auth: %w", err)
	}

	fetched := 0
	for _, row := range missing {
		if err := ctx.Err(); err != nil {
			return fetched, err
		}
		resp, err := peer.FetchArtworkBlob(ctx, ArtworkBlobRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
			PeerID:    local.PeerID,
			Auth:      auth,
			ScopeType: row.ScopeType,
			ScopeID:   row.ScopeID,
			Variant:   row.Variant,
			BlobID:    row.BlobID,
			MIME:      row.MIME,
			FileExt:   row.FileExt,
		})
		if err != nil {
			a.App.logf("desktopcore: fetch artwork blob %s %s/%s from %s failed: %v", row.Variant, row.ScopeType, row.ScopeID, firstNonEmpty(peer.Address(), peer.PeerID(), peer.DeviceID()), err)
			return fetched, nil
		}
		if _, err := a.verifyTransportPeerAuth(ctx, local.LibraryID, resp.DeviceID, resp.PeerID, firstNonEmpty(peer.PeerID(), resp.PeerID), resp.Auth); err != nil {
			a.App.logf("desktopcore: verify artwork blob auth for %s %s/%s failed: %v", row.Variant, row.ScopeType, row.ScopeID, err)
			return fetched, nil
		}
		_ = a.updateDevicePeerID(ctx, local.LibraryID, firstNonEmpty(resp.DeviceID, peer.DeviceID()), firstNonEmpty(resp.PeerID, peer.PeerID()), firstNonEmpty(resp.DeviceID, peer.DeviceID()))
		if !resp.Available {
			continue
		}
		if err := a.storeFetchedArtworkBlob(resp.Artwork); err != nil {
			return fetched, err
		}
		fetched++
	}
	return fetched, nil
}

func (a *SyncService) listMissingArtworkVariants(ctx context.Context, libraryID string) ([]artworkBlobRecord, error) {
	var variantRows []ArtworkVariant
	if err := a.storage.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("updated_at DESC, scope_type ASC, scope_id ASC, variant ASC").
		Find(&variantRows).Error; err != nil {
		return nil, err
	}
	var coverRows []PlaylistCover
	if err := a.storage.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("updated_at DESC, playlist_id ASC").
		Find(&coverRows).Error; err != nil {
		return nil, err
	}

	out := make([]artworkBlobRecord, 0, len(variantRows)+len(coverRows))
	seen := make(map[string]struct{}, len(variantRows)+len(coverRows))
	for _, row := range variantRows {
		record := artworkBlobRecord{
			ScopeType: strings.TrimSpace(row.ScopeType),
			ScopeID:   strings.TrimSpace(row.ScopeID),
			Variant:   strings.TrimSpace(row.Variant),
			BlobID:    strings.TrimSpace(row.BlobID),
			MIME:      strings.TrimSpace(row.MIME),
			FileExt:   normalizeArtworkFileExt(row.FileExt, row.MIME),
			UpdatedAt: row.UpdatedAt,
		}
		if err := appendMissingArtworkBlobRecord(a.App, &out, seen, record); err != nil {
			return nil, err
		}
	}
	for _, row := range coverRows {
		if err := appendMissingArtworkBlobRecord(a.App, &out, seen, playlistCoverBlobRecordFromRow(row)); err != nil {
			return nil, err
		}
	}
	return sortedArtworkBlobRecords(out), nil
}

func appendMissingArtworkBlobRecord(app *App, out *[]artworkBlobRecord, seen map[string]struct{}, row artworkBlobRecord) error {
	row.BlobID = strings.TrimSpace(row.BlobID)
	row.MIME = strings.TrimSpace(row.MIME)
	row.FileExt = normalizeArtworkFileExt(row.FileExt, row.MIME)
	if row.BlobID == "" || row.FileExt == "" {
		return nil
	}
	key := row.BlobID + "|" + row.FileExt
	if _, ok := seen[key]; ok {
		return nil
	}
	path, ok, err := app.blobs.ArtworkFilePath(row.BlobID, row.FileExt)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if strings.TrimSpace(path) == "" {
		return nil
	}
	seen[key] = struct{}{}
	*out = append(*out, row)
	return nil
}

func (a *SyncService) storeFetchedArtworkBlob(transfer ArtworkBlobTransfer) error {
	transfer.BlobID = strings.TrimSpace(transfer.BlobID)
	transfer.MIME = strings.TrimSpace(transfer.MIME)
	transfer.FileExt = normalizeArtworkFileExt(transfer.FileExt, transfer.MIME)
	if transfer.BlobID == "" {
		return fmt.Errorf("remote artwork blob id is required")
	}
	if transfer.FileExt == "" {
		return fmt.Errorf("remote artwork file extension is required")
	}
	if len(transfer.Data) == 0 {
		return fmt.Errorf("remote artwork data is required")
	}
	if err := verifyBlobIDBytes(transfer.BlobID, transfer.Data); err != nil {
		return fmt.Errorf("remote artwork %w", err)
	}
	storedBlobID, err := a.App.blobs.StoreArtworkBytes(transfer.Data, transfer.FileExt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(storedBlobID) != transfer.BlobID {
		return fmt.Errorf("remote artwork blob hash mismatch")
	}
	return nil
}

func (a *App) lookupArtworkBlobRecord(ctx context.Context, libraryID, scopeType, scopeID, variant string) (artworkBlobRecord, bool, error) {
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	variant = strings.TrimSpace(variant)

	if scopeType == "playlist" && (variant == "" || variant == playlistCoverVariantCanonical) {
		if row, ok, err := a.loadPlaylistCoverRow(ctx, libraryID, scopeID); err != nil {
			return artworkBlobRecord{}, false, err
		} else if ok {
			return playlistCoverBlobRecordFromRow(row), true, nil
		}
	}

	var row ArtworkVariant
	err := a.storage.WithContext(ctx).
		Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", libraryID, scopeType, scopeID, variant).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return artworkBlobRecord{}, false, nil
		}
		return artworkBlobRecord{}, false, err
	}
	return artworkBlobRecord{
		ScopeType: strings.TrimSpace(row.ScopeType),
		ScopeID:   strings.TrimSpace(row.ScopeID),
		Variant:   strings.TrimSpace(row.Variant),
		BlobID:    strings.TrimSpace(row.BlobID),
		MIME:      strings.TrimSpace(row.MIME),
		FileExt:   normalizeArtworkFileExt(row.FileExt, row.MIME),
		UpdatedAt: row.UpdatedAt,
	}, true, nil
}
