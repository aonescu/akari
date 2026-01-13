package engine

import (
	"testing"
	"time"

	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/state"
	"github.com/aonescu/akari/internal/types"
)

func TestInvariantEngine_EvaluateAll(t *testing.T) {
	store := state.NewMemoryStore()
	eng := NewInvariantEngine(store)

	// Add some test resources
	pod := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"status.conditions[Ready].status": "False",
		},
		Actor: "kubelet",
	}

	store.Record(pod)

	violations := eng.EvaluateAll()
	if len(violations) == 0 {
		t.Fatal("Expected at least some evaluation results")
	}

	// Check that we got results for the pod
	found := false
	for _, v := range violations {
		if v.AffectedResource == "default/test-pod" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find violation for test-pod")
	}
}

func TestInvariantEngine_Evaluate(t *testing.T) {
	store := state.NewMemoryStore()
	eng := NewInvariantEngine(store)

	inv := dsl.Invariant{
		ID:          "pod_ready",
		Description: "Pod should be in Ready state",
		Subject:     dsl.Subject{Kind: "Pod"},
		Predicate: &dsl.Predicate{
			Field:    "status.conditions[Ready].status",
			Operator: dsl.Equals,
			Value:    "True",
		},
		Responsibility: dsl.Responsibility{
			Primary: "kubelet",
		},
		Severity: dsl.Critical,
	}

	// Add a pod that violates the invariant
	pod := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"status.conditions[Ready].status": "False",
		},
		Actor: "kubelet",
	}

	store.Record(pod)

	violations := eng.Evaluate(inv)
	if len(violations) == 0 {
		t.Fatal("Expected at least one violation")
	}

	if violations[0].InvariantID != "pod_ready" {
		t.Errorf("Expected invariant ID 'pod_ready', got '%s'", violations[0].InvariantID)
	}

	if !violations[0].Violated {
		t.Error("Expected violation to be true")
	}
}

func TestInvariantEngine_GetInvariants(t *testing.T) {
	store := state.NewMemoryStore()
	eng := NewInvariantEngine(store)

	invariants := eng.GetInvariants()
	if len(invariants) == 0 {
		t.Fatal("Expected invariants to be loaded")
	}

	// Check that some expected invariants are present
	invariantMap := make(map[string]dsl.Invariant)
	for _, inv := range invariants {
		invariantMap[inv.ID] = inv
	}

	expectedIDs := []string{"pod_ready", "node_ready", "pod_scheduled"}
	for _, id := range expectedIDs {
		if _, exists := invariantMap[id]; !exists {
			t.Errorf("Expected invariant '%s' not found", id)
		}
	}
}

func TestInvariantEngine_EvaluateWithDependencies(t *testing.T) {
	store := state.NewMemoryStore()
	eng := NewInvariantEngine(store)

	// Add a pod that's not scheduled (should violate pod_scheduled)
	pod := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"spec.nodeName": nil, // Not scheduled
		},
		Actor: "kube-scheduler",
	}

	store.Record(pod)

	// Evaluate pod_ready which requires pod_scheduled
	invariants := eng.GetInvariants()
	var podReadyInv dsl.Invariant
	for _, inv := range invariants {
		if inv.ID == "pod_ready" {
			podReadyInv = inv
			break
		}
	}

	if podReadyInv.ID == "" {
		t.Fatal("pod_ready invariant not found")
	}

	violations := eng.Evaluate(podReadyInv)
	if len(violations) == 0 {
		t.Fatal("Expected violations for unscheduled pod")
	}

	// Should have violation due to dependency failure
	foundDependencyViolation := false
	for _, v := range violations {
		if v.Violated && (v.InvariantID == "pod_ready" || v.Reason != "") {
			foundDependencyViolation = true
			break
		}
	}

	if !foundDependencyViolation {
		t.Error("Expected violation due to dependency failure")
	}
}
