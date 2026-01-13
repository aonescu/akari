package server

import (
	"log"
	"net/http"

	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/state"
)

type APIServer struct {
	store  state.StateStore
	engine *engine.InvariantEngine
	mux    *http.ServeMux
}

func NewAPIServer(store state.StateStore, eng *engine.InvariantEngine) *APIServer {
	api := &APIServer{
		store:  store,
		engine: eng,
		mux:    http.NewServeMux(),
	}
	api.registerRoutes()
	return api
}

func (api *APIServer) registerRoutes() {
	// Violations endpoints
	api.mux.HandleFunc("/api/v1/violations", api.handleViolations)
	api.mux.HandleFunc("/api/v1/violations/active", api.handleActiveViolations)

	// Explanation endpoints
	api.mux.HandleFunc("/api/v1/explain", api.handleExplain)
	api.mux.HandleFunc("/api/v1/explain/resource", api.handleExplainResource)

	// Causality graph endpoints
	api.mux.HandleFunc("/api/v1/causal-chain", api.handleCausalChain)

	// Resource history
	api.mux.HandleFunc("/api/v1/history", api.handleHistory)

	// Invariants
	api.mux.HandleFunc("/api/v1/invariants", api.handleInvariants)
	api.mux.HandleFunc("/api/v1/invariants/evaluate", api.handleEvaluateInvariants)

	// Health check
	api.mux.HandleFunc("/health", api.handleHealth)
	api.mux.HandleFunc("/ready", api.handleReady)

	// Metrics/stats
	api.mux.HandleFunc("/api/v1/stats", api.handleStats)
}

func (api *APIServer) Start(addr string) error {
	log.Printf("Starting API server on %s", addr)

	// Add CORS middleware
	handler := api.corsMiddleware(api.loggingMiddleware(api.mux))

	return http.ListenAndServe(addr, handler)
}
