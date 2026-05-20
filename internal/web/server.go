package web

import (
	"embed"
	"net/http"

	"nir/internal/storage"
	pb "nir/proto/iam/v1"
)

//go:embed static
var staticFiles embed.FS

type Server struct {
	mux *http.ServeMux
}

func NewServer(store *storage.PostgresStore, grpcClient pb.IAMClient) *Server {
	h := &Handler{store: store, grpc: grpcClient}
	mux := http.NewServeMux()

	// Public (no auth)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)

	// Middleware helpers
	auth := h.authRequired

	onlyAdmin := func(fn http.HandlerFunc) http.Handler {
		return auth(requireRole("admin")(fn))
	}
	adminOrReviewer := func(fn http.HandlerFunc) http.Handler {
		return auth(requireRole("admin", "reviewer")(fn))
	}
	notViewer := func(fn http.HandlerFunc) http.Handler {
		return auth(requireRole("admin", "editor", "reviewer")(fn))
	}

	// Current user
	mux.Handle("GET /api/me", auth(http.HandlerFunc(h.getMe)))

	// Policies
	mux.Handle("GET /api/policies", auth(http.HandlerFunc(h.listPolicies)))
	mux.Handle("POST /api/policies", notViewer(h.createPolicy))
	mux.Handle("PUT /api/policies/{id}", onlyAdmin(h.updatePolicy))
	mux.Handle("DELETE /api/policies/{id}", onlyAdmin(h.deletePolicy))
	mux.Handle("PATCH /api/policies/{id}/toggle", onlyAdmin(h.togglePolicy))
	mux.Handle("GET /api/stats", auth(http.HandlerFunc(h.stats)))

	// Review queue
	mux.Handle("GET /api/pending", adminOrReviewer(h.listPending))
	mux.Handle("POST /api/pending/{id}/approve", adminOrReviewer(h.approvePolicy))
	mux.Handle("POST /api/pending/{id}/reject", adminOrReviewer(h.rejectPolicy))

	// User management (admin only)
	mux.Handle("GET /api/users", onlyAdmin(h.listUsers))
	mux.Handle("POST /api/users", onlyAdmin(h.createUserHandler))
	mux.Handle("DELETE /api/users/{id}", onlyAdmin(h.deleteUserHandler))
	mux.Handle("PATCH /api/users/{id}/password", onlyAdmin(h.changePasswordHandler))

	// Test / Explain / DSL
	mux.Handle("POST /api/test", auth(http.HandlerFunc(h.testRequest)))
	mux.Handle("POST /api/explain", auth(http.HandlerFunc(h.explainRequest)))
	mux.Handle("POST /api/dsl/validate", auth(http.HandlerFunc(h.dslValidate)))
	mux.Handle("POST /api/dsl/evaluate", auth(http.HandlerFunc(h.dslEvaluate)))

	// Static (SPA) — serve index.html for all unmatched paths
	indexHTML, _ := staticFiles.ReadFile("static/index.html")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	return &Server{mux: mux}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
