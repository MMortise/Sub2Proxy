package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/wuxi/sub2proxy/internal/core"
)

// --- subscriptions ---

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Subscriptions())
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var in core.SubscriptionInput
	if !decode(w, r, &in) {
		return
	}
	sub, count, err := s.app.AddSubscription(in)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscription": sub, "node_count": count})
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	var in core.SubscriptionInput
	if !decode(w, r, &in) {
		return
	}
	sub, err := s.app.UpdateSubscription(r.PathValue("id"), in)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteSubscription(r.PathValue("id")); err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRefreshSubscription(w http.ResponseWriter, r *http.Request) {
	count, err := s.app.RefreshSubscription(r.PathValue("id"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_count": count})
}

// --- nodes ---

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Nodes(r.URL.Query().Get("q")))
}

func (s *Server) handleAddNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Link string `json:"link"`
	}
	if !decode(w, r, &body) {
		return
	}
	node, err := s.app.AddManualNode(body.Link)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteNode(r.PathValue("id")); err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestNode(w http.ResponseWriter, r *http.Request) {
	res, err := s.app.TestNode(r.PathValue("id"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTestAllNodes(w http.ResponseWriter, r *http.Request) {
	// Optional ?source=<subscription id|manual> scopes the sweep.
	s.app.TestAllNodes(r.URL.Query().Get("source"))
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}

// --- mappings ---

func (s *Server) handleListMappings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Mappings())
}

// handleMappingPortRange reports the mapping-port bounds so the UI can show how
// close it is to the ceiling. Static per process — clients fetch it once.
func (s *Server) handleMappingPortRange(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.PortRange())
}

func (s *Server) handleCreateMapping(w http.ResponseWriter, r *http.Request) {
	var in core.MappingInput
	if !decode(w, r, &in) {
		return
	}
	m, err := s.app.CreateMapping(in)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleUpdateMapping(w http.ResponseWriter, r *http.Request) {
	port, ok := pathPort(w, r)
	if !ok {
		return
	}
	var in core.MappingInput
	if !decode(w, r, &in) {
		return
	}
	m, err := s.app.UpdateMapping(port, in)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMapping(w http.ResponseWriter, r *http.Request) {
	port, ok := pathPort(w, r)
	if !ok {
		return
	}
	if err := s.app.DeleteMapping(port); err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleEnableMapping(w http.ResponseWriter, r *http.Request) {
	s.setMappingEnabled(w, r, true)
}

func (s *Server) handleDisableMapping(w http.ResponseWriter, r *http.Request) {
	s.setMappingEnabled(w, r, false)
}

func (s *Server) setMappingEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	port, ok := pathPort(w, r)
	if !ok {
		return
	}
	if err := s.app.SetMappingEnabled(port, enabled); err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- status ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Status())
}

// --- helpers ---

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

func pathPort(w http.ResponseWriter, r *http.Request) (int, bool) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "port must be an integer")
		return 0, false
	}
	return port, true
}
