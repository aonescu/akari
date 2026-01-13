package dsl

type Operator string

const (
	Equals      Operator = "equals"
	NotEquals   Operator = "not_equals"
	Exists      Operator = "exists"
	NotExists   Operator = "not_exists"
	GreaterThan Operator = "gt"
	LessThan    Operator = "lt"
	Contains    Operator = "contains"
	AnyTrue     Operator = "any_true"
	AllTrue     Operator = "all_true"
)

type Relation string

const (
	Same     Relation = "same"
	Owner    Relation = "owner"
	Selector Relation = "selector"
	Node     Relation = "node"
)

type Severity string

const (
	Critical Severity = "critical"
	Degraded Severity = "degraded"
	Warning  Severity = "warning"
)

type Predicate struct {
	Field    string      `json:"field"`
	Operator Operator    `json:"operator"`
	Value    interface{} `json:"value,omitempty"`
}

type Scope struct {
	Relation Relation `json:"relation"`
}

type Requirement struct {
	Invariant string `json:"invariant"`
	Scope     Scope  `json:"scope"`
}

type Subject struct {
	Kind      string            `json:"kind"`
	Namespace string            `json:"namespace,omitempty"`
	Selector  map[string]string `json:"selector,omitempty"`
}

type Responsibility struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary,omitempty"`
	Team      string `json:"team,omitempty"`
}

type Invariant struct {
	ID             string         `json:"id"`
	Version        int            `json:"version"`
	Description    string         `json:"description"`
	Subject        Subject        `json:"subject"`
	Predicate      *Predicate     `json:"predicate,omitempty"`
	Requires       []Requirement  `json:"requires,omitempty"`
	Blocks         []string       `json:"blocks,omitempty"`
	Responsibility Responsibility `json:"responsibility"`
	Severity       Severity       `json:"severity"`
}
