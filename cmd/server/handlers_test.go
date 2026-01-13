package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/state"
	"github.com/aonescu/akari/internal/types"
)

func TestAPIServer_HandleHealth(t *testing.T) {
	store := state.NewMemoryStore()
	engine := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, engine)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	api.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%v'", response["status"])
	}
}

func TestAPIServer_HandleReady(t *testing.T) {
	store := state.NewMemoryStore()
	engine := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, engine)

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	api.handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response["ready"].(bool) {
		t.Error("Expected ready to be true")
	}
}

func TestAPIServer_HandleInvariants(t *testing.T) {
	store := state.NewMemoryStore()
	engine := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, engine)

	req := httptest.NewRequest("GET", "/api/v1/invariants", nil)
	w := httptest.NewRecorder()

	api.handleInvariants(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var invariants []dsl.Invariant
	if err := json.NewDecoder(w.Body).Decode(&invariants); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(invariants) == 0 {
		t.Error("Expected at least one invariant")
	}
}

func TestAPIServer_HandleEvaluateInvariants(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	// Add a test pod
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

	req := httptest.NewRequest("POST", "/api/v1/invariants/evaluate", nil)
	w := httptest.NewRecorder()

	api.handleEvaluateInvariants(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := response["violations"]; !ok {
		t.Error("Expected 'violations' key in response")
	}

	if _, ok := response["total_count"]; !ok {
		t.Error("Expected 'total_count' key in response")
	}
}

func TestAPIServer_HandleViolations(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	// Add a test pod with violation
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

	req := httptest.NewRequest("GET", "/api/v1/violations", nil)
	w := httptest.NewRecorder()

	api.handleViolations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var violations []*engine.ViolationResult
	if err := json.NewDecoder(w.Body).Decode(&violations); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(violations) == 0 {
		t.Error("Expected at least one violation result")
	}
}

func TestAPIServer_HandleViolations_WithSeverityFilter(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	// Add a test pod
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

	req := httptest.NewRequest("GET", "/api/v1/violations?severity=critical", nil)
	w := httptest.NewRecorder()

	api.handleViolations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var violations []*engine.ViolationResult
	if err := json.NewDecoder(w.Body).Decode(&violations); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// All returned violations should be critical
	for _, v := range violations {
		if v.Violated && v.Severity != dsl.Critical {
			t.Errorf("Expected only critical violations, got %s", v.Severity)
		}
	}
}

func TestAPIServer_HandleExplain(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	// Add a test pod
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

	body := map[string]string{
		"kind":      "Pod",
		"namespace": "default",
		"name":      "test-pod",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/explain", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.handleExplain(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := response["resource"]; !ok {
		t.Error("Expected 'resource' key in response")
	}

	if _, ok := response["violations"]; !ok {
		t.Error("Expected 'violations' key in response")
	}
}

func TestAPIServer_HandleExplain_NotFound(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	body := map[string]string{
		"kind":      "Pod",
		"namespace": "default",
		"name":      "non-existent-pod",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/explain", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.handleExplain(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestAPIServer_HandleStats(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	api.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := stats["total_invariants"]; !ok {
		t.Error("Expected 'total_invariants' key in stats")
	}

	if _, ok := stats["total_violations"]; !ok {
		t.Error("Expected 'total_violations' key in stats")
	}

	if _, ok := stats["by_severity"]; !ok {
		t.Error("Expected 'by_severity' key in stats")
	}
}

func TestAPIServer_CORSMiddleware(t *testing.T) {
	store := state.NewMemoryStore()
	eng := engine.NewInvariantEngine(store)
	api := NewAPIServer(store, eng)

	req := httptest.NewRequest("OPTIONS", "/health", nil)
	w := httptest.NewRecorder()

	handler := api.corsMiddleware(http.HandlerFunc(api.handleHealth))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", w.Code)
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected CORS header to be set")
	}
}
