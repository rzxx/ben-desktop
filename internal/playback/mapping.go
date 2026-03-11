package playback

import (
	"strings"

	apitypes "ben/core/api/types"
)

const (
	SourceKindAlbum     = "album"
	SourceKindPlaylist  = "playlist"
	SourceKindLiked     = "liked"
	SourceKindRecording = "recording"
)

func ItemFromRecording(recording apitypes.RecordingListItem) SessionItem {
	return SessionItem{
		RecordingID: recording.RecordingID,
		Title:       recording.Title,
		Subtitle:    joinArtists(recording.Artists),
		DurationMS:  recording.DurationMS,
		ArtworkRef:  recording.RecordingID,
		SourceKind:  SourceKindRecording,
		SourceID:    recording.RecordingID,
	}
}

func ItemsFromAlbumTracks(albumID string, tracks []apitypes.AlbumTrackItem) []SessionItem {
	items := make([]SessionItem, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, SessionItem{
			RecordingID: track.RecordingID,
			Title:       track.Title,
			Subtitle:    joinArtists(track.Artists),
			DurationMS:  track.DurationMS,
			ArtworkRef:  track.RecordingID,
			SourceKind:  SourceKindAlbum,
			SourceID:    strings.TrimSpace(albumID),
			AlbumID:     strings.TrimSpace(albumID),
		})
	}
	return items
}

func ItemsFromPlaylistTracks(playlistID string, tracks []apitypes.PlaylistTrackItem) []SessionItem {
	items := make([]SessionItem, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, SessionItem{
			RecordingID:  track.RecordingID,
			Title:        track.Title,
			Subtitle:     joinArtists(track.Artists),
			DurationMS:   track.DurationMS,
			ArtworkRef:   track.RecordingID,
			SourceKind:   SourceKindPlaylist,
			SourceID:     strings.TrimSpace(playlistID),
			SourceItemID: strings.TrimSpace(track.ItemID),
		})
	}
	return items
}

func ItemsFromLikedRecordings(recordings []apitypes.LikedRecordingItem) []SessionItem {
	items := make([]SessionItem, 0, len(recordings))
	for _, recording := range recordings {
		items = append(items, SessionItem{
			RecordingID: recording.RecordingID,
			Title:       recording.Title,
			Subtitle:    joinArtists(recording.Artists),
			DurationMS:  recording.DurationMS,
			ArtworkRef:  recording.RecordingID,
			SourceKind:  SourceKindLiked,
		})
	}
	return items
}

func NormalizeItems(items []SessionItem) []SessionItem {
	out := make([]SessionItem, 0, len(items))
	for _, item := range items {
		item.RecordingID = strings.TrimSpace(item.RecordingID)
		item.Title = strings.TrimSpace(item.Title)
		item.Subtitle = strings.TrimSpace(item.Subtitle)
		item.ArtworkRef = strings.TrimSpace(item.ArtworkRef)
		item.SourceKind = strings.TrimSpace(item.SourceKind)
		item.SourceID = strings.TrimSpace(item.SourceID)
		item.SourceItemID = strings.TrimSpace(item.SourceItemID)
		item.AlbumID = strings.TrimSpace(item.AlbumID)
		if item.RecordingID == "" {
			continue
		}
		if item.Title == "" {
			item.Title = item.RecordingID
		}
		out = append(out, item)
	}
	return out
}

func joinArtists(values []string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.Join(filtered, ", ")
}
