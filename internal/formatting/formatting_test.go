package formatting

import (
	"strings"
	"testing"
	"time"

	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/engine"
)

func TestFormatExplanation(t *testing.T) {
	violation := &engine.ViolationResult{
		InvariantID:      "pod_ready",
		Violated:         true,
		Reason:           "Pod is not in Ready state",
		ResponsibleActor: "kubelet",
		EliminatedActors: []string{"kube-scheduler", "deployment-controller"},
		AffectedResource: "default/test-pod",
		DetectedAt:       time.Now(),
		Severity:         dsl.Critical,
	}

	explanation := FormatExplanation(violation)

	if explanation == "" {
		t.Fatal("Expected non-empty explanation")
	}

	// Check that key sections are present
	if !strings.Contains(explanation, "ISSUE") {
		t.Error("Expected 'ISSUE' section in explanation")
	}

	if !strings.Contains(explanation, "CAUSE") {
		t.Error("Expected 'CAUSE' section in explanation")
	}

	if !strings.Contains(explanation, "RESPONSIBILITY") {
		t.Error("Expected 'RESPONSIBILITY' section in explanation")
	}

	if !strings.Contains(explanation, "ELIMINATED") {
		t.Error("Expected 'ELIMINATED' section in explanation")
	}

	if !strings.Contains(explanation, "NEXT ACTION") {
		t.Error("Expected 'NEXT ACTION' section in explanation")
	}

	// Check that violation details are included
	if !strings.Contains(explanation, "pod_ready") {
		t.Error("Expected invariant ID in explanation")
	}

	if !strings.Contains(explanation, "kubelet") {
		t.Error("Expected responsible actor in explanation")
	}

	if !strings.Contains(explanation, "Pod is not in Ready state") {
		t.Error("Expected reason in explanation")
	}
}

func TestFormatMultipleExplanations(t *testing.T) {
	violations := []*engine.ViolationResult{
		{
			InvariantID:      "pod_ready",
			Violated:         true,
			Reason:           "Pod is not ready",
			ResponsibleActor: "kubelet",
			AffectedResource: "default/pod-1",
			Severity:         dsl.Critical,
		},
		{
			InvariantID:      "pod_scheduled",
			Violated:         true,
			Reason:           "Pod is not scheduled",
			ResponsibleActor: "kube-scheduler",
			AffectedResource: "default/pod-1",
			Severity:         dsl.Critical,
		},
		{
			InvariantID:      "node_ready",
			Violated:         false,
			Reason:           "",
			ResponsibleActor: "",
			AffectedResource: "",
			Severity:         dsl.Critical,
		},
	}

	explanations := FormatMultipleExplanations(violations)

	// Should only include violated invariants
	if len(explanations) != 2 {
		t.Errorf("Expected 2 explanations, got %d", len(explanations))
	}

	// Check that both violations are included
	foundPodReady := false
	foundPodScheduled := false
	for _, exp := range explanations {
		if strings.Contains(exp, "pod_ready") {
			foundPodReady = true
		}
		if strings.Contains(exp, "pod_scheduled") {
			foundPodScheduled = true
		}
	}

	if !foundPodReady {
		t.Error("Expected pod_ready explanation")
	}
	if !foundPodScheduled {
		t.Error("Expected pod_scheduled explanation")
	}
}

func TestGenerateSummary(t *testing.T) {
	violations := []*engine.ViolationResult{
		{
			InvariantID:      "pod_ready",
			Violated:         true,
			Reason:           "Pod is not ready",
			ResponsibleActor: "kubelet",
			Severity:         dsl.Critical,
		},
		{
			InvariantID:      "pod_scheduled",
			Violated:         true,
			Reason:           "Pod is not scheduled",
			ResponsibleActor: "kube-scheduler",
			Severity:         dsl.Critical,
		},
		{
			InvariantID:      "node_ready",
			Violated:         false,
			Reason:           "",
			ResponsibleActor: "",
			Severity:         dsl.Critical,
		},
		{
			InvariantID:      "deployment_available",
			Violated:         true,
			Reason:           "Deployment has no available replicas",
			ResponsibleActor: "deployment-controller",
			Severity:         dsl.Degraded,
		},
	}

	summary := GenerateSummary(violations)

	if summary["total"].(int) != 4 {
		t.Errorf("Expected total 4, got %d", summary["total"])
	}

	if summary["violated"].(int) != 3 {
		t.Errorf("Expected 3 violated, got %d", summary["violated"])
	}

	if summary["satisfied"].(int) != 1 {
		t.Errorf("Expected 1 satisfied, got %d", summary["satisfied"])
	}

	if summary["critical"].(int) != 2 {
		t.Errorf("Expected 2 critical, got %d", summary["critical"])
	}

	responsible := summary["responsible"].(map[string]int)
	if responsible["kubelet"] != 1 {
		t.Errorf("Expected kubelet count 1, got %d", responsible["kubelet"])
	}
	if responsible["kube-scheduler"] != 1 {
		t.Errorf("Expected kube-scheduler count 1, got %d", responsible["kube-scheduler"])
	}
	if responsible["deployment-controller"] != 1 {
		t.Errorf("Expected deployment-controller count 1, got %d", responsible["deployment-controller"])
	}
}
