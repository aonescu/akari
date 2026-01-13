# Akari – Kubernetes Invariant Engine

Akari is a lightweight engine to monitor, evaluate, and enforce invariants in a Kubernetes cluster. It allows you to define rules about your cluster state (Pods, Nodes, Services, etc.) and automatically detect violations, identify responsible controllers, and analyze cascading effects.

Key Features
 • Declarative DSL: Define invariants with predicates, scope, and responsibilities.
 • Real-time evaluation: Automatically processes cluster events and detects violations.
 • Actor responsibility tracking: Determines which controller or actor is responsible for violations.
 • Supports dependency chains: Evaluates invariants that depend on other invariants.
 • In-memory or Postgres storage: Flexible backend for storing cluster state.
 • Testable: Includes comprehensive scenarios to simulate common Kubernetes failure cases.

Example Invariants
 • Pods must not be in CrashLoopBackOff.
 • Nodes must be ready and not under memory pressure.
 • Services must have ready endpoints.

Getting Started

 1. Run an in-memory store for testing:

store := NewMemoryStore()
engine := NewInvariantEngine(store)

 1. Load cluster events and evaluate invariants:

violations := engine.EvaluateAll()

 1. Check violations and responsible actors:

for _, v := range violations {
    if v.Violated {
        fmt.Println(v.InvariantID, v.ResponsibleActor, v.Reason)
    }
}
