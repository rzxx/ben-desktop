package desktopcore

import (
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
)

const EventCatalogChanged = "catalog:changed"

type CatalogEventsService struct {
	mu             sync.RWMutex
	subscribers    map[uint64]func(apitypes.CatalogChangeEvent)
	nextSubscriber uint64
}

func NewCatalogEventsService() *CatalogEventsService {
	return &CatalogEventsService{
		subscribers: make(map[uint64]func(apitypes.CatalogChangeEvent)),
	}
}

func (s *CatalogEventsService) Emit(event apitypes.CatalogChangeEvent) {
	if s == nil {
		return
	}
	event.EntityID = strings.TrimSpace(event.EntityID)
	event.QueryKey = strings.TrimSpace(event.QueryKey)
	event.RecordingIDs = compactNonEmptyStrings(event.RecordingIDs)
	event.AlbumIDs = compactNonEmptyStrings(event.AlbumIDs)
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}

	s.mu.RLock()
	subscribers := make([]func(apitypes.CatalogChangeEvent), 0, len(s.subscribers))
	for _, subscriber := range s.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.RUnlock()

	for _, subscriber := range subscribers {
		subscriber(event)
	}
}

func (s *CatalogEventsService) Subscribe(listener func(apitypes.CatalogChangeEvent)) func() {
	if s == nil || listener == nil {
		return func() {}
	}

	s.mu.Lock()
	id := s.nextSubscriber
	s.nextSubscriber++
	s.subscribers[id] = listener
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}
