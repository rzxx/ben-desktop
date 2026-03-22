package playback

import (
	"strings"

	apitypes "ben/desktop/api/types"
)

const (
	SourceKindAlbum     = "album"
	SourceKindPlaylist  = "playlist"
	SourceKindLiked     = "liked"
	SourceKindRecording = "recording"
)

func ItemFromRecording(recording apitypes.RecordingListItem) SessionItem {
	return SessionItem{
		LibraryRecordingID: recording.LibraryRecordingID,
		VariantRecordingID: recording.PreferredVariantRecordingID,
		RecordingID:        firstNonEmpty(recording.LibraryRecordingID, recording.RecordingID),
		Title:              recording.Title,
		Subtitle:           joinArtists(recording.Artists),
		DurationMS:         recording.DurationMS,
		ArtworkRef:         firstNonEmpty(recording.LibraryRecordingID, recording.RecordingID),
		SourceKind:         SourceKindRecording,
		SourceID:           firstNonEmpty(recording.LibraryRecordingID, recording.RecordingID),
		ResolutionMode:     ResolutionModeLibrary,
	}
}

func ItemsFromAlbumTracks(albumID string, tracks []apitypes.AlbumTrackItem) []SessionItem {
	items := make([]SessionItem, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, SessionItem{
			LibraryRecordingID: track.LibraryRecordingID,
			VariantRecordingID: firstNonEmpty(track.VariantRecordingID, track.RecordingID),
			RecordingID:        firstNonEmpty(track.VariantRecordingID, track.RecordingID),
			Title:              track.Title,
			Subtitle:           joinArtists(track.Artists),
			DurationMS:         track.DurationMS,
			ArtworkRef:         firstNonEmpty(track.VariantRecordingID, track.RecordingID),
			SourceKind:         SourceKindAlbum,
			SourceID:           strings.TrimSpace(albumID),
			AlbumID:            strings.TrimSpace(albumID),
			VariantAlbumID:     strings.TrimSpace(albumID),
			ResolutionMode:     ResolutionModeExplicit,
		})
	}
	return items
}

func ItemsFromPlaylistTracks(playlistID string, tracks []apitypes.PlaylistTrackItem) []SessionItem {
	items := make([]SessionItem, 0, len(tracks))
	for _, track := range tracks {
		libraryRecordingID := firstNonEmpty(track.LibraryRecordingID, track.RecordingID)
		variantRecordingID := firstNonEmpty(track.RecordingID, track.LibraryRecordingID)
		items = append(items, SessionItem{
			LibraryRecordingID: libraryRecordingID,
			VariantRecordingID: variantRecordingID,
			RecordingID:        variantRecordingID,
			Title:              track.Title,
			Subtitle:           joinArtists(track.Artists),
			DurationMS:         track.DurationMS,
			ArtworkRef:         variantRecordingID,
			SourceKind:         SourceKindPlaylist,
			SourceID:           strings.TrimSpace(playlistID),
			SourceItemID:       strings.TrimSpace(track.ItemID),
			ResolutionMode:     ResolutionModeExplicit,
		})
	}
	return items
}

func ItemsFromLikedRecordings(recordings []apitypes.LikedRecordingItem) []SessionItem {
	items := make([]SessionItem, 0, len(recordings))
	for _, recording := range recordings {
		libraryRecordingID := firstNonEmpty(recording.LibraryRecordingID, recording.RecordingID)
		variantRecordingID := firstNonEmpty(recording.RecordingID, recording.LibraryRecordingID)
		items = append(items, SessionItem{
			LibraryRecordingID: libraryRecordingID,
			VariantRecordingID: variantRecordingID,
			RecordingID:        variantRecordingID,
			Title:              recording.Title,
			Subtitle:           joinArtists(recording.Artists),
			DurationMS:         recording.DurationMS,
			ArtworkRef:         variantRecordingID,
			SourceKind:         SourceKindLiked,
			ResolutionMode:     ResolutionModeExplicit,
		})
	}
	return items
}

func NormalizeItems(items []SessionItem) []SessionItem {
	out := make([]SessionItem, 0, len(items))
	for _, item := range items {
		item.LibraryRecordingID = strings.TrimSpace(item.LibraryRecordingID)
		item.VariantRecordingID = strings.TrimSpace(item.VariantRecordingID)
		item.RecordingID = strings.TrimSpace(item.RecordingID)
		item.Title = strings.TrimSpace(item.Title)
		item.Subtitle = strings.TrimSpace(item.Subtitle)
		item.ArtworkRef = strings.TrimSpace(item.ArtworkRef)
		item.SourceKind = strings.TrimSpace(item.SourceKind)
		item.SourceID = strings.TrimSpace(item.SourceID)
		item.SourceItemID = strings.TrimSpace(item.SourceItemID)
		item.AlbumID = strings.TrimSpace(item.AlbumID)
		item.VariantAlbumID = strings.TrimSpace(item.VariantAlbumID)
		if item.RecordingID == "" {
			item.RecordingID = firstNonEmpty(item.LibraryRecordingID, item.VariantRecordingID)
		}
		if item.RecordingID == "" {
			continue
		}
		if item.Title == "" {
			item.Title = item.RecordingID
		}
		if item.ArtworkRef == "" {
			item.ArtworkRef = item.RecordingID
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
