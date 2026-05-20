package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nir/internal/storage"
	"nir/pkg/dsl"
	pb "nir/proto/iam/v1"

	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	store *storage.PostgresStore
	grpc  pb.IAMClient
}

// Helpers

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Auth

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	user, hash, err := h.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		writeError(w, 401, "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, 401, "invalid credentials")
		return
	}

	token, err := generateToken()
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}

	expiresAt := time.Now().Add(sessionTTL)
	if err := h.store.CreateSession(r.Context(), token, user.ID, expiresAt); err != nil {
		writeError(w, 500, "could not create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, user)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = h.store.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, MaxAge: -1, Path: "/"})
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, currentUser(r))
}

// Policies CRUD

func (h *Handler) listPolicies(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListAllPolicies(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if rows == nil {
		rows = []storage.PolicyRow{}
	}
	writeJSON(w, rows)
}

func (h *Handler) createPolicy(w http.ResponseWriter, r *http.Request) {
	var row storage.PolicyRow
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if row.PolicyID == "" {
		writeError(w, 400, "policy_id required")
		return
	}

	u := currentUser(r)
	if u == nil {
		writeError(w, 401, "not authenticated")
		return
	}
	if u.Role == "admin" {
		row.Status = "active"
	} else {
		row.Status = "pending_review"
		row.SubmittedBy = u.Username
	}

	if err := h.store.CreatePolicy(r.Context(), row); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if row.Status == "active" {
		h.store.NotifyPolicyChange(r.Context())
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusAccepted)
	}
}

func (h *Handler) updatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var row storage.PolicyRow
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	row.PolicyID = id
	if err := h.store.UpdatePolicy(r.Context(), row); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, 404, err.Error())
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) deletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeletePolicy(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, 404, err.Error())
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) togglePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := h.store.TogglePolicy(r.Context(), id, body.Enabled); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	h.store.NotifyPolicyChange(r.Context())
	w.WriteHeader(http.StatusOK)
}

// Stats

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListAllPolicies(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type Stats struct {
		Total    int            `json:"total"`
		Active   int            `json:"active"`
		Inactive int            `json:"inactive"`
		ByType   map[string]int `json:"by_type"`
	}
	s := Stats{Total: len(rows), ByType: make(map[string]int)}
	for _, row := range rows {
		if row.Enabled {
			s.Active++
		} else {
			s.Inactive++
		}
		s.ByType[row.Type]++
	}
	writeJSON(w, s)
}

// Review queue

func (h *Handler) listPending(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListPendingPolicies(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if rows == nil {
		rows = []storage.PolicyRow{}
	}
	writeJSON(w, rows)
}

func (h *Handler) approvePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u := currentUser(r)
	if err := h.store.ApprovePolicy(r.Context(), id, u.Username); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, 404, err.Error())
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	h.store.NotifyPolicyChange(r.Context())
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) rejectPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u := currentUser(r)
	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.store.RejectPolicy(r.Context(), id, u.Username, body.Comment); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, 404, err.Error())
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

// User management

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if users == nil {
		users = []storage.UserRow{}
	}
	writeJSON(w, users)
}

func (h *Handler) createUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, 400, "username and password required")
		return
	}
	validRoles := map[string]bool{"admin": true, "editor": true, "reviewer": true, "viewer": true}
	if !validRoles[req.Role] {
		writeError(w, 400, "invalid role")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, 500, "could not hash password")
		return
	}

	user, err := h.store.CreateUser(r.Context(), req.Username, string(hash), req.Role)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeError(w, 409, "username already exists")
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, user)
}

func (h *Handler) deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}
	me := currentUser(r)
	if me.ID == id {
		writeError(w, 400, "cannot delete yourself")
		return
	}
	if err := h.store.DeleteUser(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, 404, err.Error())
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeError(w, 400, "password required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, 500, "could not hash password")
		return
	}
	if err := h.store.UpdateUserPassword(r.Context(), id, string(hash)); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Test / Explain

type WorkflowRequest struct {
	UserID       string            `json:"user_id"`
	ResourceType string            `json:"resource_type"`
	ResourceName string            `json:"resource_name"`
	ResourceID   string            `json:"resource_id"`
	Environment  string            `json:"environment"`
	Labels       []string          `json:"labels"`
	Roles        []string          `json:"roles"`
	Attributes   map[string]string `json:"attributes"`
}

func toPBRequest(req WorkflowRequest) *pb.AccessRequest {
	env := pb.Environment_ENV_UNSPECIFIED
	switch strings.ToUpper(req.Environment) {
	case "PROD":
		env = pb.Environment_PROD
	case "STAGE":
		env = pb.Environment_STAGE
	case "DEV":
		env = pb.Environment_DEV
	}
	var roles []*pb.Role
	for _, r := range req.Roles {
		roles = append(roles, &pb.Role{Name: r})
	}
	return &pb.AccessRequest{
		Subject: &pb.Subject{UserId: req.UserID},
		Resource: &pb.Resource{
			Type:        req.ResourceType,
			Name:        req.ResourceName,
			Id:          req.ResourceID,
			Environment: env,
			Labels:      req.Labels,
			Attributes:  req.Attributes,
		},
		Roles: roles,
	}
}

type TestStep struct {
	Name      string   `json:"name"`
	Approvers []string `json:"approvers"`
	Mode      string   `json:"mode"`
}

type TestResult struct {
	Steps []TestStep `json:"steps"`
	Total int        `json:"total"`
}

func (h *Handler) testRequest(w http.ResponseWriter, r *http.Request) {
	var req WorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pbResp, err := h.grpc.CreateAccessRequest(ctx, toPBRequest(req))
	if err != nil {
		writeError(w, 502, "gRPC: "+err.Error())
		return
	}

	result := TestResult{Steps: []TestStep{}}
	for _, step := range pbResp.Steps {
		result.Steps = append(result.Steps, TestStep{
			Name:      step.Name,
			Approvers: step.ApproverIds,
			Mode:      step.Mode,
		})
	}
	result.Total = len(result.Steps)
	writeJSON(w, result)
}

type ExplainCondStep struct {
	If        string   `json:"if"`
	Triggered bool     `json:"triggered"`
	Steps     []string `json:"steps"`
}

type ExplainPolicy struct {
	PolicyID         string            `json:"policy_id"`
	Type             string            `json:"type"`
	Priority         int32             `json:"priority"`
	Matched          bool              `json:"matched"`
	Reasons          []string          `json:"reasons"`
	ConditionalSteps []ExplainCondStep `json:"conditional_steps,omitempty"`
}

type ExplainResult struct {
	Policies []ExplainPolicy `json:"policies"`
}

func (h *Handler) explainRequest(w http.ResponseWriter, r *http.Request) {
	var req WorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pbResp, err := h.grpc.ExplainAccessRequest(ctx, toPBRequest(req))
	if err != nil {
		writeError(w, 502, "gRPC: "+err.Error())
		return
	}

	result := ExplainResult{Policies: []ExplainPolicy{}}
	for _, p := range pbResp.Policies {
		ep := ExplainPolicy{
			PolicyID: p.PolicyId,
			Type:     p.Type,
			Priority: p.Priority,
			Matched:  p.Matched,
			Reasons:  p.Reasons,
		}
		for _, cs := range p.ConditionalSteps {
			ep.ConditionalSteps = append(ep.ConditionalSteps, ExplainCondStep{
				If:        cs.IfExpr,
				Triggered: cs.Triggered,
				Steps:     cs.StepNames,
			})
		}
		result.Policies = append(result.Policies, ep)
	}
	writeJSON(w, result)
}

// DSL

type DSLValidateRequest struct {
	Expression string `json:"expression"`
}

func (h *Handler) dslValidate(w http.ResponseWriter, r *http.Request) {
	var req DSLValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := dsl.Validate(req.Expression); err != nil {
		writeJSON(w, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"valid": true})
}

type DSLEvalRequest struct {
	Expression string     `json:"expression"`
	Context    DSLContext `json:"context"`
}

type DSLContext struct {
	ResourceType        string   `json:"resource_type"`
	ResourceEnvironment string   `json:"resource_environment"`
	HRDepartment        string   `json:"hr_department"`
	HRGroups            []string `json:"hr_groups"`
	Roles               []string `json:"roles"`
	Labels              []string `json:"labels"`
}

func (h *Handler) dslEvaluate(w http.ResponseWriter, r *http.Request) {
	var req DSLEvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	env := pb.Environment_ENV_UNSPECIFIED
	switch strings.ToUpper(req.Context.ResourceEnvironment) {
	case "PROD":
		env = pb.Environment_PROD
	case "STAGE":
		env = pb.Environment_STAGE
	case "DEV":
		env = pb.Environment_DEV
	}

	var roles []*pb.Role
	for _, role := range req.Context.Roles {
		roles = append(roles, &pb.Role{Name: role})
	}

	pbReq := &pb.AccessRequest{
		Resource: &pb.Resource{
			Type:        req.Context.ResourceType,
			Environment: env,
			Labels:      req.Context.Labels,
		},
		Roles: roles,
	}
	hr := &pb.HRResponse{
		Department: req.Context.HRDepartment,
		Groups:     req.Context.HRGroups,
	}

	result, err := dsl.Evaluate(req.Expression, dsl.EvalContext{Request: pbReq, HR: hr})
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"result": result})
}
