package playback

import (
	"context"
	"fmt"
	"strings"

	apitypes "ben/core/api/types"
)

type CatalogLoader struct {
	bridge CorePlaybackBridge
}

func NewCatalogLoader(bridge CorePlaybackBridge) *CatalogLoader {
	return &CatalogLoader{bridge: bridge}
}

func (l *CatalogLoader) LoadAlbumContext(ctx context.Context, albumID string) (PlaybackContextInput, error) {
	if l == nil || l.bridge == nil {
		return PlaybackContextInput{}, fmt.Errorf("core playback bridge is not available")
	}

	albumID = strings.TrimSpace(albumID)
	var tracks []apitypes.AlbumTrackItem
	offset := 0
	for {
		page, err := l.bridge.ListAlbumTracks(ctx, apitypes.AlbumTrackListRequest{
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

func (l *CatalogLoader) LoadPlaylistContext(ctx context.Context, playlistID string) (PlaybackContextInput, error) {
	if l == nil || l.bridge == nil {
		return PlaybackContextInput{}, fmt.Errorf("core playback bridge is not available")
	}

	playlistID = strings.TrimSpace(playlistID)
	var tracks []apitypes.PlaylistTrackItem
	offset := 0
	for {
		page, err := l.bridge.ListPlaylistTracks(ctx, apitypes.PlaylistTrackListRequest{
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

func (l *CatalogLoader) LoadLikedContext(ctx context.Context) (PlaybackContextInput, error) {
	if l == nil || l.bridge == nil {
		return PlaybackContextInput{}, fmt.Errorf("core playback bridge is not available")
	}

	var liked []apitypes.LikedRecordingItem
	offset := 0
	for {
		page, err := l.bridge.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{
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

func (l *CatalogLoader) LoadRecordingContext(ctx context.Context, recordingID string) (PlaybackContextInput, error) {
	if l == nil || l.bridge == nil {
		return PlaybackContextInput{}, fmt.Errorf("core playback bridge is not available")
	}

	recordingID = strings.TrimSpace(recordingID)
	recording, err := l.bridge.GetRecording(ctx, recordingID)
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
