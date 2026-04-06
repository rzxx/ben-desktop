package desktopcore

import (
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
)

const EventPinChanged = "pin:changed"

type PinEventsService struct {
	mu             sync.RWMutex
	subscribers    map[uint64]func(apitypes.PinChangeEvent)
	nextSubscriber uint64
}

func NewPinEventsService() *PinEventsService {
	return &PinEventsService{
		subscribers: make(map[uint64]func(apitypes.PinChangeEvent)),
	}
}

func (s *PinEventsService) Emit(event apitypes.PinChangeEvent) {
	if s == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if len(event.Subjects) > 0 {
		subjects := make([]apitypes.PinSubjectRef, 0, len(event.Subjects))
		seen := make(map[string]struct{}, len(event.Subjects))
		for _, subject := range event.Subjects {
			subject.Kind = apitypes.PinSubjectKind(strings.TrimSpace(string(subject.Kind)))
			subject.ID = strings.TrimSpace(subject.ID)
			if subject.Kind == "" || subject.ID == "" {
				continue
			}
			key := string(subject.Kind) + "|" + subject.ID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			subjects = append(subjects, subject)
		}
		event.Subjects = subjects
	}

	s.mu.RLock()
	subscribers := make([]func(apitypes.PinChangeEvent), 0, len(s.subscribers))
	for _, subscriber := range s.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.RUnlock()

	for _, subscriber := range subscribers {
		subscriber(event)
	}
}

func (s *PinEventsService) Subscribe(listener func(apitypes.PinChangeEvent)) func() {
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
