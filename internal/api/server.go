// Package api exposes the REST API and serves the embedded web UI. All state
// mutations go through core.App (design appendix A, web-console spec).
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/wuxi/sub2proxy/internal/core"
)

// Version is set at build time; surfaced by /api/health.
var Version = "dev"

// Server holds the API dependencies.
type Server struct {
	app      *core.App
	authKey  string
	sessions *sessionStore
	limiter  *loginLimiter
	staticFS fs.FS // embedded web UI; nil serves a placeholder
}

// NewServer builds the API server. staticFS may be nil during development.
func NewServer(app *core.App, authKey string, staticFS fs.FS) *Server {
	return &Server{
		app:      app,
		authKey:  authKey,
		sessions: newSessionStore(sessionTTL),
		limiter:  newLoginLimiter(),
		staticFS: staticFS,
	}
}

// Handler builds the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated.
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/health", s.handleHealth)

	// Subscriptions.
	mux.HandleFunc("GET /api/subscriptions", s.requireAuth(s.handleListSubscriptions))
	mux.HandleFunc("POST /api/subscriptions", s.requireAuth(s.handleCreateSubscription))
	mux.HandleFunc("PUT /api/subscriptions/{id}", s.requireAuth(s.handleUpdateSubscription))
	mux.HandleFunc("DELETE /api/subscriptions/{id}", s.requireAuth(s.handleDeleteSubscription))
	mux.HandleFunc("POST /api/subscriptions/{id}/refresh", s.requireAuth(s.handleRefreshSubscription))

	// Nodes.
	mux.HandleFunc("GET /api/nodes", s.requireAuth(s.handleListNodes))
	mux.HandleFunc("POST /api/nodes", s.requireAuth(s.handleAddNode))
	mux.HandleFunc("DELETE /api/nodes/{id}", s.requireAuth(s.handleDeleteNode))
	mux.HandleFunc("POST /api/nodes/{id}/test", s.requireAuth(s.handleTestNode))
	mux.HandleFunc("POST /api/nodes/test-all", s.requireAuth(s.handleTestAllNodes))

	// Mappings.
	mux.HandleFunc("GET /api/mappings", s.requireAuth(s.handleListMappings))
	mux.HandleFunc("GET /api/mappings/port-range", s.requireAuth(s.handleMappingPortRange))
	mux.HandleFunc("POST /api/mappings", s.requireAuth(s.handleCreateMapping))
	mux.HandleFunc("PUT /api/mappings/{port}", s.requireAuth(s.handleUpdateMapping))
	mux.HandleFunc("DELETE /api/mappings/{port}", s.requireAuth(s.handleDeleteMapping))
	mux.HandleFunc("POST /api/mappings/{port}/enable", s.requireAuth(s.handleEnableMapping))
	mux.HandleFunc("POST /api/mappings/{port}/disable", s.requireAuth(s.handleDisableMapping))

	// Status.
	mux.HandleFunc("GET /api/status", s.requireAuth(s.handleStatus))

	// Static web UI + SPA fallback for everything else.
	mux.HandleFunc("/", s.serveStatic)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a {"error": msg} body with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeCoreError maps a core error to its HTTP status.
func writeCoreError(w http.ResponseWriter, err error) {
	writeError(w, core.HTTPStatus(err), err.Error())
}
