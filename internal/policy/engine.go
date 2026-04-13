package policy

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"nir/pkg/dsl"
	pb "nir/proto/iam/v1"
)

// ==========================
// MODELS
// ==========================

type Policy struct {
	ID               string            `json:"policy_id"`
	Type             string            `json:"type"`                        // baseline | augment | override | restrict
	Priority         int               `json:"priority"`
	Selectors        Selectors         `json:"selectors"`
	Steps            []Step            `json:"steps"`                       // безусловные шаги
	ConditionalSteps []ConditionalStep `json:"conditional_steps,omitempty"` // шаги с условием
}

type Selectors struct {
	ResourceType interface{} `json:"resource_type"`
	ResourceName interface{} `json:"resource_name,omitempty"`
	ResourceID   interface{} `json:"resource_id,omitempty"`
	Environment  interface{} `json:"environment,omitempty"`
	Labels       []string    `json:"labels,omitempty"`
	Roles        []string    `json:"roles,omitempty"`
	Department   string      `json:"department,omitempty"`
	Groups       []string    `json:"groups,omitempty"`
}

type Step struct {
	Name      string    `json:"name"`
	Approvers Approvers `json:"approvers"`
	Mode      string    `json:"mode"`
	Order     int       `json:"order"`
}

type Approvers struct {
	Dynamic []DynamicApprover `json:"dynamic,omitempty"`
	Static  []string          `json:"static,omitempty"`
}

type DynamicApprover struct {
	Role string `json:"role"`
}

// ConditionalStep — шаг, который добавляется только при выполнении DSL-условия.
type ConditionalStep struct {
	If    string `json:"if"`    // DSL-выражение
	Steps []Step `json:"steps"` // шаги, добавляемые при if == true
}

// ==========================
// ENGINE
// ==========================

type Engine struct {
	policies []Policy
}

func NewEngine(policies []Policy) (*Engine, error) {
	if len(policies) == 0 {
		return nil, fmt.Errorf("no policies provided")
	}

	for i := range policies {
		if policies[i].ID == "" {
			return nil, fmt.Errorf("policy at index %d has empty policy_id", i)
		}
		if policies[i].Type == "" {
			policies[i].Type = "baseline"
		}
		// Валидация DSL в conditional_steps при загрузке
		for j, cs := range policies[i].ConditionalSteps {
			if cs.If == "" {
				return nil, fmt.Errorf("policy %s: conditional_steps[%d] has empty 'if'", policies[i].ID, j)
			}
			if err := dsl.Validate(cs.If); err != nil {
				return nil, fmt.Errorf("policy %s: conditional_steps[%d] invalid: %w", policies[i].ID, j, err)
			}
		}
	}

	sort.SliceStable(policies, func(i, j int) bool {
		return policies[i].Priority > policies[j].Priority
	})

	return &Engine{policies: policies}, nil
}

func (e *Engine) Policies() []Policy {
	result := make([]Policy, len(e.policies))
	copy(result, e.policies)
	return result
}

// ==========================
// ENGINE HOLDER
// ==========================

type EngineHolder struct {
	mu     sync.RWMutex
	engine *Engine
}

func NewEngineHolder(e *Engine) *EngineHolder {
	return &EngineHolder{engine: e}
}

func (h *EngineHolder) Get() *Engine {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.engine
}

func (h *EngineHolder) Set(e *Engine) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.engine = e
}

// ==========================
// PIPELINE
// ==========================

type PipelineTrace struct {
	TotalPolicies    int
	AfterSelectors   []string
	RejectedSelector []string
	AfterHR          []string
	RejectedHR       []string
	TriggeredConds   []string // какие conditional_steps сработали
}

func policyIDs(policies []Policy) []string {
	ids := make([]string, len(policies))
	for i, p := range policies {
		ids[i] = p.ID
	}
	return ids
}

func diff(before, after []string) []string {
	set := make(map[string]bool)
	for _, id := range after {
		set[id] = true
	}
	var rejected []string
	for _, id := range before {
		if !set[id] {
			rejected = append(rejected, id)
		}
	}
	return rejected
}

// EvaluatePipeline выполняет фильтрацию политик (selectors → HR).
func (e *Engine) EvaluatePipeline(req *pb.AccessRequest, hr *pb.HRResponse) ([]Policy, PipelineTrace) {
	trace := PipelineTrace{TotalPolicies: len(e.policies)}

	labels := []string{}
	if req.Resource != nil {
		labels = req.Resource.Labels
	}

	// Stage 1: map selectors
	stage1 := e.selectPoliciesPreHR(req, labels)
	trace.AfterSelectors = policyIDs(stage1)
	trace.RejectedSelector = diff(policyIDs(e.policies), trace.AfterSelectors)

	// Stage 2: HR
	stage2 := e.filterPoliciesWithHR(stage1, hr)
	trace.AfterHR = policyIDs(stage2)
	trace.RejectedHR = diff(trace.AfterSelectors, trace.AfterHR)

	return stage2, trace
}

// CollectSteps собирает все шаги из сматченных политик:
// безусловные (steps) + условные (conditional_steps с выполненным if).
// Возвращает плоский список шагов и список сработавших условий для trace.
func CollectSteps(policies []Policy, req *pb.AccessRequest, hr *pb.HRResponse) ([]Step, []string) {
	var allSteps []Step
	var triggered []string

	for _, p := range policies {
		allSteps = append(allSteps, p.Steps...)

		condSteps, condTriggered := CollectConditionalSteps(p, req, hr)
		allSteps = append(allSteps, condSteps...)
		triggered = append(triggered, condTriggered...)
	}

	return allSteps, triggered
}

// CollectConditionalSteps вычисляет conditional_steps одной политики.
// Возвращает шаги из сработавших условий и описания для trace.
func CollectConditionalSteps(p Policy, req *pb.AccessRequest, hr *pb.HRResponse) ([]Step, []string) {
	if len(p.ConditionalSteps) == 0 {
		return nil, nil
	}

	var steps []Step
	var triggered []string

	ctx := dsl.EvalContext{Request: req, HR: hr}

	for _, cs := range p.ConditionalSteps {
		ok, err := dsl.Evaluate(cs.If, ctx)
		if err != nil {
			log.Printf("  ⚠️  condition error [%s]: %v", p.ID, err)
			continue
		}
		if ok {
			triggered = append(triggered, fmt.Sprintf("%s → %s", p.ID, cs.If))
			steps = append(steps, cs.Steps...)
		}
	}

	return steps, triggered
}

// --- internal ---

func (e *Engine) selectPoliciesPreHR(req *pb.AccessRequest, labels []string) []Policy {
	var selected []Policy
	for _, p := range e.policies {
		if matchesSelectorsPreHR(p, req, labels) {
			selected = append(selected, p)
		}
	}
	return selected
}

func (e *Engine) filterPoliciesWithHR(policies []Policy, hr *pb.HRResponse) []Policy {
	var result []Policy
	for _, p := range policies {
		if p.Selectors.Department != "" && !strings.EqualFold(p.Selectors.Department, hr.Department) {
			continue
		}
		if len(p.Selectors.Groups) > 0 && !intersect(p.Selectors.Groups, hr.Groups) {
			continue
		}
		result = append(result, p)
	}
	return result
}

// ==========================
// EXPLAIN
// ==========================

type PolicyExplainResult struct {
	PolicyID         string
	Type             string
	Priority         int
	Matched          bool
	Reasons          []string
	ConditionalSteps []ConditionalStepExplain
}

type ConditionalStepExplain struct {
	If        string
	Triggered bool
	Steps     []string // имена шагов
}

func (e *Engine) Explain(req *pb.AccessRequest, hr *pb.HRResponse) []PolicyExplainResult {
	var results []PolicyExplainResult
	dslCtx := dsl.EvalContext{Request: req, HR: hr}

	for _, p := range e.policies {
		var reasons []string
		matched := true

		check := func(name string, ok bool) {
			if ok {
				reasons = append(reasons, name+" matched")
			} else {
				matched = false
				reasons = append(reasons, name+" NOT matched")
			}
		}

		check("resource_type", matchInterface(p.Selectors.ResourceType, req.Resource.Type))

		if p.Selectors.ResourceName != nil {
			check("resource_name", matchInterface(p.Selectors.ResourceName, req.Resource.Name))
		}
		if p.Selectors.ResourceID != nil {
			check("resource_id", matchInterface(p.Selectors.ResourceID, req.Resource.Id))
		}

		check("environment", matchInterface(p.Selectors.Environment, req.Resource.Environment.String()))

		if len(p.Selectors.Roles) > 0 {
			check("roles", matchRoles(p.Selectors.Roles, req.Roles))
		}
		if p.Selectors.Department != "" {
			check("department", strings.EqualFold(p.Selectors.Department, hr.Department))
		}
		if len(p.Selectors.Groups) > 0 {
			check("groups", intersect(p.Selectors.Groups, hr.Groups))
		}

		// Explain conditional_steps
		var csExplains []ConditionalStepExplain
		for _, cs := range p.ConditionalSteps {
			ok, _ := dsl.Evaluate(cs.If, dslCtx)
			var stepNames []string
			for _, s := range cs.Steps {
				stepNames = append(stepNames, s.Name)
			}
			csExplains = append(csExplains, ConditionalStepExplain{
				If:        cs.If,
				Triggered: ok,
				Steps:     stepNames,
			})
		}

		results = append(results, PolicyExplainResult{
			PolicyID:         p.ID,
			Type:             p.Type,
			Priority:         p.Priority,
			Matched:          matched,
			Reasons:          reasons,
			ConditionalSteps: csExplains,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		typeOrder := map[string]int{"override": 0, "baseline": 1, "augment": 1, "restrict": 2}
		if typeOrder[results[i].Type] != typeOrder[results[j].Type] {
			return typeOrder[results[i].Type] < typeOrder[results[j].Type]
		}
		return results[i].Priority > results[j].Priority
	})

	return results
}

// ==========================
// MATCHING HELPERS
// ==========================

func matchesSelectorsPreHR(p Policy, req *pb.AccessRequest, labels []string) bool {
	if req == nil || req.Resource == nil {
		return false
	}
	if !matchInterface(p.Selectors.ResourceType, req.Resource.Type) {
		return false
	}
	if p.Selectors.ResourceName != nil {
		if !matchInterface(p.Selectors.ResourceName, req.Resource.Name) {
			return false
		}
	}
	if p.Selectors.ResourceID != nil {
		if !matchInterface(p.Selectors.ResourceID, req.Resource.Id) {
			return false
		}
	}
	if !matchInterface(p.Selectors.Environment, req.Resource.Environment.String()) {
		return false
	}
	if len(p.Selectors.Labels) > 0 {
		for _, required := range p.Selectors.Labels {
			if !contains(labels, required) {
				return false
			}
		}
	}
	if len(p.Selectors.Roles) > 0 {
		if !matchRoles(p.Selectors.Roles, req.Roles) {
			return false
		}
	}
	return true
}

func matchInterface(selector interface{}, value string) bool {
	if selector == nil {
		return true
	}
	switch v := selector.(type) {
	case string:
		return matchWildcard(v, value)
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok && matchWildcard(str, value) {
				return true
			}
		}
	}
	return false
}

func matchWildcard(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	p := strings.ToUpper(pattern)
	v := strings.ToUpper(value)
	if strings.HasSuffix(p, "/*") {
		return strings.HasPrefix(v, strings.TrimSuffix(p, "/*"))
	}
	return p == v
}

func matchRoles(policyRoles []string, reqRoles []*pb.Role) bool {
	for _, pr := range policyRoles {
		for _, rr := range reqRoles {
			if strings.EqualFold(pr, rr.Name) {
				return true
			}
		}
	}
	return false
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func intersect(a, b []string) bool {
	set := make(map[string]bool)
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if set[y] {
			return true
		}
	}
	return false
}
