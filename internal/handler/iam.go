package handler

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"nir/internal/policy"
	pb "nir/proto/iam/v1"
)

type IAMHandler struct {
	pb.UnimplementedIAMServer
	policyEngine *policy.EngineHolder
	debug        bool
}

func NewIAMHandler(pe *policy.EngineHolder, debug bool) *IAMHandler {
	return &IAMHandler{policyEngine: pe, debug: debug}
}

func mockHR(userID string) *pb.HRResponse {
	return &pb.HRResponse{
		FullName:   "Test User",
		Department: "finance",
		ManagerId:  "manager-" + userID,
		HrBp:       "hrbp-001",
		Groups:     []string{"risk-team"},
		Position:   "Senior Engineer",
		Status:     "active",
	}
}

func (h *IAMHandler) logTrace(userID string, trace policy.PipelineTrace, steps []*pb.WorkflowStep) {
	log.Printf("━━━ Policy evaluation for user=%s ━━━", userID)
	log.Printf("  Total policies: %d", trace.TotalPolicies)
	log.Printf("  Stage 1 (selectors): %d matched, %d rejected",
		len(trace.AfterSelectors), len(trace.RejectedSelector))
	log.Printf("  Stage 2 (HR):        %d matched, %d rejected",
		len(trace.AfterHR), len(trace.RejectedHR))

	if h.debug {
		if len(trace.RejectedSelector) > 0 {
			log.Printf("    rejected (selectors): %s", strings.Join(trace.RejectedSelector, ", "))
		}
		if len(trace.RejectedHR) > 0 {
			log.Printf("    rejected (HR):        %s", strings.Join(trace.RejectedHR, ", "))
		}
	}

	log.Printf("  Final policies: [%s]", strings.Join(trace.AfterHR, ", "))

	if len(trace.TriggeredConds) > 0 {
		log.Printf("  Triggered conditions: %d", len(trace.TriggeredConds))
		for _, tc := range trace.TriggeredConds {
			log.Printf("    ⚡ %s", tc)
		}
	}

	log.Printf("━━━ Workflow: %d steps ━━━", len(steps))
	for i, step := range steps {
		log.Printf("  Step %d: %s | mode=%s | approvers=[%s]",
			i+1, step.Name, step.Mode, strings.Join(step.ApproverIds, ", "))
	}
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// ==========================
// CREATE ACCESS REQUEST
// ==========================

func (h *IAMHandler) CreateAccessRequest(ctx context.Context, req *pb.AccessRequest) (*pb.WorkflowResponse, error) {
	engine := h.policyEngine.Get()
	hr := mockHR(req.Subject.UserId)

	// Pipeline: selectors → HR
	policies, trace := engine.EvaluatePipeline(req, hr)

	if len(policies) == 0 {
		log.Printf("━━━ No matching policies for user=%s ━━━", req.Subject.UserId)
		return nil, fmt.Errorf("no matching policies found")
	}

	// Собираем шаги: steps + conditional_steps (DSL evaluation)
	collectedSteps, triggered := policy.CollectSteps(policies, req, hr)
	trace.TriggeredConds = triggered

	// Формируем «виртуальные» политики для merge (шаги уже собраны)
	// Создаём одну Policy на каждую исходную, но с расширенным Steps
	expanded := expandPolicies(policies, req, hr)

	mergedSteps := policy.MergeSteps(expanded)

	var steps []*pb.WorkflowStep
	for _, step := range mergedSteps {
		approvers := resolveApprovers(step.Approvers, hr)
		mode := step.Mode
		if len(approvers) == 1 {
			mode = "ANY"
		}
		steps = append(steps, &pb.WorkflowStep{
			Name:        step.Name,
			ApproverIds: approvers,
			Mode:        mode,
		})
	}

	// Используем collectedSteps для trace count
	_ = collectedSteps

	h.logTrace(req.Subject.UserId, trace, steps)

	return &pb.WorkflowResponse{
		Steps:      steps,
		TotalSteps: int32(len(steps)),
	}, nil
}

// expandPolicies создаёт копии политик, где Steps включает
// как безусловные шаги, так и шаги из сработавших conditional_steps.
func expandPolicies(policies []policy.Policy, req *pb.AccessRequest, hr *pb.HRResponse) []policy.Policy {
	expanded := make([]policy.Policy, len(policies))

	for i, p := range policies {
		expanded[i] = policy.Policy{
			ID:       p.ID,
			Type:     p.Type,
			Priority: p.Priority,
			Steps:    make([]policy.Step, len(p.Steps)),
		}
		copy(expanded[i].Steps, p.Steps)

		// Evaluate conditional_steps и добавить сработавшие
		condSteps, _ := policy.CollectConditionalSteps(p, req, hr)
		expanded[i].Steps = append(expanded[i].Steps, condSteps...)
	}

	return expanded
}

// ==========================
// EXPLAIN
// ==========================

func (h *IAMHandler) ExplainAccessRequest(ctx context.Context, req *pb.AccessRequest) (*pb.ExplainResponse, error) {
	engine := h.policyEngine.Get()
	hr := mockHR(req.Subject.UserId)

	results := engine.Explain(req, hr)

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Type == "override" && results[j].Type != "override" {
			return true
		}
		if results[i].Type != "override" && results[j].Type == "override" {
			return false
		}
		return results[i].Priority > results[j].Priority
	})

	var resp []*pb.PolicyExplain
	for _, r := range results {
		resp = append(resp, &pb.PolicyExplain{
			PolicyId: r.PolicyID,
			Type:     r.Type,
			Priority: int32(r.Priority),
			Matched:  r.Matched,
			Reasons:  r.Reasons,
		})
	}

	return &pb.ExplainResponse{Policies: resp}, nil
}

// ==========================
// IS APPROVER
// ==========================

func (h *IAMHandler) IsApprover(ctx context.Context, req *pb.IsApproverRequest) (*pb.IsApproverResponse, error) {
	engine := h.policyEngine.Get()
	hr := mockHR(req.Request.Subject.UserId)

	policies, _ := engine.EvaluatePipeline(req.Request, hr)
	expanded := expandPolicies(policies, req.Request, hr)
	steps := policy.MergeSteps(expanded)

	if int(req.StepIndex) >= len(steps) {
		return &pb.IsApproverResponse{IsApprover: false}, nil
	}

	approvers := resolveApprovers(steps[req.StepIndex].Approvers, hr)
	for _, a := range approvers {
		if a == req.ApproverId {
			return &pb.IsApproverResponse{IsApprover: true}, nil
		}
	}

	return &pb.IsApproverResponse{IsApprover: false}, nil
}

// ==========================
// APPROVER RESOLVER
// ==========================

func resolveApprovers(approvers policy.Approvers, hr *pb.HRResponse) []string {
	var result []string
	result = append(result, approvers.Static...)
	for _, dyn := range approvers.Dynamic {
		if id := resolveDynamic(dyn.Role, hr); id != "" {
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		result = append(result, "default-approver")
	}
	return result
}

func resolveDynamic(role string, hr *pb.HRResponse) string {
	switch role {
	case "manager":
		return hr.ManagerId
	case "hr_bp":
		return hr.HrBp
	case "department_head":
		return "head-" + hr.Department
	case "group_lead":
		if len(hr.Groups) > 0 {
			return "lead-" + hr.Groups[0]
		}
	}
	return ""
}
