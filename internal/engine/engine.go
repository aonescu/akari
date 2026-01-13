package engine

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/aonescu/akari/internal/authority"
	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/dsl/invariants"
	"github.com/aonescu/akari/internal/state"
	"github.com/aonescu/akari/internal/types"
)

type ViolationResult struct {
	InvariantID      string       `json:"invariant_id"`
	Violated         bool         `json:"violated"`
	Reason           string       `json:"reason"`
	ResponsibleActor string       `json:"responsible_actor"`
	EliminatedActors []string     `json:"eliminated_actors"`
	AffectedResource string       `json:"affected_resource"`
	DetectedAt       time.Time    `json:"detected_at"`
	Severity         dsl.Severity `json:"severity"`
}

type InvariantEngine struct {
	mu         sync.RWMutex
	invariants map[string]dsl.Invariant
	store      state.StateStore
	evalEngine *EvaluationEngine
}

func NewInvariantEngine(store state.StateStore) *InvariantEngine {
	authorityMap := authority.NewControllerAuthorityMap()
	evalEngine := NewEvaluationEngine(store, authorityMap)

	engine := &InvariantEngine{
		invariants: evalEngine.invariants,
		store:      store,
		evalEngine: evalEngine,
	}
	return engine
}

func (e *InvariantEngine) GetInvariants() []dsl.Invariant {
	e.mu.RLock()
	defer e.mu.RUnlock()

	invariants := make([]dsl.Invariant, 0, len(e.invariants))
	for _, inv := range e.invariants {
		invariants = append(invariants, inv)
	}
	return invariants
}

// GetInvariantByID returns an invariant by its ID
func (e *InvariantEngine) GetInvariantByID(id string) (dsl.Invariant, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	inv, exists := e.invariants[id]
	return inv, exists
}

func (e *InvariantEngine) EvaluateAll() []*ViolationResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var violations []*ViolationResult
	for _, inv := range e.invariants {
		results := e.Evaluate(inv)
		violations = append(violations, results...)
	}
	return violations
}

func (e *InvariantEngine) Evaluate(inv dsl.Invariant) []*ViolationResult {
	var violations []*ViolationResult

	subjects := e.store.GetLatestByKind(inv.Subject.Kind)

	for _, subject := range subjects {
		violation := e.evaluateSubject(inv, subject)
		if violation != nil {
			violations = append(violations, violation)
		}
	}

	return violations
}

func (e *InvariantEngine) evaluateSubject(inv dsl.Invariant, subject types.StateEvent) *ViolationResult {
	ctx := types.EvaluationContext{
		Resource:      subject,
		RelatedStates: make(map[string]types.StateEvent),
		Timestamp:     time.Now(),
	}

	return e.evalEngine.EvaluateWithContext(inv, ctx)
}

func (e *InvariantEngine) evaluatePredicate(pred dsl.Predicate, subject types.StateEvent) bool {
	satisfied, _ := e.evalEngine.evaluatePredicateWithReason(pred, subject)
	return satisfied
}

func (e *InvariantEngine) eliminateActors(field string, primary string) []string {
	return e.evalEngine.eliminateActors(field, primary)
}

type EvaluationEngine struct {
	invariants    map[string]dsl.Invariant
	store         state.StateStore
	authorityMap  *authority.ControllerAuthorityMap
	evaluationLog []EvaluationLogEntry
	mu            sync.RWMutex
}

type EvaluationLogEntry struct {
	InvariantID string
	ResourceUID string
	Result      bool
	Reason      string
	Timestamp   time.Time
	Duration    time.Duration
}

func NewEvaluationEngine(store state.StateStore, authorityMap *authority.ControllerAuthorityMap) *EvaluationEngine {
	engine := &EvaluationEngine{
		invariants:    make(map[string]dsl.Invariant),
		store:         store,
		authorityMap:  authorityMap,
		evaluationLog: make([]EvaluationLogEntry, 0),
	}
	engine.LoadDefaultInvariants()
	return engine
}

func (e *EvaluationEngine) LoadDefaultInvariants() {
	invariants := invariants.GetMVPInvariants()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, inv := range invariants {
		e.invariants[inv.ID] = inv
	}
	log.Printf("Loaded %d invariants into evaluation engine", len(invariants))
}

func (e *EvaluationEngine) evaluateDependency(
	req dsl.Requirement,
	ctx types.EvaluationContext,
) *ViolationResult {

	reqInv, exists := e.invariants[req.Invariant]
	if !exists {
		return &ViolationResult{
			InvariantID: req.Invariant,
			Violated:    true,
			Reason:      "Required invariant not registered",
		}
	}

	switch req.Scope.Relation {

	case dsl.Same:
		// Evaluate on the same resource
		return e.EvaluateWithContext(reqInv, ctx)

	case dsl.Owner, dsl.Node, dsl.Selector:
		// Not yet supported by StateStore
		return &ViolationResult{
			InvariantID: reqInv.ID,
			Violated:    true,
			Reason:      "Dependency relation not supported by StateStore",
		}

	default:
		return &ViolationResult{
			InvariantID: reqInv.ID,
			Violated:    true,
			Reason:      "Unknown dependency relation",
		}
	}
}

// EvaluateWithContext performs evaluation with full context
func (e *EvaluationEngine) EvaluateWithContext(inv dsl.Invariant, ctx types.EvaluationContext) *ViolationResult {
	startTime := time.Now()

	result := &ViolationResult{
		InvariantID:      inv.ID,
		Violated:         false,
		AffectedResource: fmt.Sprintf("%s/%s", ctx.Resource.Namespace, ctx.Resource.Name),
		DetectedAt:       ctx.Timestamp,
		Severity:         inv.Severity,
	}

	// Step 1: Evaluate predicate if present
	if inv.Predicate != nil {
		satisfied, reason := e.evaluatePredicateWithReason(*inv.Predicate, ctx.Resource)
		if !satisfied {
			result.Violated = true
			result.Reason = reason

			// Step 2: Determine responsibility through authority analysis
			result.ResponsibleActor = e.determineResponsibility(inv, ctx.Resource)
			result.EliminatedActors = e.eliminateActors(inv.Predicate.Field, result.ResponsibleActor)

			// Log evaluation
			e.logEvaluation(inv.ID, ctx.Resource.UID, false, reason, time.Since(startTime))
			return result
		}
	}

	// Step 3: Evaluate dependencies (requires)
	for _, req := range inv.Requires {
		reqInv, exists := e.invariants[req.Invariant]
		if !exists {
			continue
		}

		depViolation := e.evaluateDependency(req, ctx)
		if depViolation != nil {
			result.Violated = true
			result.Reason = fmt.Sprintf(
				"Dependency %s failed: %s",
				reqInv.ID,
				depViolation.Reason,
			)
			result.ResponsibleActor = depViolation.ResponsibleActor
			result.EliminatedActors = depViolation.EliminatedActors

			e.logEvaluation(inv.ID, ctx.Resource.UID, false, result.Reason, time.Since(startTime))
			return result
		}
	}

	// All checks passed
	e.logEvaluation(inv.ID, ctx.Resource.UID, true, "satisfied", time.Since(startTime))
	return nil
}

func (e *EvaluationEngine) evaluatePredicateWithReason(pred dsl.Predicate, subject types.StateEvent) (bool, string) {
	value, exists := subject.FieldDiff[pred.Field]

	switch pred.Operator {
	case dsl.Exists:
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist", pred.Field)
		}
		return true, ""

	case dsl.NotExists:
		if exists {
			return false, fmt.Sprintf("Field %s exists but should not (value: %v)", pred.Field, value)
		}
		return true, ""

	case dsl.Equals:
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist (expected: %v)", pred.Field, pred.Value)
		}
		if value != pred.Value {
			return false, fmt.Sprintf("Field %s is '%v' (expected: %v)", pred.Field, value, pred.Value)
		}
		return true, ""

	case dsl.NotEquals:
		if !exists {
			return true, ""
		}
		if value == pred.Value {
			return false, fmt.Sprintf("Field %s is '%v' (must not equal: %v)", pred.Field, value, pred.Value)
		}
		return true, ""

	case dsl.GreaterThan:
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist", pred.Field)
		}

		numValue, ok := toNumber(value)
		if !ok {
			return false, fmt.Sprintf("Field %s is not numeric: %v", pred.Field, value)
		}

		expectedNum, ok := toNumber(pred.Value)
		if !ok {
			return false, "Comparison value is not numeric"
		}

		if numValue <= expectedNum {
			return false, fmt.Sprintf("Field %s is %v (must be > %v)", pred.Field, numValue, expectedNum)
		}
		return true, ""

	case dsl.LessThan:
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist", pred.Field)
		}

		numValue, ok := toNumber(value)
		if !ok {
			return false, fmt.Sprintf("Field %s is not numeric: %v", pred.Field, value)
		}

		expectedNum, ok := toNumber(pred.Value)
		if !ok {
			return false, "Comparison value is not numeric"
		}

		if numValue >= expectedNum {
			return false, fmt.Sprintf("Field %s is %v (must be < %v)", pred.Field, numValue, expectedNum)
		}
		return true, ""

	case dsl.AnyTrue:
		// For array fields: check if any element is truthy
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist", pred.Field)
		}

		// Handle array values
		if arr, ok := value.([]interface{}); ok {
			for _, item := range arr {
				if isTruthy(item) {
					return true, ""
				}
			}
			return false, fmt.Sprintf("Field %s has no truthy elements", pred.Field)
		}

		// Handle single value
		if isTruthy(value) {
			return true, ""
		}
		return false, fmt.Sprintf("Field %s is not true", pred.Field)

	case dsl.AllTrue:
		// For array fields: check if all elements are truthy
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist", pred.Field)
		}

		// Handle array values
		if arr, ok := value.([]interface{}); ok {
			if len(arr) == 0 {
				return false, fmt.Sprintf("Field %s is empty array", pred.Field)
			}
			for _, item := range arr {
				if !isTruthy(item) {
					return false, fmt.Sprintf("Field %s has non-truthy element: %v", pred.Field, item)
				}
			}
			return true, ""
		}

		// Handle single value
		if isTruthy(value) {
			return true, ""
		}
		return false, fmt.Sprintf("Field %s is not true", pred.Field)

	case dsl.Contains:
		// Check if array contains a value
		if !exists {
			return false, fmt.Sprintf("Field %s does not exist", pred.Field)
		}

		if arr, ok := value.([]interface{}); ok {
			for _, item := range arr {
				if item == pred.Value {
					return true, ""
				}
			}
			return false, fmt.Sprintf("Field %s does not contain %v", pred.Field, pred.Value)
		}

		return false, fmt.Sprintf("Field %s is not an array", pred.Field)

	default:
		return false, fmt.Sprintf("Unknown operator: %s", pred.Operator)
	}
}

func isTruthy(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != "" && v != "False" && v != "false"
	case int, int32, int64:
		return v != 0
	case float32, float64:
		return v != 0.0
	case nil:
		return false
	default:
		return true // Non-nil objects are truthy
	}
}

func (e *EvaluationEngine) determineResponsibility(inv dsl.Invariant, resource types.StateEvent) string {
	// Use authority map to determine which controller is responsible
	if inv.Predicate != nil {
		authorizedControllers := e.authorityMap.GetAuthorizedControllers(inv.Predicate.Field)
		if len(authorizedControllers) == 1 {
			return authorizedControllers[0]
		}

		// If multiple controllers have authority, use primary from invariant
		for _, controller := range authorizedControllers {
			if controller == inv.Responsibility.Primary {
				return controller
			}
		}
	}

	return inv.Responsibility.Primary
}

func (e *EvaluationEngine) eliminateActors(field string, primary string) []string {
	authorized := e.authorityMap.GetAuthorizedControllers(field)

	var eliminated []string
	allControllers := e.authorityMap.GetAllControllers()

	for _, controller := range allControllers {
		if controller != primary && !contains(authorized, controller) {
			eliminated = append(eliminated, controller)
		}
	}

	return eliminated
}

func (e *EvaluationEngine) logEvaluation(invID, resourceUID string, result bool, reason string, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry := EvaluationLogEntry{
		InvariantID: invID,
		ResourceUID: resourceUID,
		Result:      result,
		Reason:      reason,
		Timestamp:   time.Now(),
		Duration:    duration,
	}

	e.evaluationLog = append(e.evaluationLog, entry)

	// Keep only last 1000 entries
	if len(e.evaluationLog) > 1000 {
		e.evaluationLog = e.evaluationLog[len(e.evaluationLog)-1000:]
	}
}

func (e *EvaluationEngine) GetEvaluationStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	totalEvaluations := len(e.evaluationLog)
	violations := 0
	var totalDuration time.Duration

	for _, entry := range e.evaluationLog {
		if !entry.Result {
			violations++
		}
		totalDuration += entry.Duration
	}

	avgDuration := time.Duration(0)
	if totalEvaluations > 0 {
		avgDuration = totalDuration / time.Duration(totalEvaluations)
	}

	return map[string]interface{}{
		"total_evaluations": totalEvaluations,
		"violations_found":  violations,
		"avg_duration_ms":   avgDuration.Milliseconds(),
		"total_invariants":  len(e.invariants),
	}
}

func toNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Suppress some noisy logs
	if os.Getenv("DEBUG") == "" {
		log.SetOutput(os.Stdout)
	}
}
