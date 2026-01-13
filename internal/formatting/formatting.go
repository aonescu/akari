package formatting

import (
	"fmt"
	"strings"

	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/engine"
)

func FormatMultipleExplanations(violations []*engine.ViolationResult) []string {
	explanations := make([]string, 0)
	for _, v := range violations {
		if v.Violated {
			explanations = append(explanations, FormatExplanation(v))
		}
	}
	return explanations
}

func GenerateSummary(violations []*engine.ViolationResult) map[string]interface{} {
	summary := map[string]interface{}{
		"total":       len(violations),
		"violated":    0,
		"satisfied":   0,
		"critical":    0,
		"responsible": make(map[string]int),
	}

	for _, v := range violations {
		if v.Violated {
			summary["violated"] = summary["violated"].(int) + 1
			if v.Severity == dsl.Critical {
				summary["critical"] = summary["critical"].(int) + 1
			}

			responsible := summary["responsible"].(map[string]int)
			responsible[v.ResponsibleActor]++
		} else {
			summary["satisfied"] = summary["satisfied"].(int) + 1
		}
	}

	return summary
}

func FormatExplanation(violation *engine.ViolationResult) string {
	var output strings.Builder

	output.WriteString("\nISSUE\n")
	output.WriteString("────────────────────────\n")
	output.WriteString(fmt.Sprintf("%s: %s\n", violation.InvariantID, violation.AffectedResource))
	output.WriteString(fmt.Sprintf("Severity: %s\n\n", violation.Severity))

	output.WriteString("CAUSE\n")
	output.WriteString("────────────────────────\n")
	output.WriteString(fmt.Sprintf("%s\n\n", violation.Reason))

	output.WriteString("RESPONSIBILITY\n")
	output.WriteString("────────────────────────\n")
	output.WriteString(fmt.Sprintf("%s\n\n", violation.ResponsibleActor))

	if len(violation.EliminatedActors) > 0 {
		output.WriteString("ELIMINATED\n")
		output.WriteString("────────────────────────\n")
		for _, actor := range violation.EliminatedActors {
			output.WriteString(fmt.Sprintf("✓ %s\n", actor))
		}
		output.WriteString("\n")
	}

	output.WriteString("NEXT ACTION\n")
	output.WriteString("────────────────────────\n")
	output.WriteString(fmt.Sprintf("Inspect %s and related components\n", violation.ResponsibleActor))

	return output.String()
}
