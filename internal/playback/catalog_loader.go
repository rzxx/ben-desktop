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

func (l *CatalogLoader) LoadAlbumContext(ctx context.Context, albumID string) (PlaybackContextInput, error) {
	if l == nil || l.core == nil {
		return PlaybackContextInput{}, fmt.Errorf("playback core is not available")
	}

	albumID = strings.TrimSpace(albumID)
	var tracks []apitypes.AlbumTrackItem
	offset := 0
	for {
		page, err := l.core.ListAlbumTracks(ctx, apitypes.AlbumTrackListRequest{
			AlbumID: albumID,
			PageRequest: apitypes.PageRequest{
				Limit:  200,
				Offset: offset,
			},
		})
		if err != nil {
			return PlaybackContextInput{}, err
		}
		tracks = append(tracks, page.Items...)
		if !page.Page.HasMore {
			break
		}
		offset = page.Page.NextOffset
	}

	items := ItemsFromAlbumTracks(albumID, tracks)
	if len(items) == 0 {
		return PlaybackContextInput{}, fmt.Errorf("album %s has no playable tracks", albumID)
	}
	return PlaybackContextInput{Kind: ContextKindAlbum, ID: albumID, Items: items}, nil
}

func (l *CatalogLoader) LoadAlbumTrackContext(ctx context.Context, albumID, recordingID string) (PlaybackContextInput, error) {
	contextInput, err := l.LoadAlbumContext(ctx, albumID)
	if err != nil {
		return PlaybackContextInput{}, err
	}
	startIndex, err := findContextStartIndex(contextInput.Items, func(item SessionItem) bool {
		return item.RecordingID == strings.TrimSpace(recordingID)
	})
	if err != nil {
		return PlaybackContextInput{}, err
	}
	contextInput.StartIndex = startIndex
	return contextInput, nil
}

func (l *CatalogLoader) LoadPlaylistContext(ctx context.Context, playlistID string) (PlaybackContextInput, error) {
	if l == nil || l.core == nil {
		return PlaybackContextInput{}, fmt.Errorf("playback core is not available")
	}

	playlistID = strings.TrimSpace(playlistID)
	var tracks []apitypes.PlaylistTrackItem
	offset := 0
	for {
		page, err := l.core.ListPlaylistTracks(ctx, apitypes.PlaylistTrackListRequest{
			PlaylistID: playlistID,
			PageRequest: apitypes.PageRequest{
				Limit:  200,
				Offset: offset,
			},
		})
		if err != nil {
			return PlaybackContextInput{}, err
		}
		tracks = append(tracks, page.Items...)
		if !page.Page.HasMore {
			break
		}
		offset = page.Page.NextOffset
	}

	items := ItemsFromPlaylistTracks(playlistID, tracks)
	if len(items) == 0 {
		return PlaybackContextInput{}, fmt.Errorf("playlist %s has no playable tracks", playlistID)
	}
	return PlaybackContextInput{Kind: ContextKindPlaylist, ID: playlistID, Items: items}, nil
}

func (l *CatalogLoader) LoadPlaylistTrackContext(ctx context.Context, playlistID, itemID string) (PlaybackContextInput, error) {
	contextInput, err := l.LoadPlaylistContext(ctx, playlistID)
	if err != nil {
		return PlaybackContextInput{}, err
	}
	startIndex, err := findContextStartIndex(contextInput.Items, func(item SessionItem) bool {
		return item.SourceItemID == strings.TrimSpace(itemID)
	})
	if err != nil {
		return PlaybackContextInput{}, err
	}
	contextInput.StartIndex = startIndex
	return contextInput, nil
}

func (l *CatalogLoader) LoadLikedContext(ctx context.Context) (PlaybackContextInput, error) {
	if l == nil || l.core == nil {
		return PlaybackContextInput{}, fmt.Errorf("playback core is not available")
	}

	var liked []apitypes.LikedRecordingItem
	offset := 0
	for {
		page, err := l.core.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{
			PageRequest: apitypes.PageRequest{
				Limit:  200,
				Offset: offset,
			},
		})
		if err != nil {
			return PlaybackContextInput{}, err
		}
		liked = append(liked, page.Items...)
		if !page.Page.HasMore {
			break
		}
		offset = page.Page.NextOffset
	}

	items := ItemsFromLikedRecordings(liked)
	if len(items) == 0 {
		return PlaybackContextInput{}, fmt.Errorf("liked recordings are empty")
	}
	return PlaybackContextInput{Kind: ContextKindLiked, ID: "liked", Items: items}, nil
}

func (l *CatalogLoader) LoadLikedTrackContext(ctx context.Context, recordingID string) (PlaybackContextInput, error) {
	contextInput, err := l.LoadLikedContext(ctx)
	if err != nil {
		return PlaybackContextInput{}, err
	}
	startIndex, err := findContextStartIndex(contextInput.Items, func(item SessionItem) bool {
		return item.RecordingID == strings.TrimSpace(recordingID)
	})
	if err != nil {
		return PlaybackContextInput{}, err
	}
	contextInput.StartIndex = startIndex
	return contextInput, nil
}

func (l *CatalogLoader) LoadRecordingContext(ctx context.Context, recordingID string) (PlaybackContextInput, error) {
	if l == nil || l.core == nil {
		return PlaybackContextInput{}, fmt.Errorf("playback core is not available")
	}

	recordingID = strings.TrimSpace(recordingID)
	recording, err := l.core.GetRecording(ctx, recordingID)
	if err != nil {
		return PlaybackContextInput{}, err
	}

	item := ItemFromRecording(recording)
	return PlaybackContextInput{
		Kind:  ContextKindRecording,
		ID:    recordingID,
		Items: []SessionItem{item},
	}, nil
}

func findContextStartIndex(items []SessionItem, match func(SessionItem) bool) (int, error) {
	for index, item := range items {
		if match(item) {
			return index, nil
		}
	}
	return 0, fmt.Errorf("selected item is not present in playback context")
}
