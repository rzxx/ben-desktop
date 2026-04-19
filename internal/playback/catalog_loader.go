package playback

import (
	"context"
	"fmt"
	"strings"

	apitypes "ben/desktop/api/types"
)

type CatalogLoader struct {
	core PlaybackCore
}

func NewCatalogLoader(core PlaybackCore) *CatalogLoader {
	return &CatalogLoader{core: core}
}

const playbackSourcePageLimit = 500

func (l *CatalogLoader) BuildAlbumSource(albumID string) PlaybackSourceRequest {
	albumID = strings.TrimSpace(albumID)
	return PlaybackSourceRequest{
		Descriptor: PlaybackSourceDescriptor{
			Kind:         ContextKindAlbum,
			ID:           albumID,
			RebasePolicy: ContextRebaseFrozen,
		},
	}
}

func (l *CatalogLoader) BuildAlbumTrackSource(albumID, recordingID string) PlaybackSourceRequest {
	req := l.BuildAlbumSource(albumID)
	req.Anchor = PlaybackSourceAnchor{RecordingID: strings.TrimSpace(recordingID)}
	return req
}

func (l *CatalogLoader) BuildPlaylistSource(playlistID string) PlaybackSourceRequest {
	playlistID = strings.TrimSpace(playlistID)
	return PlaybackSourceRequest{
		Descriptor: PlaybackSourceDescriptor{
			Kind:         ContextKindPlaylist,
			ID:           playlistID,
			RebasePolicy: ContextRebaseLive,
			Live:         true,
		},
	}
}

func (l *CatalogLoader) BuildPlaylistTrackSource(playlistID, itemID string) PlaybackSourceRequest {
	req := l.BuildPlaylistSource(playlistID)
	req.Anchor = PlaybackSourceAnchor{SourceItemID: strings.TrimSpace(itemID)}
	return req
}

func (l *CatalogLoader) BuildLikedSource() PlaybackSourceRequest {
	return PlaybackSourceRequest{
		Descriptor: PlaybackSourceDescriptor{
			Kind:         ContextKindLiked,
			ID:           "liked",
			Title:        "Liked songs",
			RebasePolicy: ContextRebaseLive,
			Live:         true,
		},
	}
}

func (l *CatalogLoader) BuildLikedTrackSource(recordingID string) PlaybackSourceRequest {
	req := l.BuildLikedSource()
	req.Anchor = PlaybackSourceAnchor{RecordingID: strings.TrimSpace(recordingID)}
	return req
}

func (l *CatalogLoader) BuildTracksSource() PlaybackSourceRequest {
	return PlaybackSourceRequest{
		Descriptor: PlaybackSourceDescriptor{
			Kind:         ContextKindTracks,
			ID:           "tracks",
			Title:        "Tracks",
			RebasePolicy: ContextRebaseLive,
			Live:         true,
		},
	}
}

func (l *CatalogLoader) BuildTracksTrackSource(recordingID string) PlaybackSourceRequest {
	req := l.BuildTracksSource()
	req.Anchor = PlaybackSourceAnchor{RecordingID: strings.TrimSpace(recordingID)}
	return req
}

func (l *CatalogLoader) BuildRecordingSource(recordingID string) PlaybackSourceRequest {
	recordingID = strings.TrimSpace(recordingID)
	return PlaybackSourceRequest{
		Descriptor: PlaybackSourceDescriptor{
			Kind:         ContextKindRecording,
			ID:           recordingID,
			Title:        recordingID,
			RebasePolicy: ContextRebaseFrozen,
		},
		Anchor: PlaybackSourceAnchor{RecordingID: recordingID},
	}
}

func (l *CatalogLoader) EnumerateSource(ctx context.Context, req PlaybackSourceRequest) (PlaybackSourceRequest, []PlaybackSourceCandidate, int, error) {
	if l == nil || l.core == nil {
		return PlaybackSourceRequest{}, nil, 0, fmt.Errorf("playback core is not available")
	}

	descriptor := req.Descriptor
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Title = strings.TrimSpace(descriptor.Title)
	if descriptor.Kind == ContextKindAlbum &&
		(descriptor.Title == "" || descriptor.Title == descriptor.ID) {
		album, err := l.core.GetAlbum(ctx, descriptor.ID)
		if err == nil && strings.TrimSpace(album.Title) != "" {
			descriptor.Title = strings.TrimSpace(album.Title)
		}
	}
	if descriptor.Kind == ContextKindPlaylist &&
		(descriptor.Title == "" || descriptor.Title == descriptor.ID) {
		playlist, err := l.core.GetPlaylistSummary(ctx, descriptor.ID)
		if err == nil && strings.TrimSpace(playlist.Name) != "" {
			descriptor.Title = strings.TrimSpace(playlist.Name)
		}
	}
	anchor := req.Anchor

	var candidates []PlaybackSourceCandidate
	switch descriptor.Kind {
	case ContextKindAlbum:
		items, err := l.enumerateAlbumSource(ctx, descriptor.ID)
		if err != nil {
			return PlaybackSourceRequest{}, nil, 0, err
		}
		candidates = items
	case ContextKindPlaylist:
		items, err := l.enumeratePlaylistSource(ctx, descriptor.ID)
		if err != nil {
			return PlaybackSourceRequest{}, nil, 0, err
		}
		candidates = items
	case ContextKindLiked:
		items, err := l.enumerateLikedSource(ctx)
		if err != nil {
			return PlaybackSourceRequest{}, nil, 0, err
		}
		candidates = items
	case ContextKindTracks:
		items, err := l.enumerateTracksSource(ctx)
		if err != nil {
			return PlaybackSourceRequest{}, nil, 0, err
		}
		candidates = items
	case ContextKindRecording:
		items, err := l.enumerateRecordingSource(ctx, descriptor.ID)
		if err != nil {
			return PlaybackSourceRequest{}, nil, 0, err
		}
		candidates = items
	default:
		return PlaybackSourceRequest{}, nil, 0, fmt.Errorf("unsupported playback source %q", descriptor.Kind)
	}

	if len(candidates) == 0 {
		return PlaybackSourceRequest{}, nil, 0, fmt.Errorf("playback source %s has no playable tracks", descriptor.ID)
	}

	startIndex, err := findSourceAnchorIndex(candidates, anchor)
	if err != nil {
		return PlaybackSourceRequest{}, nil, 0, err
	}
	if anchor.EntryKey == "" {
		anchor.EntryKey = candidates[startIndex].Key
	}
	req.Descriptor = descriptor
	req.Anchor = anchor
	return req, candidates, startIndex, nil
}

func (l *CatalogLoader) enumerateAlbumSource(ctx context.Context, albumID string) ([]PlaybackSourceCandidate, error) {
	var tracks []apitypes.AlbumTrackItem
	offset := 0
	for {
		page, err := l.core.ListAlbumTracks(ctx, apitypes.AlbumTrackListRequest{
			AlbumID: albumID,
			PageRequest: apitypes.PageRequest{
				Limit:  playbackSourcePageLimit,
				Offset: offset,
			},
		})
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, page.Items...)
		if !page.Page.HasMore {
			break
		}
		offset = page.Page.NextOffset
	}
	items := ItemsFromAlbumTracks(albumID, tracks)
	out := make([]PlaybackSourceCandidate, 0, len(items))
	for index, item := range items {
		out = append(out, PlaybackSourceCandidate{
			Key:  fmt.Sprintf("album:%s:%06d:%s", albumID, index, item.Target.ExactVariantRecordingID),
			Item: item,
		})
	}
	return out, nil
}

func (l *CatalogLoader) enumeratePlaylistSource(ctx context.Context, playlistID string) ([]PlaybackSourceCandidate, error) {
	var tracks []apitypes.PlaylistTrackItem
	cursor := ""
	for {
		page, err := l.core.ListPlaylistTracksCursor(ctx, apitypes.PlaylistTrackCursorRequest{
			PlaylistID: playlistID,
			CursorPageRequest: apitypes.CursorPageRequest{
				Limit:  playbackSourcePageLimit,
				Cursor: cursor,
			},
		})
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, page.Items...)
		if !page.Page.HasMore {
			break
		}
		cursor = page.Page.NextCursor
	}
	items := ItemsFromPlaylistTracks(playlistID, tracks)
	out := make([]PlaybackSourceCandidate, 0, len(items))
	for index, item := range items {
		key := strings.TrimSpace(item.SourceItemID)
		if key == "" {
			key = fmt.Sprintf("%06d:%s", index, firstNonEmpty(item.VariantRecordingID, item.RecordingID))
		}
		out = append(out, PlaybackSourceCandidate{
			Key:  "playlist:" + playlistID + ":" + key,
			Item: item,
		})
	}
	return out, nil
}

func (l *CatalogLoader) enumerateLikedSource(ctx context.Context) ([]PlaybackSourceCandidate, error) {
	var liked []apitypes.LikedRecordingItem
	cursor := ""
	for {
		page, err := l.core.ListLikedRecordingsCursor(ctx, apitypes.LikedRecordingCursorRequest{
			CursorPageRequest: apitypes.CursorPageRequest{
				Limit:  playbackSourcePageLimit,
				Cursor: cursor,
			},
		})
		if err != nil {
			return nil, err
		}
		liked = append(liked, page.Items...)
		if !page.Page.HasMore {
			break
		}
		cursor = page.Page.NextCursor
	}
	items := ItemsFromLikedRecordings(liked)
	out := make([]PlaybackSourceCandidate, 0, len(items))
	for index, item := range items {
		key := firstNonEmpty(item.LibraryRecordingID, item.RecordingID, item.VariantRecordingID)
		if key == "" {
			key = fmt.Sprintf("%06d", index)
		}
		out = append(out, PlaybackSourceCandidate{
			Key:  "liked:" + key,
			Item: item,
		})
	}
	return out, nil
}

func (l *CatalogLoader) enumerateTracksSource(ctx context.Context) ([]PlaybackSourceCandidate, error) {
	var recordings []apitypes.RecordingListItem
	cursor := ""
	for {
		page, err := l.core.ListRecordingsCursor(ctx, apitypes.RecordingCursorRequest{
			CursorPageRequest: apitypes.CursorPageRequest{
				Limit:  playbackSourcePageLimit,
				Cursor: cursor,
			},
		})
		if err != nil {
			return nil, err
		}
		recordings = append(recordings, page.Items...)
		if !page.Page.HasMore {
			break
		}
		cursor = page.Page.NextCursor
	}
	out := make([]PlaybackSourceCandidate, 0, len(recordings))
	for _, recording := range recordings {
		item := ItemFromRecording(recording)
		item.SourceKind = SourceKindTracks
		item.SourceID = "tracks"
		out = append(out, PlaybackSourceCandidate{
			Key:  "tracks:" + firstNonEmpty(item.LibraryRecordingID, item.RecordingID),
			Item: item,
		})
	}
	return out, nil
}

func (l *CatalogLoader) enumerateRecordingSource(ctx context.Context, recordingID string) ([]PlaybackSourceCandidate, error) {
	recording, err := l.core.GetRecording(ctx, recordingID)
	if err != nil {
		return nil, err
	}
	item := ItemFromRecordingRequest(recording, recordingID)
	return []PlaybackSourceCandidate{{
		Key:  "recording:" + strings.TrimSpace(recordingID),
		Item: item,
	}}, nil
}

func (l *CatalogLoader) MaterializeSource(ctx context.Context, req PlaybackSourceRequest) (PlaybackContextInput, error) {
	if l == nil || l.core == nil {
		return PlaybackContextInput{}, fmt.Errorf("playback core is not available")
	}
	resolved, candidates, startIndex, err := l.EnumerateSource(ctx, req)
	if err != nil {
		return PlaybackContextInput{}, err
	}
	return playbackContextInputFromSource(resolved, candidates, startIndex), nil
}

func (l *CatalogLoader) ResolveSourceItem(ctx context.Context, req PlaybackSourceRequest) (SessionItem, error) {
	if l == nil || l.core == nil {
		return SessionItem{}, fmt.Errorf("playback core is not available")
	}
	_, candidates, startIndex, err := l.EnumerateSource(ctx, req)
	if err != nil {
		return SessionItem{}, err
	}
	if startIndex < 0 || startIndex >= len(candidates) {
		return SessionItem{}, fmt.Errorf("selected item is not present in playback source")
	}
	return candidates[startIndex].Item, nil
}

func playbackContextInputFromSource(req PlaybackSourceRequest, candidates []PlaybackSourceCandidate, startIndex int) PlaybackContextInput {
	items := make([]SessionItem, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate.Item)
	}
	return PlaybackContextInput{
		Kind:       req.Descriptor.Kind,
		ID:         req.Descriptor.ID,
		Title:      req.Descriptor.Title,
		Items:      items,
		StartIndex: startIndex,
	}
}

func findSourceAnchorIndex(candidates []PlaybackSourceCandidate, anchor PlaybackSourceAnchor) (int, error) {
	for index, candidate := range candidates {
		if strings.TrimSpace(anchor.EntryKey) != "" && candidate.Key == strings.TrimSpace(anchor.EntryKey) {
			return index, nil
		}
	}
	for index, candidate := range candidates {
		if strings.TrimSpace(anchor.SourceItemID) != "" && candidate.Item.SourceItemID == strings.TrimSpace(anchor.SourceItemID) {
			return index, nil
		}
	}
	for index, candidate := range candidates {
		if strings.TrimSpace(anchor.RecordingID) == "" {
			break
		}
		if candidate.Item.RecordingID == strings.TrimSpace(anchor.RecordingID) ||
			candidate.Item.LibraryRecordingID == strings.TrimSpace(anchor.RecordingID) ||
			candidate.Item.VariantRecordingID == strings.TrimSpace(anchor.RecordingID) {
			return index, nil
		}
	}
	if strings.TrimSpace(anchor.EntryKey) != "" || strings.TrimSpace(anchor.SourceItemID) != "" || strings.TrimSpace(anchor.RecordingID) != "" {
		return 0, fmt.Errorf("selected item is not present in playback source")
	}
	return 0, nil
}
