package policy

import (
	"testing"
)

func step(name, mode string, order int, static ...string) Step {
	return Step{Name: name, Mode: mode, Order: order, Approvers: Approvers{Static: static}}
}

func policyWith(id, pType string, priority int, steps ...Step) Policy {
	return Policy{ID: id, Type: pType, Priority: priority, Steps: steps}
}

// baseline merge

func TestMergeSteps_SinglePolicy(t *testing.T) {
	p := policyWith("p1", "baseline", 100, step("Manager", "ANY", 1, "mgr"))
	result := MergeSteps([]Policy{p})
	if len(result) != 1 {
		t.Fatalf("want 1 step, got %d", len(result))
	}
	if result[0].Name != "Manager" {
		t.Errorf("want step 'Manager', got %q", result[0].Name)
	}
}

func TestMergeSteps_SameNameStepsMerged(t *testing.T) {
	p1 := policyWith("p1", "baseline", 100, step("Manager", "ANY", 1, "mgr1"))
	p2 := policyWith("p2", "augment", 50, step("Manager", "ANY", 1, "mgr2"))
	result := MergeSteps([]Policy{p1, p2})
	if len(result) != 1 {
		t.Fatalf("same-name steps should merge into one, got %d", len(result))
	}
	approvers := result[0].Approvers.Static
	if len(approvers) != 2 {
		t.Errorf("merged step should have 2 approvers, got %d: %v", len(approvers), approvers)
	}
}

func TestMergeSteps_ModeEscalatestoALL(t *testing.T) {
	p1 := policyWith("p1", "baseline", 100, step("Review", "ANY", 1, "a"))
	p2 := policyWith("p2", "augment", 50, step("Review", "ALL", 1, "b"))
	result := MergeSteps([]Policy{p1, p2})
	if result[0].Mode != "ALL" {
		t.Errorf("mode should escalate to ALL, got %q", result[0].Mode)
	}
}

func TestMergeSteps_OrderPreserved(t *testing.T) {
	p := policyWith("p1", "baseline", 100,
		step("Third", "ANY", 3, "c"),
		step("First", "ANY", 1, "a"),
		step("Second", "ANY", 2, "b"),
	)
	result := MergeSteps([]Policy{p})
	if len(result) != 3 {
		t.Fatalf("want 3 steps, got %d", len(result))
	}
	if result[0].Name != "First" || result[1].Name != "Second" || result[2].Name != "Third" {
		t.Errorf("wrong order: %v", []string{result[0].Name, result[1].Name, result[2].Name})
	}
}

//override

func TestMergeSteps_OverrideWins(t *testing.T) {
	baseline := policyWith("base", "baseline", 100, step("Manager", "ANY", 1, "mgr"))
	override := policyWith("over", "override", 200, step("Security", "ALL", 1, "sec"))
	result := MergeSteps([]Policy{baseline, override})

	if len(result) != 1 {
		t.Fatalf("override should suppress baseline, got %d steps", len(result))
	}
	if result[0].Name != "Security" {
		t.Errorf("want 'Security' from override, got %q", result[0].Name)
	}
}

//restrict

func TestMergeSteps_RestrictRemovesStep(t *testing.T) {
	baseline := policyWith("base", "baseline", 100, step("Manager", "ANY", 1, "mgr"))
	restrict := policyWith("res", "restrict", 50, step("Manager", "ANY", 1, "x"))
	result := MergeSteps([]Policy{baseline, restrict})

	for _, s := range result {
		if s.Name == "Manager" {
			t.Error("restrict should have removed 'Manager' step")
		}
	}
}

//deduplication

func TestMergeSteps_ApproverDeduplication(t *testing.T) {
	p1 := policyWith("p1", "baseline", 100, step("Step1", "ANY", 1, "alice"))
	p2 := policyWith("p2", "augment", 50, step("Step2", "ANY", 2, "alice", "bob"))
	result := MergeSteps([]Policy{p1, p2})

	if len(result) != 2 {
		t.Fatalf("want 2 steps, got %d", len(result))
	}
	// alice appeared in Step1 so should not appear again in Step2
	for _, a := range result[1].Approvers.Static {
		if a == "alice" {
			t.Error("alice should be deduped from Step2 (already in Step1)")
		}
	}
}

func TestMergeSteps_EmptyApproversSkipped(t *testing.T) {
	p := policyWith("p1", "baseline", 100, Step{Name: "Empty", Mode: "ANY", Order: 1})
	result := MergeSteps([]Policy{p})
	if len(result) != 0 {
		t.Error("step with no approvers should be skipped")
	}
}
