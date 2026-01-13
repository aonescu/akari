package state

import (
	"sync"

	"github.com/aonescu/akari/internal/types"
)

type StateStore interface {
	Record(event types.StateEvent) error
	GetLatestByKind(kind string) []types.StateEvent
	GetByUID(uid string) (types.StateEvent, bool)
}

// In-memory implementation for fallback
type MemoryStore struct {
	mu          sync.RWMutex
	events      []types.StateEvent
	latestByUID map[string]types.StateEvent
	uidsByKind  map[string][]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events:      make([]types.StateEvent, 0),
		latestByUID: make(map[string]types.StateEvent),
		uidsByKind:  make(map[string][]string),
	}
}

func (s *MemoryStore) Record(event types.StateEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)
	s.latestByUID[event.UID] = event

	found := false
	for _, uid := range s.uidsByKind[event.Kind] {
		if uid == event.UID {
			found = true
			break
		}
	}
	if !found {
		s.uidsByKind[event.Kind] = append(s.uidsByKind[event.Kind], event.UID)
	}
	return nil
}

func (s *MemoryStore) GetLatestByKind(kind string) []types.StateEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []types.StateEvent
	for _, uid := range s.uidsByKind[kind] {
		if event, exists := s.latestByUID[uid]; exists {
			results = append(results, event)
		}
	}
	return results
}

func (s *MemoryStore) GetByUID(uid string) (types.StateEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, exists := s.latestByUID[uid]
	return event, exists
}
