package types

import "time"

// StateEvent represents a state change event for a Kubernetes resource
type StateEvent struct {
	UID       string                 `json:"uid"`
	Kind      string                 `json:"kind"`
	Namespace string                 `json:"namespace"`
	Name      string                 `json:"name"`
	Version   string                 `json:"version"`
	Timestamp time.Time              `json:"timestamp"`
	FieldDiff map[string]interface{} `json:"field_diff"`
	Actor     string                 `json:"actor"`
	FullState interface{}            `json:"full_state,omitempty"`
}

// EvaluationContext provides context for invariant evaluation
type EvaluationContext struct {
	Resource      StateEvent
	RelatedStates map[string]StateEvent // For dependency lookups
	Timestamp     time.Time
}
