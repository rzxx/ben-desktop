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
	logicalRecordingID := firstNonEmpty(recording.LibraryRecordingID, recording.RecordingID)
	exactVariantID := strings.TrimSpace(recording.PreferredVariantRecordingID)
	return SessionItem{
		LibraryRecordingID: recording.LibraryRecordingID,
		VariantRecordingID: exactVariantID,
		RecordingID:        logicalRecordingID,
		Title:              recording.Title,
		Subtitle:           joinArtists(recording.Artists),
		DurationMS:         recording.DurationMS,
		ArtworkRef:         firstNonEmpty(exactVariantID, logicalRecordingID),
		SourceKind:         SourceKindRecording,
		SourceID:           logicalRecordingID,
		ResolutionMode:     ResolutionModeLibrary,
		Target: PlaybackTargetRef{
			LogicalRecordingID:      logicalRecordingID,
			ExactVariantRecordingID: exactVariantID,
			ResolutionPolicy:        PlaybackTargetResolutionPreferred,
		},
	}
}

func ItemFromRecordingRequest(recording apitypes.RecordingListItem, requestedRecordingID string) SessionItem {
	item := ItemFromRecording(recording)
	requestedRecordingID = strings.TrimSpace(requestedRecordingID)
	if requestedRecordingID == "" || requestedRecordingID == item.Target.LogicalRecordingID {
		return item
	}

	// Preserve an explicitly requested variant so queued single-track playback
	// follows the same exact variant the user picked from album details.
	item.VariantRecordingID = requestedRecordingID
	item.ArtworkRef = firstNonEmpty(requestedRecordingID, item.ArtworkRef)
	item.Target.ExactVariantRecordingID = requestedRecordingID
	item.Target.ResolutionPolicy = PlaybackTargetResolutionExact
	return item
}

func ItemsFromAlbumTracks(albumID string, tracks []apitypes.AlbumTrackItem) []SessionItem {
	items := make([]SessionItem, 0, len(tracks))
	for _, track := range tracks {
		logicalRecordingID := firstNonEmpty(track.LibraryRecordingID, track.RecordingID, track.VariantRecordingID)
		exactVariantID := firstNonEmpty(track.VariantRecordingID, track.RecordingID, track.LibraryRecordingID)
		items = append(items, SessionItem{
			LibraryRecordingID: track.LibraryRecordingID,
			VariantRecordingID: exactVariantID,
			RecordingID:        exactVariantID,
			Title:              track.Title,
			Subtitle:           joinArtists(track.Artists),
			DurationMS:         track.DurationMS,
			ArtworkRef:         exactVariantID,
			SourceKind:         SourceKindAlbum,
			SourceID:           strings.TrimSpace(albumID),
			AlbumID:            strings.TrimSpace(albumID),
			VariantAlbumID:     strings.TrimSpace(albumID),
			ResolutionMode:     ResolutionModeExplicit,
			Target: PlaybackTargetRef{
				LogicalRecordingID:      logicalRecordingID,
				ExactVariantRecordingID: exactVariantID,
				ResolutionPolicy:        PlaybackTargetResolutionExact,
			},
		})
	}
	return items
}

func ItemsFromPlaylistTracks(playlistID string, tracks []apitypes.PlaylistTrackItem) []SessionItem {
	items := make([]SessionItem, 0, len(tracks))
	for _, track := range tracks {
		libraryRecordingID := firstNonEmpty(track.LibraryRecordingID, track.RecordingID)
		variantRecordingID := firstNonEmpty(track.RecordingID, track.LibraryRecordingID)
		recordingID := libraryRecordingID
		if recordingID == "" {
			recordingID = variantRecordingID
		}
		items = append(items, SessionItem{
			LibraryRecordingID: libraryRecordingID,
			VariantRecordingID: variantRecordingID,
			RecordingID:        recordingID,
			Title:              track.Title,
			Subtitle:           joinArtists(track.Artists),
			DurationMS:         track.DurationMS,
			ArtworkRef:         firstNonEmpty(variantRecordingID, recordingID),
			SourceKind:         SourceKindPlaylist,
			SourceID:           strings.TrimSpace(playlistID),
			SourceItemID:       strings.TrimSpace(track.ItemID),
			ResolutionMode:     ResolutionModeExplicit,
			Target: PlaybackTargetRef{
				LogicalRecordingID:      recordingID,
				ExactVariantRecordingID: variantRecordingID,
				ResolutionPolicy:        PlaybackTargetResolutionExact,
			},
		})
	}
	return items
}

func ItemsFromLikedRecordings(recordings []apitypes.LikedRecordingItem) []SessionItem {
	items := make([]SessionItem, 0, len(recordings))
	for _, recording := range recordings {
		libraryRecordingID := firstNonEmpty(recording.LibraryRecordingID, recording.RecordingID)
		variantRecordingID := firstNonEmpty(recording.RecordingID, recording.LibraryRecordingID)
		recordingID := libraryRecordingID
		if recordingID == "" {
			recordingID = variantRecordingID
		}
		items = append(items, SessionItem{
			LibraryRecordingID: libraryRecordingID,
			VariantRecordingID: variantRecordingID,
			RecordingID:        recordingID,
			Title:              recording.Title,
			Subtitle:           joinArtists(recording.Artists),
			DurationMS:         recording.DurationMS,
			ArtworkRef:         firstNonEmpty(variantRecordingID, recordingID),
			SourceKind:         SourceKindLiked,
			ResolutionMode:     ResolutionModeExplicit,
			Target: PlaybackTargetRef{
				LogicalRecordingID:      recordingID,
				ExactVariantRecordingID: variantRecordingID,
				ResolutionPolicy:        PlaybackTargetResolutionExact,
			},
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
		item.Target.LogicalRecordingID = strings.TrimSpace(item.Target.LogicalRecordingID)
		item.Target.ExactVariantRecordingID = strings.TrimSpace(item.Target.ExactVariantRecordingID)
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
		if item.Target.ResolutionPolicy == "" {
			switch item.ResolutionMode {
			case ResolutionModeExplicit:
				item.Target.ResolutionPolicy = PlaybackTargetResolutionExact
			default:
				item.Target.ResolutionPolicy = PlaybackTargetResolutionPreferred
			}
		}
		if item.Target.LogicalRecordingID == "" {
			item.Target.LogicalRecordingID = firstNonEmpty(item.LibraryRecordingID, item.RecordingID, item.VariantRecordingID)
		}
		if item.Target.ExactVariantRecordingID == "" {
			item.Target.ExactVariantRecordingID = firstNonEmpty(item.VariantRecordingID, item.RecordingID)
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
