package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/aonescu/akari/internal/db"
	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/formatting"
	"github.com/aonescu/akari/internal/types"
)

// GET /api/v1/violations?severity=critical&limit=50
func (api *APIServer) handleViolations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	severity := r.URL.Query().Get("severity")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	var violations []*engine.ViolationResult

	if pgStore, ok := api.store.(*db.PostgresStore); ok {
		// Get from database
		dbViolations, err := pgStore.GetViolations(severity, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		violations = dbViolations
	} else {
		// Get from live evaluation
		violations = api.engine.EvaluateAll()

		// Filter by severity if specified
		if severity != "" {
			filtered := make([]*engine.ViolationResult, 0)
			for _, v := range violations {
				if v.Violated && string(v.Severity) == severity {
					filtered = append(filtered, v)
				}
			}
			violations = filtered
		}

		// Limit results
		if len(violations) > limit {
			violations = violations[:limit]
		}
	}

	api.respondJSON(w, violations)
}

// GET /api/v1/violations/active
func (api *APIServer) handleActiveViolations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if pgStore, ok := api.store.(*db.PostgresStore); ok {
		violations, err := pgStore.GetActiveViolations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		api.respondJSON(w, violations)
	} else {
		// Fall back to current evaluation
		violations := api.engine.EvaluateAll()
		active := make([]*engine.ViolationResult, 0)
		for _, v := range violations {
			if v.Violated {
				active = append(active, v)
			}
		}
		api.respondJSON(w, active)
	}
}

// POST /api/v1/explain
// Body: {"kind": "Pod", "namespace": "default", "name": "api-pod"}
func (api *APIServer) handleExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Find the resource
	resources := api.store.GetLatestByKind(req.Kind)
	var target *types.StateEvent
	for _, res := range resources {
		if res.Namespace == req.Namespace && res.Name == req.Name {
			target = &res
			break
		}
	}

	if target == nil {
		http.Error(w, "Resource not found", http.StatusNotFound)
		return
	}

	// Evaluate all invariants for this resource
	violations := make([]*engine.ViolationResult, 0)
	for _, inv := range api.engine.GetInvariants() {
		if inv.Subject.Kind == req.Kind {
			// Note: evaluateSubject is not exported, need to use Evaluate method
			results := api.engine.Evaluate(inv)
			for _, result := range results {
				if result != nil && result.Violated {
					violations = append(violations, result)
				}
			}
		}
	}

	response := map[string]interface{}{
		"resource": map[string]string{
			"kind":      req.Kind,
			"namespace": req.Namespace,
			"name":      req.Name,
			"uid":       target.UID,
		},
		"violations":   violations,
		"explanations": formatting.FormatMultipleExplanations(violations),
	}

	api.respondJSON(w, response)
}

// GET /api/v1/explain/resource?kind=Pod&namespace=default&name=api-pod
func (api *APIServer) handleExplainResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	kind := r.URL.Query().Get("kind")
	namespace := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("name")

	if kind == "" || name == "" {
		http.Error(w, "kind and name are required", http.StatusBadRequest)
		return
	}

	// Find the resource
	resources := api.store.GetLatestByKind(kind)
	var target *types.StateEvent
	for _, res := range resources {
		if res.Namespace == namespace && res.Name == name {
			target = &res
			break
		}
	}

	if target == nil {
		http.Error(w, "Resource not found", http.StatusNotFound)
		return
	}

	// Evaluate all invariants
	violations := make([]*engine.ViolationResult, 0)
	for _, inv := range api.engine.GetInvariants() {
		if inv.Subject.Kind == kind {
			results := api.engine.Evaluate(inv)
			for _, result := range results {
				if result != nil {
					violations = append(violations, result)
				}
			}
		}
	}

	response := map[string]interface{}{
		"resource": map[string]string{
			"kind":      kind,
			"namespace": namespace,
			"name":      name,
			"uid":       target.UID,
		},
		"violations": violations,
		"summary":    formatting.GenerateSummary(violations),
	}

	api.respondJSON(w, response)
}

// GET /api/v1/causal-chain?violation_id=uuid
func (api *APIServer) handleCausalChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	invariantID := r.URL.Query().Get("invariant_id")
	if invariantID == "" {
		http.Error(w, "invariant_id is required", http.StatusBadRequest)
		return
	}

	// Build causal chain
	chain := api.buildCausalChain(invariantID)

	response := map[string]interface{}{
		"invariant_id": invariantID,
		"chain":        chain,
		"depth":        len(chain),
	}

	api.respondJSON(w, response)
}

// GET /api/v1/history?uid=pod-123&limit=20
func (api *APIServer) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uid := r.URL.Query().Get("uid")
	if uid == "" {
		http.Error(w, "uid is required", http.StatusBadRequest)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	if pgStore, ok := api.store.(*db.PostgresStore); ok {
		history, err := pgStore.GetHistory(uid, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		api.respondJSON(w, history)
	} else {
		http.Error(w, "History only available with PostgreSQL storage", http.StatusServiceUnavailable)
	}
}

// GET /api/v1/invariants
func (api *APIServer) handleInvariants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	invariants := api.engine.GetInvariants()
	api.respondJSON(w, invariants)
}

// POST /api/v1/invariants/evaluate
func (api *APIServer) handleEvaluateInvariants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	violations := api.engine.EvaluateAll()

	response := map[string]interface{}{
		"evaluated_at": time.Now(),
		"total_count":  len(violations),
		"violations":   violations,
	}

	api.respondJSON(w, response)
}

// GET /health
func (api *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status": "healthy",
		"time":   time.Now(),
	}

	// Check database connection if using PostgreSQL
	if pgStore, ok := api.store.(*db.PostgresStore); ok {
		if err := pgStore.Ping(); err != nil {
			health["status"] = "unhealthy"
			health["database"] = "disconnected"
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			health["database"] = "connected"
		}
	}

	api.respondJSON(w, health)
}

// GET /ready
func (api *APIServer) handleReady(w http.ResponseWriter, r *http.Request) {
	ready := map[string]interface{}{
		"ready":           true,
		"invariants_load": len(api.engine.GetInvariants()) > 0,
	}
	api.respondJSON(w, ready)
}

// GET /api/v1/stats
func (api *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	violations := api.engine.EvaluateAll()

	stats := map[string]interface{}{
		"total_invariants": len(api.engine.GetInvariants()),
		"total_violations": 0,
		"by_severity": map[string]int{
			"critical": 0,
			"degraded": 0,
			"warning":  0,
		},
		"by_actor": make(map[string]int),
	}

	for _, v := range violations {
		if v.Violated {
			stats["total_violations"] = stats["total_violations"].(int) + 1

			// Count by severity
			severityMap := stats["by_severity"].(map[string]int)
			severityMap[string(v.Severity)]++

			// Count by actor
			actorMap := stats["by_actor"].(map[string]int)
			actorMap[v.ResponsibleActor]++
		}
	}

	api.respondJSON(w, stats)
}

func (api *APIServer) buildCausalChain(invariantID string) []map[string]interface{} {
	chain := make([]map[string]interface{}, 0)

	inv, exists := api.engine.GetInvariantByID(invariantID)
	if !exists {
		return chain
	}

	// Add current invariant
	chain = append(chain, map[string]interface{}{
		"invariant_id": inv.ID,
		"description":  inv.Description,
		"severity":     inv.Severity,
		"actor":        inv.Responsibility.Primary,
	})

	// Traverse requirements
	for _, req := range inv.Requires {
		if reqInv, exists := api.engine.GetInvariantByID(req.Invariant); exists {
			chain = append(chain, map[string]interface{}{
				"invariant_id": reqInv.ID,
				"description":  reqInv.Description,
				"severity":     reqInv.Severity,
				"actor":        reqInv.Responsibility.Primary,
				"relation":     req.Scope.Relation,
			})
		}
	}

	return chain
}

func (api *APIServer) respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (api *APIServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[%s] %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s completed in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func (api *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
