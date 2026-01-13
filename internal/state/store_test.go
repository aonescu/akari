package state

import (
	"testing"
	"time"

	"github.com/aonescu/akari/internal/types"
)

func TestMemoryStore_Record(t *testing.T) {
	store := NewMemoryStore()

	event := types.StateEvent{
		UID:       "test-uid-1",
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"status.phase": "Running",
		},
		Actor: "kubelet",
	}

	err := store.Record(event)
	if err != nil {
		t.Fatalf("Record() failed: %v", err)
	}

	// Verify event was stored
	retrieved, exists := store.GetByUID("test-uid-1")
	if !exists {
		t.Fatal("Event not found after Record()")
	}

	if retrieved.UID != event.UID {
		t.Errorf("Expected UID %s, got %s", event.UID, retrieved.UID)
	}
	if retrieved.Name != event.Name {
		t.Errorf("Expected Name %s, got %s", event.Name, retrieved.Name)
	}
}

func TestMemoryStore_GetLatestByKind(t *testing.T) {
	store := NewMemoryStore()

	// Add multiple pods
	pod1 := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Namespace: "default",
		Name:      "pod-1",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: make(map[string]interface{}),
		Actor:     "kubelet",
	}

	pod2 := types.StateEvent{
		UID:       "pod-2",
		Kind:      "Pod",
		Namespace: "default",
		Name:      "pod-2",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: make(map[string]interface{}),
		Actor:     "kubelet",
	}

	node1 := types.StateEvent{
		UID:       "node-1",
		Kind:      "Node",
		Name:      "node-1",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: make(map[string]interface{}),
		Actor:     "node-controller",
	}

	store.Record(pod1)
	store.Record(pod2)
	store.Record(node1)

	pods := store.GetLatestByKind("Pod")
	if len(pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(pods))
	}

	nodes := store.GetLatestByKind("Node")
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(nodes))
	}

	// Verify empty kind returns empty slice
	empty := store.GetLatestByKind("NonExistent")
	if len(empty) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(empty))
	}
}

func TestMemoryStore_UpdateEvent(t *testing.T) {
	store := NewMemoryStore()

	event1 := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Name:      "pod-1",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"status.phase": "Pending",
		},
		Actor: "kube-scheduler",
	}

	store.Record(event1)

	// Update the same pod with new version
	event2 := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Name:      "pod-1",
		Version:   "2",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"status.phase": "Running",
		},
		Actor: "kubelet",
	}

	store.Record(event2)

	// Verify latest version is stored
	retrieved, exists := store.GetByUID("pod-1")
	if !exists {
		t.Fatal("Event not found")
	}

	if retrieved.Version != "2" {
		t.Errorf("Expected version 2, got %s", retrieved.Version)
	}

	phase, ok := retrieved.FieldDiff["status.phase"]
	if !ok {
		t.Fatal("status.phase not found in FieldDiff")
	}
	if phase != "Running" {
		t.Errorf("Expected phase 'Running', got %v", phase)
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()

	// Test concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			event := types.StateEvent{
				UID:       "pod-" + string(rune(id)),
				Kind:      "Pod",
				Name:      "pod-" + string(rune(id)),
				Version:   "1",
				Timestamp: time.Now(),
				FieldDiff: make(map[string]interface{}),
				Actor:     "kubelet",
			}
			store.Record(event)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all events were recorded
	pods := store.GetLatestByKind("Pod")
	if len(pods) != 10 {
		t.Errorf("Expected 10 pods, got %d", len(pods))
	}
}
