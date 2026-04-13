package dsl

import (
	"testing"

	pb "nir/proto/iam/v1"
)

func baseCtx() EvalContext {
	return EvalContext{
		Request: &pb.AccessRequest{
			Subject:  &pb.Subject{UserId: "user-1"},
			Resource: &pb.Resource{Type: "app", Name: "billing", Environment: pb.Environment_PROD, Labels: []string{"critical", "pci"}},
			Roles:    []*pb.Role{{Name: "admin"}, {Name: "viewer"}},
		},
		HR: &pb.HRResponse{
			Department: "finance",
			Groups:     []string{"risk-team", "payments"},
			ManagerId:  "mgr-42",
			HrBp:       "hrbp-001",
			Position:   "Senior Engineer",
			Status:     "active",
		},
	}
}

func TestEvaluate_SimpleCompare(t *testing.T) {
	ctx := baseCtx()

	tests := []struct {
		expr string
		want bool
	}{
		{`resource.type == "app"`, true},
		{`resource.type == "database"`, false},
		{`resource.type != "database"`, true},
		{`resource.type != "app"`, false},
		{`resource.name == "billing"`, true},
		{`environment == "PROD"`, true},
		{`environment == "DEV"`, false},
		{`subject.user_id == "user-1"`, true},
		{`hr.department == "finance"`, true},
		{`subject.department == "finance"`, true}, // alias
		{`hr.department == "Finance"`, true},       // case-insensitive
		{`hr.position == "Senior Engineer"`, true},
	}

	for _, tt := range tests {
		got, err := Evaluate(tt.expr, ctx)
		if err != nil {
			t.Errorf("Evaluate(%q) error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Evaluate(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestEvaluate_IN(t *testing.T) {
	ctx := baseCtx()

	tests := []struct {
		expr string
		want bool
	}{
		{`"risk-team" IN subject.groups`, true},
		{`"payments" IN subject.groups`, true},
		{`"unknown" IN subject.groups`, false},
		{`"admin" IN subject.roles`, true},
		{`"viewer" IN subject.roles`, true},
		{`"superadmin" IN subject.roles`, false},
		{`"critical" IN resource.labels`, true},
		{`"pci" IN resource.labels`, true},
		{`"public" IN resource.labels`, false},
	}

	for _, tt := range tests {
		got, err := Evaluate(tt.expr, ctx)
		if err != nil {
			t.Errorf("Evaluate(%q) error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Evaluate(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestEvaluate_AND(t *testing.T) {
	ctx := baseCtx()

	tests := []struct {
		expr string
		want bool
	}{
		{`resource.type == "app" AND environment == "PROD"`, true},
		{`resource.type == "app" AND environment == "DEV"`, false},
		{`resource.type == "database" AND environment == "PROD"`, false},
		{`hr.department == "finance" AND "risk-team" IN subject.groups`, true},
		{`hr.department == "hr" AND "risk-team" IN subject.groups`, false},
	}

	for _, tt := range tests {
		got, err := Evaluate(tt.expr, ctx)
		if err != nil {
			t.Errorf("Evaluate(%q) error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Evaluate(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestEvaluate_OR(t *testing.T) {
	ctx := baseCtx()

	tests := []struct {
		expr string
		want bool
	}{
		{`hr.department == "finance" OR hr.department == "hr"`, true},
		{`hr.department == "sales" OR hr.department == "hr"`, false},
		{`hr.department == "sales" OR "risk-team" IN subject.groups`, true},
	}

	for _, tt := range tests {
		got, err := Evaluate(tt.expr, ctx)
		if err != nil {
			t.Errorf("Evaluate(%q) error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Evaluate(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestEvaluate_NOT(t *testing.T) {
	ctx := baseCtx()

	tests := []struct {
		expr string
		want bool
	}{
		{`NOT hr.department == "hr"`, true},
		{`NOT hr.department == "finance"`, false},
		{`NOT "unknown" IN subject.groups`, true},
		{`NOT "risk-team" IN subject.groups`, false},
	}

	for _, tt := range tests {
		got, err := Evaluate(tt.expr, ctx)
		if err != nil {
			t.Errorf("Evaluate(%q) error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Evaluate(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestEvaluate_Complex(t *testing.T) {
	ctx := baseCtx()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{
			"AND + OR with parens",
			`resource.type == "app" AND (hr.department == "finance" OR "risk-team" IN subject.groups)`,
			true,
		},
		{
			"AND + OR parens, dept mismatch but group matches",
			`resource.type == "app" AND (hr.department == "sales" OR "risk-team" IN subject.groups)`,
			true,
		},
		{
			"AND + OR parens, both inner false",
			`resource.type == "app" AND (hr.department == "sales" OR "unknown" IN subject.groups)`,
			false,
		},
		{
			"nested NOT in parens",
			`resource.type == "app" AND NOT (hr.department == "hr")`,
			true,
		},
		{
			"triple AND",
			`resource.type == "app" AND environment == "PROD" AND hr.department == "finance"`,
			true,
		},
		{
			"OR chain",
			`hr.department == "sales" OR hr.department == "hr" OR hr.department == "finance"`,
			true,
		},
		{
			"complex real-world: prod app, finance or risk, with admin role",
			`resource.type == "app" AND environment == "PROD" AND (hr.department == "finance" OR "risk-team" IN subject.groups) AND "admin" IN subject.roles`,
			true,
		},
		{
			"complex real-world: same but wrong role",
			`resource.type == "app" AND environment == "PROD" AND (hr.department == "finance" OR "risk-team" IN subject.groups) AND "superadmin" IN subject.roles`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.expr, ctx)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluate_NilHR(t *testing.T) {
	ctx := EvalContext{
		Request: &pb.AccessRequest{
			Subject:  &pb.Subject{UserId: "user-1"},
			Resource: &pb.Resource{Type: "app", Environment: pb.Environment_PROD},
		},
		HR: nil, // HR ещё не вызван
	}

	// Поля HR резолвятся в пустую строку → не матчат
	got, err := Evaluate(`hr.department == "finance"`, ctx)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got {
		t.Error("expected false when HR is nil")
	}

	// Но resource-поля работают
	got, err = Evaluate(`resource.type == "app"`, ctx)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !got {
		t.Error("expected true for resource.type == app")
	}
}

func TestEvaluate_Errors(t *testing.T) {
	ctx := baseCtx()

	bad := []string{
		``,
		`resource.type`,
		`== "app"`,
		`resource.type == `,
		`(resource.type == "app"`,
		`resource.type == "app")`,
		`resource.type == "app" XAND environment == "PROD"`,
		`"unterminated string`,
	}

	for _, expr := range bad {
		_, err := Evaluate(expr, ctx)
		if err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}
