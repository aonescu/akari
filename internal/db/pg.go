package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/types"
)

type PostgresStore struct {
	db *sql.DB
	mu sync.RWMutex
	// In-memory cache for fast reads
	latestByUID map[string]types.StateEvent
	uidsByKind  map[string][]string
}

func NewPostgresStore(connStr string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	store := &PostgresStore{
		db:          db,
		latestByUID: make(map[string]types.StateEvent),
		uidsByKind:  make(map[string][]string),
	}

	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Load recent state into cache
	if err := store.loadCache(); err != nil {
		log.Printf("Warning: failed to load cache: %v", err)
	}

	return store, nil
}

func (s *PostgresStore) initSchema() error {
	schema := `
	-- Objects table: authoritative snapshot index
	CREATE TABLE IF NOT EXISTS objects (
		uid TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		namespace TEXT,
		name TEXT NOT NULL,
		owner_uid TEXT,
		labels JSONB,
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_objects_kind ON objects(kind);
	CREATE INDEX IF NOT EXISTS idx_objects_namespace ON objects(namespace);
	CREATE INDEX IF NOT EXISTS idx_objects_labels ON objects USING GIN(labels);

	-- Object versions: append-only state history
	CREATE TABLE IF NOT EXISTS object_versions (
		uid TEXT NOT NULL,
		resource_version TEXT NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		spec JSONB,
		status JSONB,
		actor TEXT,
		event_type TEXT,
		PRIMARY KEY (uid, resource_version)
	);
	CREATE INDEX IF NOT EXISTS idx_object_versions_timestamp ON object_versions(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_object_versions_uid ON object_versions(uid);

	-- Field diffs: materialized for fast evaluation
	CREATE TABLE IF NOT EXISTS field_diffs (
		id SERIAL PRIMARY KEY,
		uid TEXT NOT NULL,
		resource_version TEXT NOT NULL,
		field_path TEXT NOT NULL,
		old_value JSONB,
		new_value JSONB,
		timestamp TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_field_diffs_uid ON field_diffs(uid);
	CREATE INDEX IF NOT EXISTS idx_field_diffs_timestamp ON field_diffs(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_field_diffs_field ON field_diffs(field_path);

	-- Invariants: static registry
	CREATE TABLE IF NOT EXISTS invariants (
		id TEXT PRIMARY KEY,
		version INT NOT NULL,
		definition JSONB NOT NULL,
		severity TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);

	-- Invariant evaluations: cached results
	CREATE TABLE IF NOT EXISTS invariant_evaluations (
		invariant_id TEXT NOT NULL,
		uid TEXT NOT NULL,
		status TEXT NOT NULL, -- satisfied | violated | unknown
		reason TEXT,
		last_evaluated TIMESTAMP NOT NULL,
		PRIMARY KEY (invariant_id, uid)
	);
	CREATE INDEX IF NOT EXISTS idx_evaluations_status ON invariant_evaluations(status);
	CREATE INDEX IF NOT EXISTS idx_evaluations_timestamp ON invariant_evaluations(last_evaluated DESC);

	-- Violations: active failures
	CREATE TABLE IF NOT EXISTS violations (
		violation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		invariant_id TEXT NOT NULL,
		uid TEXT NOT NULL,
		resource_kind TEXT NOT NULL,
		resource_name TEXT NOT NULL,
		namespace TEXT,
		detected_at TIMESTAMP NOT NULL,
		responsible_actor TEXT,
		eliminated_actors JSONB,
		reason TEXT,
		severity TEXT,
		resolved_at TIMESTAMP,
		resolution_reason TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_violations_invariant ON violations(invariant_id);
	CREATE INDEX IF NOT EXISTS idx_violations_detected ON violations(detected_at DESC);
	CREATE INDEX IF NOT EXISTS idx_violations_active ON violations(resolved_at) WHERE resolved_at IS NULL;
	`

	_, err := s.db.Exec(schema)
	return err
}

func (s *PostgresStore) Record(event types.StateEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Upsert object
	_, err = tx.Exec(`
		INSERT INTO objects (uid, kind, namespace, name, labels)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (uid) DO UPDATE SET
			updated_at = NOW(),
			name = EXCLUDED.name,
			namespace = EXCLUDED.namespace,
			labels = EXCLUDED.labels
	`, event.UID, event.Kind, event.Namespace, event.Name, nil)
	if err != nil {
		return fmt.Errorf("failed to upsert object: %w", err)
	}

	// Extract spec/status from full state if available
	var specJSON, statusJSON []byte
	if event.FullState != nil {
		fullJSON, _ := json.Marshal(event.FullState)
		specJSON = fullJSON // Simplified - in production, extract properly
		statusJSON = fullJSON
	}

	// Insert object version (append-only)
	_, err = tx.Exec(`
		INSERT INTO object_versions (uid, resource_version, timestamp, spec, status, actor, event_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (uid, resource_version) DO NOTHING
	`, event.UID, event.Version, event.Timestamp, specJSON, statusJSON, event.Actor, "UPDATE")
	if err != nil {
		return fmt.Errorf("failed to insert object version: %w", err)
	}

	// Insert field diffs
	for field, newValue := range event.FieldDiff {
		newValueJSON, _ := json.Marshal(newValue)

		_, err = tx.Exec(`
			INSERT INTO field_diffs (uid, resource_version, field_path, new_value, timestamp)
			VALUES ($1, $2, $3, $4, $5)
		`, event.UID, event.Version, field, newValueJSON, event.Timestamp)
		if err != nil {
			log.Printf("Warning: failed to insert field diff: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Update in-memory cache
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

func (s *PostgresStore) GetLatestByKind(kind string) []types.StateEvent {
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

func (s *PostgresStore) GetByUID(uid string) (types.StateEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, exists := s.latestByUID[uid]
	return event, exists
}

func (s *PostgresStore) GetHistory(uid string, limit int) ([]types.StateEvent, error) {
	rows, err := s.db.Query(`
		SELECT uid, resource_version, timestamp, actor
		FROM object_versions
		WHERE uid = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []types.StateEvent
	for rows.Next() {
		var event types.StateEvent
		if err := rows.Scan(&event.UID, &event.Version, &event.Timestamp, &event.Actor); err != nil {
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

func (s *PostgresStore) RecordViolation(violation *engine.ViolationResult) error {
	eliminatedJSON, _ := json.Marshal(violation.EliminatedActors)

	_, err := s.db.Exec(`
		INSERT INTO violations (
			invariant_id, uid, resource_kind, resource_name, namespace,
			detected_at, responsible_actor, eliminated_actors, reason, severity
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, violation.InvariantID, "unknown", // UID extraction needed
		violation.InvariantID, violation.AffectedResource, "",
		violation.DetectedAt, violation.ResponsibleActor,
		eliminatedJSON, violation.Reason, violation.Severity)

	return err
}

func (s *PostgresStore) GetViolations(severity string, limit int) ([]*engine.ViolationResult, error) {
	query := `
		SELECT invariant_id, resource_name, detected_at, responsible_actor,
		       eliminated_actors, reason, severity, resolved_at
		FROM violations
		WHERE 1=1
	`
	args := make([]interface{}, 0)

	if severity != "" {
		query += " AND severity = $1"
		args = append(args, severity)
	}

	query += " ORDER BY detected_at DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var violations []*engine.ViolationResult
	for rows.Next() {
		var v engine.ViolationResult
		var eliminatedJSON []byte
		var resolvedAt sql.NullTime

		if err := rows.Scan(
			&v.InvariantID, &v.AffectedResource, &v.DetectedAt,
			&v.ResponsibleActor, &eliminatedJSON, &v.Reason, &v.Severity,
			&resolvedAt,
		); err != nil {
			continue
		}

		json.Unmarshal(eliminatedJSON, &v.EliminatedActors)
		v.Violated = !resolvedAt.Valid
		violations = append(violations, &v)
	}

	return violations, nil
}

func (s *PostgresStore) GetActiveViolations() ([]*engine.ViolationResult, error) {
	rows, err := s.db.Query(`
		SELECT invariant_id, resource_name, detected_at, responsible_actor,
		       eliminated_actors, reason, severity
		FROM violations
		WHERE resolved_at IS NULL
		ORDER BY detected_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var violations []*engine.ViolationResult
	for rows.Next() {
		var v engine.ViolationResult
		var eliminatedJSON []byte

		if err := rows.Scan(
			&v.InvariantID, &v.AffectedResource, &v.DetectedAt,
			&v.ResponsibleActor, &eliminatedJSON, &v.Reason, &v.Severity,
		); err != nil {
			continue
		}

		json.Unmarshal(eliminatedJSON, &v.EliminatedActors)
		v.Violated = true
		violations = append(violations, &v)
	}

	return violations, nil
}

func (s *PostgresStore) UpdateInvariantEvaluation(invID, uid, status, reason string) error {
	_, err := s.db.Exec(`
		INSERT INTO invariant_evaluations (invariant_id, uid, status, reason, last_evaluated)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (invariant_id, uid) DO UPDATE SET
			status = EXCLUDED.status,
			reason = EXCLUDED.reason,
			last_evaluated = NOW()
	`, invID, uid, status, reason)
	return err
}

func (s *PostgresStore) loadCache() error {
	rows, err := s.db.Query(`
		SELECT DISTINCT ON (uid)
			uid, kind, namespace, name
		FROM objects
		ORDER BY uid, updated_at DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var event types.StateEvent
		if err := rows.Scan(&event.UID, &event.Kind, &event.Namespace, &event.Name); err != nil {
			continue
		}

		s.latestByUID[event.UID] = event
		s.uidsByKind[event.Kind] = append(s.uidsByKind[event.Kind], event.UID)
	}

	log.Printf("Loaded %d objects into cache", len(s.latestByUID))
	return nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// Ping checks the database connection
func (s *PostgresStore) Ping() error {
	return s.db.Ping()
}
