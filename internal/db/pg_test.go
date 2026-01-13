package db

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/types"
	_ "github.com/lib/pq"
)

// Test database connection string
func getTestDBConnString() string {
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/causality_test?sslmode=disable"
	}
	return connStr
}

// Setup creates a fresh test database
func setupTestDB(t *testing.T) (*PostgresStore, func()) {
	connStr := getTestDBConnString()

	// Create the store
	store, err := NewPostgresStore(connStr)
	if err != nil {
		t.Skipf("Skipping test: PostgreSQL not available: %v", err)
		return nil, func() {}
	}

	// Cleanup function
	cleanup := func() {
		// Drop all data
		store.db.Exec("TRUNCATE objects, object_versions, field_diffs, invariants, invariant_evaluations, violations CASCADE")
		store.Close()
	}

	return store, cleanup
}

// TestNewPostgresStore tests store initialization
func TestNewPostgresStore(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	// Test ping
	if err := store.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// Verify tables exist
	tables := []string{
		"objects", "object_versions", "field_diffs",
		"invariants", "invariant_evaluations", "violations",
	}

	for _, table := range tables {
		var exists bool
		err := store.db.QueryRow(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_name = $1
			)
		`, table).Scan(&exists)

		if err != nil {
			t.Fatalf("Failed to check table %s: %v", table, err)
		}

		if !exists {
			t.Errorf("Table %s does not exist", table)
		}
	}
}

// TestRecordStateEvent tests recording state events
func TestRecordStateEvent(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	event := types.StateEvent{
		UID:       "pod-test-123",
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		Version:   "12345",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{
			"status.phase":                    "Running",
			"status.conditions[Ready].status": "True",
		},
		Actor: "kubelet/node-1",
	}

	// Record event
	err := store.Record(event)
	if err != nil {
		t.Fatalf("Failed to record event: %v", err)
	}

	// Verify object was inserted
	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM objects WHERE uid = $1", event.UID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query objects: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 object, got %d", count)
	}

	// Verify object version was inserted
	err = store.db.QueryRow("SELECT COUNT(*) FROM object_versions WHERE uid = $1", event.UID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query object_versions: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 object version, got %d", count)
	}

	// Verify field diffs were inserted
	err = store.db.QueryRow("SELECT COUNT(*) FROM field_diffs WHERE uid = $1", event.UID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query field_diffs: %v", err)
	}
	if count != 2 { // Two fields in FieldDiff
		t.Errorf("Expected 2 field diffs, got %d", count)
	}

	// Verify in-memory cache was updated
	if _, exists := store.latestByUID[event.UID]; !exists {
		t.Error("Event not found in in-memory cache")
	}
}

// TestGetLatestByKind tests retrieving resources by kind
func TestGetLatestByKind(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	// Create multiple events of different kinds
	events := []types.StateEvent{
		{
			UID:       "pod-1",
			Kind:      "Pod",
			Namespace: "default",
			Name:      "pod-1",
			Version:   "1",
			Timestamp: time.Now(),
			FieldDiff: map[string]interface{}{"status.phase": "Running"},
			Actor:     "kubelet",
		},
		{
			UID:       "pod-2",
			Kind:      "Pod",
			Namespace: "default",
			Name:      "pod-2",
			Version:   "1",
			Timestamp: time.Now(),
			FieldDiff: map[string]interface{}{"status.phase": "Pending"},
			Actor:     "kubelet",
		},
		{
			UID:       "node-1",
			Kind:      "Node",
			Namespace: "",
			Name:      "node-1",
			Version:   "1",
			Timestamp: time.Now(),
			FieldDiff: map[string]interface{}{"status.conditions[Ready].status": "True"},
			Actor:     "node-controller",
		},
	}

	for _, event := range events {
		if err := store.Record(event); err != nil {
			t.Fatalf("Failed to record event: %v", err)
		}
	}

	// Get Pods
	pods := store.GetLatestByKind("Pod")
	if len(pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(pods))
	}

	// Get Nodes
	nodes := store.GetLatestByKind("Node")
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(nodes))
	}

	// Get non-existent kind
	services := store.GetLatestByKind("Service")
	if len(services) != 0 {
		t.Errorf("Expected 0 services, got %d", len(services))
	}
}

// TestGetByUID tests retrieving a specific resource by UID
func TestGetByUID(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	event := types.StateEvent{
		UID:       "pod-unique-123",
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		Version:   "1",
		Timestamp: time.Now(),
		FieldDiff: map[string]interface{}{"status.phase": "Running"},
		Actor:     "kubelet",
	}

	if err := store.Record(event); err != nil {
		t.Fatalf("Failed to record event: %v", err)
	}

	// Get existing UID
	retrieved, exists := store.GetByUID("pod-unique-123")
	if !exists {
		t.Error("Expected to find event by UID")
	}
	if retrieved.UID != event.UID {
		t.Errorf("Expected UID %s, got %s", event.UID, retrieved.UID)
	}

	// Get non-existent UID
	_, exists = store.GetByUID("non-existent")
	if exists {
		t.Error("Expected not to find non-existent UID")
	}
}

// TestGetHistory tests retrieving object history
func TestGetHistory(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	uid := "pod-history-test"

	// Create multiple versions
	for i := 1; i <= 5; i++ {
		event := types.StateEvent{
			UID:       uid,
			Kind:      "Pod",
			Namespace: "default",
			Name:      "test-pod",
			Version:   fmt.Sprintf("%d", i),
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			FieldDiff: map[string]interface{}{"status.phase": fmt.Sprintf("Phase%d", i)},
			Actor:     "kubelet",
		}
		if err := store.Record(event); err != nil {
			t.Fatalf("Failed to record event: %v", err)
		}
	}

	// Get history with limit
	history, err := store.GetHistory(uid, 3)
	if err != nil {
		t.Fatalf("Failed to get history: %v", err)
	}

	if len(history) != 3 {
		t.Errorf("Expected 3 history entries, got %d", len(history))
	}

	// Verify order (should be descending by timestamp)
	if len(history) >= 2 {
		if history[0].Timestamp.Before(history[1].Timestamp) {
			t.Error("History not returned in descending order")
		}
	}
}

// TestRecordViolation tests recording violations
func TestRecordViolation(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	violation := &engine.ViolationResult{
		InvariantID:      "pod_ready",
		Violated:         true,
		Reason:           "Pod not ready",
		ResponsibleActor: "kubelet",
		EliminatedActors: []string{"kube-scheduler", "deployment-controller"},
		AffectedResource: "default/test-pod",
		DetectedAt:       time.Now(),
		Severity:         "critical",
	}

	err := store.RecordViolation(violation)
	if err != nil {
		t.Fatalf("Failed to record violation: %v", err)
	}

	// Verify violation was inserted
	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM violations WHERE invariant_id = $1", violation.InvariantID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query violations: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 violation, got %d", count)
	}
}

// TestGetViolations tests retrieving violations with filters
func TestGetViolations(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	// Create violations with different severities
	violations := []*engine.ViolationResult{
		{
			InvariantID:      "pod_ready",
			Violated:         true,
			Reason:           "Pod not ready",
			ResponsibleActor: "kubelet",
			EliminatedActors: []string{},
			AffectedResource: "default/pod-1",
			DetectedAt:       time.Now(),
			Severity:         "critical",
		},
		{
			InvariantID:      "no_crashloop",
			Violated:         true,
			Reason:           "Crash loop detected",
			ResponsibleActor: "kubelet",
			EliminatedActors: []string{},
			AffectedResource: "default/pod-2",
			DetectedAt:       time.Now(),
			Severity:         "degraded",
		},
		{
			InvariantID:      "node_ready",
			Violated:         true,
			Reason:           "Node not ready",
			ResponsibleActor: "node-controller",
			EliminatedActors: []string{},
			AffectedResource: "node-1",
			DetectedAt:       time.Now(),
			Severity:         "critical",
		},
	}

	for _, v := range violations {
		if err := store.RecordViolation(v); err != nil {
			t.Fatalf("Failed to record violation: %v", err)
		}
	}

	// Get all violations
	all, err := store.GetViolations("", 100)
	if err != nil {
		t.Fatalf("Failed to get violations: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Expected 3 violations, got %d", len(all))
	}

	// Get critical violations only
	critical, err := store.GetViolations("critical", 100)
	if err != nil {
		t.Fatalf("Failed to get critical violations: %v", err)
	}
	if len(critical) != 2 {
		t.Errorf("Expected 2 critical violations, got %d", len(critical))
	}

	// Test limit
	limited, err := store.GetViolations("", 1)
	if err != nil {
		t.Fatalf("Failed to get limited violations: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("Expected 1 violation with limit, got %d", len(limited))
	}
}

// TestGetActiveViolations tests retrieving only unresolved violations
func TestGetActiveViolations(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	// Create an active violation
	activeViolation := &engine.ViolationResult{
		InvariantID:      "pod_ready",
		Violated:         true,
		Reason:           "Pod not ready",
		ResponsibleActor: "kubelet",
		EliminatedActors: []string{},
		AffectedResource: "default/pod-1",
		DetectedAt:       time.Now(),
		Severity:         "critical",
	}

	if err := store.RecordViolation(activeViolation); err != nil {
		t.Fatalf("Failed to record active violation: %v", err)
	}

	// Create a resolved violation
	store.db.Exec(`
		INSERT INTO violations (
			invariant_id, uid, resource_kind, resource_name, namespace,
			detected_at, responsible_actor, eliminated_actors, reason, severity, resolved_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, "pod_scheduled", "pod-2", "Pod", "default/pod-2", "default",
		time.Now(), "kube-scheduler", "[]", "Resolved", "critical", time.Now())

	// Get active violations
	active, err := store.GetActiveViolations()
	if err != nil {
		t.Fatalf("Failed to get active violations: %v", err)
	}

	if len(active) != 1 {
		t.Errorf("Expected 1 active violation, got %d", len(active))
	}

	if len(active) > 0 && active[0].InvariantID != "pod_ready" {
		t.Errorf("Expected pod_ready violation, got %s", active[0].InvariantID)
	}
}

// TestUpdateInvariantEvaluation tests caching evaluation results
func TestUpdateInvariantEvaluation(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	// First evaluation
	err := store.UpdateInvariantEvaluation("pod_ready", "pod-123", "violated", "Not ready")
	if err != nil {
		t.Fatalf("Failed to update evaluation: %v", err)
	}

	// Verify insertion
	var status, reason string
	err = store.db.QueryRow(`
		SELECT status, reason FROM invariant_evaluations 
		WHERE invariant_id = $1 AND uid = $2
	`, "pod_ready", "pod-123").Scan(&status, &reason)

	if err != nil {
		t.Fatalf("Failed to query evaluation: %v", err)
	}

	if status != "violated" {
		t.Errorf("Expected status 'violated', got '%s'", status)
	}

	if reason != "Not ready" {
		t.Errorf("Expected reason 'Not ready', got '%s'", reason)
	}

	// Update evaluation (should upsert)
	err = store.UpdateInvariantEvaluation("pod_ready", "pod-123", "satisfied", "Now ready")
	if err != nil {
		t.Fatalf("Failed to update evaluation: %v", err)
	}

	// Verify update
	err = store.db.QueryRow(`
		SELECT status, reason FROM invariant_evaluations 
		WHERE invariant_id = $1 AND uid = $2
	`, "pod_ready", "pod-123").Scan(&status, &reason)

	if err != nil {
		t.Fatalf("Failed to query updated evaluation: %v", err)
	}

	if status != "satisfied" {
		t.Errorf("Expected status 'satisfied', got '%s'", status)
	}

	// Verify only one record exists (no duplicates)
	var count int
	err = store.db.QueryRow(`
		SELECT COUNT(*) FROM invariant_evaluations 
		WHERE invariant_id = $1 AND uid = $2
	`, "pod_ready", "pod-123").Scan(&count)

	if err != nil {
		t.Fatalf("Failed to count evaluations: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 evaluation record, got %d", count)
	}
}

// TestConcurrentRecords tests thread-safety
func TestConcurrentRecords(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	const numGoroutines = 10
	const recordsPerGoroutine = 10

	done := make(chan bool)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < recordsPerGoroutine; j++ {
				event := types.StateEvent{
					UID:       fmt.Sprintf("pod-%d-%d", id, j),
					Kind:      "Pod",
					Namespace: "default",
					Name:      fmt.Sprintf("pod-%d-%d", id, j),
					Version:   "1",
					Timestamp: time.Now(),
					FieldDiff: map[string]interface{}{"status.phase": "Running"},
					Actor:     "kubelet",
				}
				if err := store.Record(event); err != nil {
					t.Errorf("Failed to record event: %v", err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all records were inserted
	var count int
	err := store.db.QueryRow("SELECT COUNT(*) FROM objects").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count objects: %v", err)
	}

	expected := numGoroutines * recordsPerGoroutine
	if count != expected {
		t.Errorf("Expected %d objects, got %d", expected, count)
	}
}

// TestLoadCache tests cache loading on initialization
func TestLoadCache(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	// Insert data directly into database
	_, err := store.db.Exec(`
		INSERT INTO objects (uid, kind, namespace, name)
		VALUES ('pod-1', 'Pod', 'default', 'test-pod-1'),
		       ('pod-2', 'Pod', 'default', 'test-pod-2'),
		       ('node-1', 'Node', '', 'test-node-1')
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Create new store to trigger cache load
	newStore, err := NewPostgresStore(getTestDBConnString())
	if err != nil {
		t.Fatalf("Failed to create new store: %v", err)
	}
	defer newStore.Close()

	// Verify cache was loaded
	if len(newStore.latestByUID) != 3 {
		t.Errorf("Expected 3 items in cache, got %d", len(newStore.latestByUID))
	}

	pods := newStore.GetLatestByKind("Pod")
	if len(pods) != 2 {
		t.Errorf("Expected 2 pods from cache, got %d", len(pods))
	}
}

// TestAppendOnlyHistory tests that object_versions is truly append-only
func TestAppendOnlyHistory(t *testing.T) {
	store, cleanup := setupTestDB(t)
	if store == nil {
		return
	}
	defer cleanup()

	uid := "pod-append-test"

	// Record same UID multiple times with different versions
	for i := 1; i <= 3; i++ {
		event := types.StateEvent{
			UID:       uid,
			Kind:      "Pod",
			Namespace: "default",
			Name:      "test-pod",
			Version:   fmt.Sprintf("v%d", i),
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			FieldDiff: map[string]interface{}{"status.phase": fmt.Sprintf("Phase%d", i)},
			Actor:     "kubelet",
		}
		if err := store.Record(event); err != nil {
			t.Fatalf("Failed to record event: %v", err)
		}
	}

	// Verify all versions exist in object_versions
	var count int
	err := store.db.QueryRow("SELECT COUNT(*) FROM object_versions WHERE uid = $1", uid).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count versions: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected 3 versions in history, got %d", count)
	}

	// Verify only one entry in objects table (latest)
	err = store.db.QueryRow("SELECT COUNT(*) FROM objects WHERE uid = $1", uid).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count objects: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 object entry, got %d", count)
	}
}

// BenchmarkRecord benchmarks the Record operation
func BenchmarkRecord(b *testing.B) {
	store, cleanup := setupTestDB(&testing.T{})
	if store == nil {
		b.Skip("PostgreSQL not available")
		return
	}
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := types.StateEvent{
			UID:       fmt.Sprintf("pod-bench-%d", i),
			Kind:      "Pod",
			Namespace: "default",
			Name:      fmt.Sprintf("pod-%d", i),
			Version:   "1",
			Timestamp: time.Now(),
			FieldDiff: map[string]interface{}{
				"status.phase":                    "Running",
				"status.conditions[Ready].status": "True",
			},
			Actor: "kubelet",
		}
		if err := store.Record(event); err != nil {
			b.Fatalf("Failed to record: %v", err)
		}
	}
}

// BenchmarkGetLatestByKind benchmarks retrieval by kind
func BenchmarkGetLatestByKind(b *testing.B) {
	store, cleanup := setupTestDB(&testing.T{})
	if store == nil {
		b.Skip("PostgreSQL not available")
		return
	}
	defer cleanup()

	// Seed with data
	for i := 0; i < 100; i++ {
		event := types.StateEvent{
			UID:       fmt.Sprintf("pod-%d", i),
			Kind:      "Pod",
			Namespace: "default",
			Name:      fmt.Sprintf("pod-%d", i),
			Version:   "1",
			Timestamp: time.Now(),
			FieldDiff: map[string]interface{}{"status.phase": "Running"},
			Actor:     "kubelet",
		}
		store.Record(event)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.GetLatestByKind("Pod")
	}
}
