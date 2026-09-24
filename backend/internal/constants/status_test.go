package constants

import "testing"

func TestAnimalCaseTransitionGraph(t *testing.T) {
	if !CanTransition(AnimalCaseTransitions, "registered", "sampling") {
		t.Fatalf("expected registered -> sampling transition to be allowed")
	}
	if CanTransition(AnimalCaseTransitions, "registered", "unknown") {
		t.Fatal("unknown status must never be accepted")
	}
}

func TestRiskGuards(t *testing.T) {
	for _, level := range []string{"low", "medium"} {
		if IsHighRiskSignoff(level) {
			t.Fatalf("%s must not be treated as high risk", level)
		}
	}
	for _, level := range []string{"high", "critical"} {
		if !IsHighRiskSignoff(level) {
			t.Fatalf("%s must be treated as high risk", level)
		}
	}
	if !RiskAtLeast("critical", "high") || RiskAtLeast("medium", "high") || RiskAtLeast("unknown", "low") {
		t.Fatal("risk ranking must compare assay risk against signoff risk")
	}
}
