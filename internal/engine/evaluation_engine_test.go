package engine

import (
	"testing"
	"time"

	"github.com/aonescu/akari/internal/authority"
	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/state"
	"github.com/aonescu/akari/internal/types"
)

func TestEvaluatePredicate_Exists(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "status.phase",
		Operator: dsl.Exists,
	}

	// Test with existing field
	event := types.StateEvent{
		UID:  "pod-1",
		Kind: "Pod",
		Name: "test-pod",
		FieldDiff: map[string]interface{}{
			"status.phase": "Running",
		},
	}

	satisfied, reason := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Errorf("Expected predicate to be satisfied, got reason: %s", reason)
	}

	// Test with missing field
	event2 := types.StateEvent{
		UID:       "pod-2",
		Kind:      "Pod",
		Name:      "test-pod-2",
		FieldDiff: make(map[string]interface{}),
	}

	satisfied, reason = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated when field is missing")
	}
	if reason == "" {
		t.Error("Expected non-empty reason for violation")
	}
}

func TestEvaluatePredicate_NotExists(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "metadata.deletionTimestamp",
		Operator: dsl.NotExists,
	}

	// Test with field not present (should pass)
	event := types.StateEvent{
		UID:       "pod-1",
		Kind:      "Pod",
		Name:      "test-pod",
		FieldDiff: make(map[string]interface{}),
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied when field does not exist")
	}

	// Test with field present (should fail)
	event2 := types.StateEvent{
		UID:  "pod-2",
		Kind: "Pod",
		Name: "test-pod-2",
		FieldDiff: map[string]interface{}{
			"metadata.deletionTimestamp": "2024-01-01T00:00:00Z",
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated when field exists")
	}
}

func TestEvaluatePredicate_Equals(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "status.conditions[Ready].status",
		Operator: dsl.Equals,
		Value:    "True",
	}

	// Test with matching value
	event := types.StateEvent{
		UID:  "pod-1",
		Kind: "Pod",
		Name: "test-pod",
		FieldDiff: map[string]interface{}{
			"status.conditions[Ready].status": "True",
		},
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied with matching value")
	}

	// Test with non-matching value
	event2 := types.StateEvent{
		UID:  "pod-2",
		Kind: "Pod",
		Name: "test-pod-2",
		FieldDiff: map[string]interface{}{
			"status.conditions[Ready].status": "False",
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated with non-matching value")
	}
}

func TestEvaluatePredicate_NotEquals(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "status.containerStatuses.waiting.reason",
		Operator: dsl.NotEquals,
		Value:    "ImagePullBackOff",
	}

	// Test with different value (should pass)
	event := types.StateEvent{
		UID:  "pod-1",
		Kind: "Pod",
		Name: "test-pod",
		FieldDiff: map[string]interface{}{
			"status.containerStatuses.waiting.reason": "ErrImagePull",
		},
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied with different value")
	}

	// Test with matching value (should fail)
	event2 := types.StateEvent{
		UID:  "pod-2",
		Kind: "Pod",
		Name: "test-pod-2",
		FieldDiff: map[string]interface{}{
			"status.containerStatuses.waiting.reason": "ImagePullBackOff",
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated with matching value")
	}
}

func TestEvaluatePredicate_GreaterThan(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "status.availableReplicas",
		Operator: dsl.GreaterThan,
		Value:    0,
	}

	// Test with value greater than threshold
	event := types.StateEvent{
		UID:  "deploy-1",
		Kind: "Deployment",
		Name: "test-deploy",
		FieldDiff: map[string]interface{}{
			"status.availableReplicas": 3,
		},
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied with value > 0")
	}

	// Test with value equal to threshold (should fail)
	event2 := types.StateEvent{
		UID:  "deploy-2",
		Kind: "Deployment",
		Name: "test-deploy-2",
		FieldDiff: map[string]interface{}{
			"status.availableReplicas": 0,
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated with value == 0")
	}
}

func TestEvaluatePredicate_LessThan(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "status.containerStatuses[*].restartCount",
		Operator: dsl.LessThan,
		Value:    3,
	}

	// Test with value less than threshold
	event := types.StateEvent{
		UID:  "pod-1",
		Kind: "Pod",
		Name: "test-pod",
		FieldDiff: map[string]interface{}{
			"status.containerStatuses[*].restartCount": 1,
		},
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied with value < 3")
	}

	// Test with value equal to threshold (should fail)
	event2 := types.StateEvent{
		UID:  "pod-2",
		Kind: "Pod",
		Name: "test-pod-2",
		FieldDiff: map[string]interface{}{
			"status.containerStatuses[*].restartCount": 3,
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated with value == 3")
	}
}

func TestEvaluatePredicate_AnyTrue(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "endpoints[*].addresses",
		Operator: dsl.AnyTrue,
	}

	// Test with array containing truthy value
	event := types.StateEvent{
		UID:  "svc-1",
		Kind: "Service",
		Name: "test-svc",
		FieldDiff: map[string]interface{}{
			"endpoints[*].addresses": []interface{}{"10.0.0.1", "10.0.0.2"},
		},
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied with truthy array elements")
	}

	// Test with empty array (should fail)
	event2 := types.StateEvent{
		UID:  "svc-2",
		Kind: "Service",
		Name: "test-svc-2",
		FieldDiff: map[string]interface{}{
			"endpoints[*].addresses": []interface{}{},
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated with empty array")
	}
}

func TestEvaluatePredicate_AllTrue(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

	pred := dsl.Predicate{
		Field:    "status.containerStatuses[*].state.running",
		Operator: dsl.AllTrue,
	}

	// Test with all containers running
	event := types.StateEvent{
		UID:  "pod-1",
		Kind: "Pod",
		Name: "test-pod",
		FieldDiff: map[string]interface{}{
			"status.containerStatuses[*].state.running": []interface{}{true, true},
		},
	}

	satisfied, _ := eng.evaluatePredicateWithReason(pred, event)
	if !satisfied {
		t.Error("Expected predicate to be satisfied when all containers running")
	}

	// Test with some containers not running (should fail)
	event2 := types.StateEvent{
		UID:  "pod-2",
		Kind: "Pod",
		Name: "test-pod-2",
		FieldDiff: map[string]interface{}{
			"status.containerStatuses[*].state.running": []interface{}{true, false},
		},
	}

	satisfied, _ = eng.evaluatePredicateWithReason(pred, event2)
	if satisfied {
		t.Error("Expected predicate to be violated when not all containers running")
	}
}

func TestEvaluateWithContext_Violation(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

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

	ctx := types.EvaluationContext{
		Resource: types.StateEvent{
			UID:       "pod-1",
			Kind:      "Pod",
			Name:      "test-pod",
			Namespace: "default",
			FieldDiff: map[string]interface{}{
				"status.conditions[Ready].status": "False",
			},
			Actor:     "kubelet",
			Timestamp: time.Now(),
		},
		RelatedStates: make(map[string]types.StateEvent),
		Timestamp:     time.Now(),
	}

	result := eng.EvaluateWithContext(inv, ctx)
	if result == nil {
		t.Fatal("Expected violation result, got nil")
	}

	if !result.Violated {
		t.Error("Expected violation to be true")
	}

	if result.ResponsibleActor != "kubelet" {
		t.Errorf("Expected responsible actor 'kubelet', got '%s'", result.ResponsibleActor)
	}

	if result.Severity != dsl.Critical {
		t.Errorf("Expected severity 'critical', got '%s'", result.Severity)
	}
}

func TestEvaluateWithContext_Satisfied(t *testing.T) {
	store := state.NewMemoryStore()
	authorityMap := authority.NewControllerAuthorityMap()
	eng := NewEvaluationEngine(store, authorityMap)

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

	ctx := types.EvaluationContext{
		Resource: types.StateEvent{
			UID:       "pod-1",
			Kind:      "Pod",
			Name:      "test-pod",
			Namespace: "default",
			FieldDiff: map[string]interface{}{
				"status.conditions[Ready].status": "True",
			},
			Actor:     "kubelet",
			Timestamp: time.Now(),
		},
		RelatedStates: make(map[string]types.StateEvent),
		Timestamp:     time.Now(),
	}

	result := eng.EvaluateWithContext(inv, ctx)
	if result != nil {
		t.Errorf("Expected nil result for satisfied invariant, got violation: %+v", result)
	}
}
