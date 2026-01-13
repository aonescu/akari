package main

import (
	"fmt"
	"log"
	"os"

	"github.com/aonescu/akari/cmd/server"
	"github.com/aonescu/akari/internal/db"
	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/state"
)

func main() {
	fmt.Println("Causality Engine - Kubernetes Watcher + REST API")

	// Get configuration from environment
	dbConnStr := os.Getenv("DATABASE_URL")
	if dbConnStr == "" {
		dbConnStr = "postgres://postgres:postgres@localhost:5432/causality?sslmode=disable"
		log.Println("DATABASE_URL not set, using default:", dbConnStr)
	}

	apiAddr := os.Getenv("API_ADDRESS")
	if apiAddr == "" {
		apiAddr = ":8080"
	}

	// Initialize storage
	var store state.StateStore
	pgStore, err := db.NewPostgresStore(dbConnStr)
	if err != nil {
		log.Printf("Failed to connect to PostgreSQL: %v", err)
		log.Println("Falling back to in-memory storage...")
		store = state.NewMemoryStore()
	} else {
		log.Println("Connected to PostgreSQL")
		store = pgStore
		defer pgStore.Close()
	}

	// Initialize engine
	eng := engine.NewInvariantEngine(store)

	// Start API server
	apiServer := server.NewAPIServer(store, eng)
	go func() {
		log.Printf("API server listening on %s", apiAddr)
		if err := apiServer.Start(apiAddr); err != nil {
			log.Fatalf("API server failed: %v", err)
		}
	}()

	// TODO: Create Kubernetes watcher
	// watcher, err := watcher.NewKubernetesWatcher(store, eng)
	// if err != nil {
	// 	log.Printf("Failed to create Kubernetes watcher: %v", err)
	// 	log.Println("API server running in demo mode without live cluster data...")
	// 	log.Println("\nAPI Endpoints:")
	// 	printAPIEndpoints(apiAddr)
	// 	select {} // Keep API server running
	// }

	// Start watching
	// ctx := context.Background()
	// watcher.Start(ctx)

	log.Println("\n✓ API server ready")
	log.Println("✓ REST API ready for queries")
	log.Println("\nAPI Endpoints:")
	printAPIEndpoints(apiAddr)
	log.Println("\nPress Ctrl+C to exit")

	// Keep running
	select {}
}

func printAPIEndpoints(addr string) {
	baseURL := "http://localhost" + addr
	endpoints := []string{
		"GET  " + baseURL + "/health",
		"GET  " + baseURL + "/ready",
		"GET  " + baseURL + "/api/v1/violations",
		"GET  " + baseURL + "/api/v1/violations/active",
		"POST " + baseURL + "/api/v1/explain",
		"GET  " + baseURL + "/api/v1/explain/resource?kind=Pod&namespace=default&name=pod-name",
		"GET  " + baseURL + "/api/v1/causal-chain?invariant_id=pod_ready",
		"GET  " + baseURL + "/api/v1/history?uid=pod-123",
		"GET  " + baseURL + "/api/v1/invariants",
		"POST " + baseURL + "/api/v1/invariants/evaluate",
		"GET  " + baseURL + "/api/v1/stats",
	}

	for _, endpoint := range endpoints {
		log.Println("  ", endpoint)
	}
}
