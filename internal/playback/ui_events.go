package playback

import apitypes "ben/desktop/api/types"

const (
	EventTransportChanged = "playback:transport"
	EventQueueChanged     = "playback:queue"
)

type TransportEventSnapshot struct {
	CurrentEntry         *QueueEntryEventSnapshot `json:"currentEntry"`
	LoadingEntry         *QueueEntryEventSnapshot `json:"loadingEntry"`
	LoadingPreparation   *EntryPreparation        `json:"loadingPreparation"`
	RepeatMode           RepeatMode               `json:"repeatMode"`
	Shuffle              bool                     `json:"shuffle"`
	Volume               int                      `json:"volume"`
	Status               Status                   `json:"status"`
	PositionMS           int64                    `json:"positionMs"`
	PositionCapturedAtMS int64                    `json:"positionCapturedAtMs,omitempty"`
	DurationMS           *int64                   `json:"durationMs"`
	LastError            string                   `json:"lastError"`
}

type SessionItemEvent struct {
	LibraryRecordingID string `json:"libraryRecordingId,omitempty"`
	VariantRecordingID string `json:"variantRecordingId,omitempty"`
	RecordingID        string `json:"recordingId"`
	Title              string `json:"title"`
	Subtitle           string `json:"subtitle"`
	DurationMS         int64  `json:"durationMs"`
	ArtworkRef         string `json:"artworkRef"`
	SourceKind         string `json:"sourceKind,omitempty"`
	SourceID           string `json:"sourceId,omitempty"`
	SourceItemID       string `json:"sourceItemId,omitempty"`
	AlbumID            string `json:"albumId,omitempty"`
	VariantAlbumID     string `json:"variantAlbumId,omitempty"`
}

type QueueEntryEventSnapshot struct {
	EntryID      string           `json:"entryId"`
	Origin       EntryOrigin      `json:"origin,omitempty"`
	ContextIndex int              `json:"contextIndex,omitempty"`
	Item         SessionItemEvent `json:"item"`
}

type ContextQueueEventSnapshot struct {
	Title         string                    `json:"title,omitempty"`
	Entries       []QueueEntryEventSnapshot `json:"entries"`
	HasBefore     bool                      `json:"hasBefore,omitempty"`
	HasAfter      bool                      `json:"hasAfter,omitempty"`
	TotalCount    int                       `json:"totalCount,omitempty"`
	CurrentIndex  int                       `json:"currentIndex,omitempty"`
	ResumeIndex   int                       `json:"resumeIndex,omitempty"`
	WindowStart   int                       `json:"windowStart,omitempty"`
	WindowCount   int                       `json:"windowCount,omitempty"`
	Loading       bool                      `json:"loading,omitempty"`
	SourceVersion int64                     `json:"sourceVersion,omitempty"`
	Source        *PlaybackSourceDescriptor `json:"source,omitempty"`
	Anchor        *PlaybackSourceAnchor     `json:"anchor,omitempty"`
	ShuffleBag    []int                     `json:"shuffleBag,omitempty"`
}

type QueueEventSnapshot struct {
	ContextQueue      *ContextQueueEventSnapshot                        `json:"contextQueue"`
	UserQueue         []QueueEntryEventSnapshot                         `json:"userQueue"`
	EntryAvailability map[string]apitypes.RecordingPlaybackAvailability `json:"entryAvailability"`
	QueueLength       int                                               `json:"queueLength"`
	QueueVersion      int64                                             `json:"queueVersion,omitempty"`
}

func BuildTransportEventSnapshot(snapshot SessionSnapshot) TransportEventSnapshot {
	return TransportEventSnapshot{
		CurrentEntry:         buildQueueEntryEventSnapshot(snapshot.CurrentEntry, true),
		LoadingEntry:         buildQueueEntryEventSnapshot(snapshot.LoadingEntry, true),
		LoadingPreparation:   snapshot.LoadingPreparation,
		RepeatMode:           snapshot.RepeatMode,
		Shuffle:              snapshot.Shuffle,
		Volume:               snapshot.Volume,
		Status:               snapshot.Status,
		PositionMS:           snapshot.PositionMS,
		PositionCapturedAtMS: snapshot.PositionCapturedAtMS,
		DurationMS:           snapshot.DurationMS,
		LastError:            snapshot.LastError,
	}
}

func BuildQueueEventSnapshot(snapshot SessionSnapshot) QueueEventSnapshot {
	var contextQueue *ContextQueueEventSnapshot
	if snapshot.ContextQueue != nil {
		contextQueue = &ContextQueueEventSnapshot{
			Title:         snapshot.ContextQueue.Title,
			Entries:       buildQueueEntryEventSnapshots(snapshot.ContextQueue.Entries, false),
			HasBefore:     snapshot.ContextQueue.HasBefore,
			HasAfter:      snapshot.ContextQueue.HasAfter,
			TotalCount:    snapshot.ContextQueue.TotalCount,
			CurrentIndex:  snapshot.ContextQueue.CurrentIndex,
			ResumeIndex:   snapshot.ContextQueue.ResumeIndex,
			WindowStart:   snapshot.ContextQueue.WindowStart,
			WindowCount:   snapshot.ContextQueue.WindowCount,
			Loading:       snapshot.ContextQueue.Loading,
			SourceVersion: snapshot.ContextQueue.SourceVersion,
			Source:        clonePlaybackSourceDescriptor(snapshot.ContextQueue.Source),
			Anchor:        clonePlaybackSourceAnchor(snapshot.ContextQueue.Anchor),
			ShuffleBag:    append([]int(nil), snapshot.ContextQueue.ShuffleBag...),
		}
	}

	return QueueEventSnapshot{
		ContextQueue:      contextQueue,
		UserQueue:         buildQueueEntryEventSnapshots(snapshot.UserQueue, true),
		EntryAvailability: snapshot.EntryAvailability,
		QueueLength:       snapshot.QueueLength,
		QueueVersion:      snapshot.QueueVersion,
	}
}

func clonePlaybackSourceDescriptor(value *PlaybackSourceDescriptor) *PlaybackSourceDescriptor {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func clonePlaybackSourceAnchor(value *PlaybackSourceAnchor) *PlaybackSourceAnchor {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func buildQueueEntryEventSnapshot(entry *SessionEntry, includeOrigin bool) *QueueEntryEventSnapshot {
	if entry == nil {
		return nil
	}
	item := buildSessionItemEventSnapshot(&entry.Item)
	if item == nil {
		return nil
	}
	eventEntry := &QueueEntryEventSnapshot{
		EntryID:      entry.EntryID,
		ContextIndex: entry.ContextIndex,
		Item:         *item,
	}
	if includeOrigin {
		eventEntry.Origin = entry.Origin
	}
	return eventEntry
}

func buildSessionItemEventSnapshot(item *SessionItem) *SessionItemEvent {
	if item == nil {
		return nil
	}
	return &SessionItemEvent{
		LibraryRecordingID: item.LibraryRecordingID,
		VariantRecordingID: item.VariantRecordingID,
		RecordingID:        item.RecordingID,
		Title:              item.Title,
		Subtitle:           item.Subtitle,
		DurationMS:         item.DurationMS,
		ArtworkRef:         item.ArtworkRef,
		SourceKind:         item.SourceKind,
		SourceID:           item.SourceID,
		SourceItemID:       item.SourceItemID,
		AlbumID:            item.AlbumID,
		VariantAlbumID:     item.VariantAlbumID,
	}
}

func buildQueueEntryEventSnapshots(entries []SessionEntry, includeOrigin bool) []QueueEntryEventSnapshot {
	if len(entries) == 0 {
		return nil
	}
	out := make([]QueueEntryEventSnapshot, 0, len(entries))
	for _, entry := range entries {
		eventEntry := buildQueueEntryEventSnapshot(&entry, includeOrigin)
		if eventEntry == nil {
			continue
		}
		out = append(out, *eventEntry)
	}
	return out
}
