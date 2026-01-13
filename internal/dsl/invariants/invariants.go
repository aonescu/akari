package invariants

import "github.com/aonescu/akari/internal/dsl"

// GetMVPInvariants returns the minimum viable set of invariants for Kubernetes resources
func GetMVPInvariants() []dsl.Invariant {
	return []dsl.Invariant{
		{
			ID:          "pod_exists",
			Version:     1,
			Description: "Pod should not be deleted",
			Subject:     dsl.Subject{Kind: "Pod"},
			Predicate: &dsl.Predicate{
				Field:    "metadata.deletionTimestamp",
				Operator: dsl.NotExists,
			},
			Responsibility: dsl.Responsibility{
				Primary: "garbage-collector",
				Team:    "platform",
			},
			Severity: dsl.Critical,
		},
		{
			ID:          "pod_scheduled",
			Version:     1,
			Description: "Pod should be assigned to a node",
			Subject:     dsl.Subject{Kind: "Pod"},
			Predicate: &dsl.Predicate{
				Field:    "spec.nodeName",
				Operator: dsl.Exists,
			},
			Responsibility: dsl.Responsibility{
				Primary: "kube-scheduler",
				Team:    "platform",
			},
			Severity: dsl.Critical,
		},
		{
			ID:          "node_ready",
			Version:     1,
			Description: "Node should be in Ready state",
			Subject:     dsl.Subject{Kind: "Node"},
			Predicate: &dsl.Predicate{
				Field:    "status.conditions[Ready].status",
				Operator: dsl.Equals,
				Value:    "True",
			},
			Responsibility: dsl.Responsibility{
				Primary:   "node-controller",
				Secondary: "kubelet",
				Team:      "infrastructure",
			},
			Severity: dsl.Critical,
		},
		{
			ID:          "containers_running",
			Version:     1,
			Description: "All containers in pod should be running",
			Subject:     dsl.Subject{Kind: "Pod"},
			Predicate: &dsl.Predicate{
				Field:    "status.containerStatuses[*].state.running",
				Operator: dsl.AllTrue,
			},
			Blocks: []string{"pod_ready"},
			Responsibility: dsl.Responsibility{
				Primary: "kubelet",
				Team:    "platform-node",
			},
			Severity: dsl.Critical,
		},
		{
			ID:          "pod_ready",
			Version:     1,
			Description: "Pod should be in Ready state",
			Subject:     dsl.Subject{Kind: "Pod"},
			Predicate: &dsl.Predicate{
				Field:    "status.conditions[Ready].status",
				Operator: dsl.Equals,
				Value:    "True",
			},
			Requires: []dsl.Requirement{
				{
					Invariant: "containers_running",
					Scope:     dsl.Scope{Relation: dsl.Same},
				},
				{
					Invariant: "node_ready",
					Scope:     dsl.Scope{Relation: dsl.Node},
				},
			},
			Blocks: []string{"service_has_endpoints"},
			Responsibility: dsl.Responsibility{
				Primary: "kubelet",
				Team:    "platform-node",
			},
			Severity: dsl.Critical,
		},
		{
			ID:          "service_has_endpoints",
			Version:     1,
			Description: "Service should have ready endpoints",
			Subject:     dsl.Subject{Kind: "Service"},
			Predicate: &dsl.Predicate{
				Field:    "endpoints[*].addresses",
				Operator: dsl.AnyTrue,
			},
			Requires: []dsl.Requirement{
				{
					Invariant: "pod_ready",
					Scope:     dsl.Scope{Relation: dsl.Selector},
				},
			},
			Responsibility: dsl.Responsibility{
				Primary:   "kubelet",
				Secondary: "service-controller",
				Team:      "platform",
			},
			Severity: dsl.Critical,
		},
		// Add more invariants as needed - this is a minimal set
	}
}
