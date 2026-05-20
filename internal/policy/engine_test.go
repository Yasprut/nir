package policy

import (
	"testing"

	pb "nir/proto/iam/v1"
)

// ─── helpers ───────────────────────────────────────────────────────────────

func req(resType, env string, roles ...string) *pb.AccessRequest {
	var pbRoles []*pb.Role
	for _, r := range roles {
		pbRoles = append(pbRoles, &pb.Role{Name: r})
	}
	envVal := pb.Environment_ENV_UNSPECIFIED
	switch env {
	case "PROD":
		envVal = pb.Environment_PROD
	case "STAGE":
		envVal = pb.Environment_STAGE
	case "DEV":
		envVal = pb.Environment_DEV
	}
	return &pb.AccessRequest{
		Subject:  &pb.Subject{UserId: "u1"},
		Resource: &pb.Resource{Type: resType, Environment: envVal},
		Roles:    pbRoles,
	}
}

func reqWithLabels(resType, env string, labels []string, roles ...string) *pb.AccessRequest {
	r := req(resType, env, roles...)
	r.Resource.Labels = labels
	return r
}

func noHR() *pb.HRResponse {
	return &pb.HRResponse{}
}

func hrOf(dept string, groups ...string) *pb.HRResponse {
	return &pb.HRResponse{Department: dept, Groups: groups}
}

func basePolicy(id, resType, env string) Policy {
	return Policy{
		ID:       id,
		Type:     "baseline",
		Priority: 100,
		Selectors: Selectors{
			ResourceType: resType,
			Environment:  env,
		},
		Steps: []Step{
			{Name: "Manager Approval", Approvers: Approvers{Static: []string{"mgr"}}, Mode: "ANY", Order: 1},
		},
	}
}

// ─── NewEngine ──────────────────────────────────────────────────────────────

func TestNewEngine_EmptyPolicies(t *testing.T) {
	_, err := NewEngine(nil)
	if err == nil {
		t.Fatal("expected error for empty policies")
	}
}

func TestNewEngine_MissingID(t *testing.T) {
	_, err := NewEngine([]Policy{{Type: "baseline"}})
	if err == nil {
		t.Fatal("expected error for policy with empty id")
	}
}

func TestNewEngine_SortsByPriority(t *testing.T) {
	policies := []Policy{
		{ID: "low", Type: "baseline", Priority: 10, Selectors: Selectors{ResourceType: "*"}},
		{ID: "high", Type: "baseline", Priority: 200, Selectors: Selectors{ResourceType: "*"}},
	}
	e, err := NewEngine(policies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := e.Policies()
	if got[0].ID != "high" {
		t.Errorf("expected first policy 'high', got %q", got[0].ID)
	}
}

// ─── selector matching ──────────────────────────────────────────────────────

func TestEvaluatePipeline_ResourceTypeMatch(t *testing.T) {
	p := basePolicy("p1", "database", "PROD")
	e, _ := NewEngine([]Policy{p})

	matched, _ := e.EvaluatePipeline(req("database", "PROD"), noHR())
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}

	noMatch, _ := e.EvaluatePipeline(req("app", "PROD"), noHR())
	if len(noMatch) != 0 {
		t.Fatalf("expected 0 matches for wrong type, got %d", len(noMatch))
	}
}

func TestEvaluatePipeline_Wildcard(t *testing.T) {
	p := basePolicy("p1", "*", "*")
	e, _ := NewEngine([]Policy{p})

	matched, _ := e.EvaluatePipeline(req("anything", "PROD"), noHR())
	if len(matched) != 1 {
		t.Fatalf("expected wildcard to match, got %d", len(matched))
	}
}

func TestEvaluatePipeline_WildcardPrefix(t *testing.T) {
	p := basePolicy("p1", "db/*", "PROD")
	e, _ := NewEngine([]Policy{p})

	if m, _ := e.EvaluatePipeline(req("db/postgres", "PROD"), noHR()); len(m) != 1 {
		t.Error("prefix wildcard should match db/postgres")
	}
	if m, _ := e.EvaluatePipeline(req("app", "PROD"), noHR()); len(m) != 0 {
		t.Error("prefix wildcard should not match app")
	}
}

func TestEvaluatePipeline_LabelFilter(t *testing.T) {
	p := basePolicy("p1", "database", "PROD")
	p.Selectors.Labels = []string{"critical"}
	e, _ := NewEngine([]Policy{p})

	if m, _ := e.EvaluatePipeline(reqWithLabels("database", "PROD", []string{"critical"}), noHR()); len(m) != 1 {
		t.Error("should match with required label present")
	}
	if m, _ := e.EvaluatePipeline(reqWithLabels("database", "PROD", []string{"other"}), noHR()); len(m) != 0 {
		t.Error("should not match when required label is missing")
	}
}

func TestEvaluatePipeline_RoleFilter(t *testing.T) {
	p := basePolicy("p1", "database", "PROD")
	p.Selectors.Roles = []string{"admin"}
	e, _ := NewEngine([]Policy{p})

	if m, _ := e.EvaluatePipeline(req("database", "PROD", "admin"), noHR()); len(m) != 1 {
		t.Error("should match with required role")
	}
	if m, _ := e.EvaluatePipeline(req("database", "PROD", "viewer"), noHR()); len(m) != 0 {
		t.Error("should not match without required role")
	}
}

func TestEvaluatePipeline_HRDepartmentFilter(t *testing.T) {
	p := basePolicy("p1", "*", "*")
	p.Selectors.Department = "finance"
	e, _ := NewEngine([]Policy{p})

	if m, _ := e.EvaluatePipeline(req("db", "PROD"), hrOf("finance")); len(m) != 1 {
		t.Error("should match finance department")
	}
	if m, _ := e.EvaluatePipeline(req("db", "PROD"), hrOf("engineering")); len(m) != 0 {
		t.Error("should not match wrong department")
	}
}

func TestEvaluatePipeline_HRGroupFilter(t *testing.T) {
	p := basePolicy("p1", "*", "*")
	p.Selectors.Groups = []string{"risk-team"}
	e, _ := NewEngine([]Policy{p})

	if m, _ := e.EvaluatePipeline(req("db", "PROD"), hrOf("finance", "risk-team")); len(m) != 1 {
		t.Error("should match with group present")
	}
	if m, _ := e.EvaluatePipeline(req("db", "PROD"), hrOf("finance", "other")); len(m) != 0 {
		t.Error("should not match without group")
	}
}

func TestEvaluatePipeline_MultipleSelectors(t *testing.T) {
	p1 := basePolicy("p1", "database", "PROD")
	p2 := basePolicy("p2", "app", "STAGE")
	e, _ := NewEngine([]Policy{p1, p2})

	if m, _ := e.EvaluatePipeline(req("database", "PROD"), noHR()); len(m) != 1 || m[0].ID != "p1" {
		t.Error("only p1 should match database/PROD")
	}
}

// ─── PipelineTrace ──────────────────────────────────────────────────────────

func TestEvaluatePipeline_Trace(t *testing.T) {
	p1 := basePolicy("match", "database", "PROD")
	p2 := basePolicy("no-match", "app", "PROD")
	e, _ := NewEngine([]Policy{p1, p2})

	_, trace := e.EvaluatePipeline(req("database", "PROD"), noHR())

	if trace.TotalPolicies != 2 {
		t.Errorf("TotalPolicies want 2, got %d", trace.TotalPolicies)
	}
	if len(trace.AfterSelectors) != 1 {
		t.Errorf("AfterSelectors want 1, got %d", len(trace.AfterSelectors))
	}
	if len(trace.RejectedSelector) != 1 || trace.RejectedSelector[0] != "no-match" {
		t.Errorf("RejectedSelector want [no-match], got %v", trace.RejectedSelector)
	}
}
