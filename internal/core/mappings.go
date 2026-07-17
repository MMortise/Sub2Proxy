package core

import (
	"fmt"

	"github.com/wuxi/sub2proxy/internal/engine"
	"github.com/wuxi/sub2proxy/internal/mapping"
	"github.com/wuxi/sub2proxy/internal/model"
)

// MappingInput is the API's mapping payload. Enabled is a pointer so it can
// default to true when omitted on create.
type MappingInput struct {
	Port        int                `json:"port"`
	Name        string             `json:"name"`
	Strategy    model.Strategy     `json:"strategy"`
	Nodes       []model.NodeRef    `json:"nodes"`
	NodeFilter  string             `json:"node_filter"`
	HealthCheck *model.HealthCheck `json:"health_check"`
	Enabled     *bool              `json:"enabled"`
	Username    string             `json:"username"`
	Password    string             `json:"password"`
}

// Mappings returns all mappings with runtime status: resolved active node and
// any auto-disable reason from node disappearance (port-mapping spec).
func (a *App) Mappings() []model.Mapping {
	status := a.engine.Status()
	active := make(map[int]string, len(status.Mappings))
	for _, ms := range status.Mappings {
		active[ms.Port] = ms.ActiveNode
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	nodes := a.pool.Nodes()
	out := make([]model.Mapping, 0, len(a.cfg.Mappings))
	for i := range a.cfg.Mappings {
		m := a.cfg.Mappings[i] // copy
		res := mapping.Resolve(&m, nodes)
		if m.Enabled && res.AutoDisable {
			m.DisabledReason = res.Reason
		}
		m.ActiveNode = active[m.Port]
		out = append(out, m)
	}
	return out
}

// CreateMapping validates and stores a new mapping, allocating a port when the
// input omits one. It returns the created mapping.
func (a *App) CreateMapping(in MappingInput) (model.Mapping, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	m, err := a.buildMapping(in, 0)
	if err != nil {
		return model.Mapping{}, err
	}
	next := append(append([]model.Mapping(nil), a.cfg.Mappings...), m)
	if err := a.validateTrial(next); err != nil {
		return model.Mapping{}, err
	}
	a.cfg.Mappings = next
	a.persistAndReload()
	return m, nil
}

// UpdateMapping replaces the mapping at oldPort with a full update.
func (a *App) UpdateMapping(oldPort int, in MappingInput) (model.Mapping, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	idx := a.mappingIndex(oldPort)
	if idx < 0 {
		return model.Mapping{}, notFound("mapping not found")
	}
	m, err := a.buildMapping(in, oldPort)
	if err != nil {
		return model.Mapping{}, err
	}
	next := append([]model.Mapping(nil), a.cfg.Mappings...)
	next[idx] = m
	if err := a.validateTrial(next); err != nil {
		return model.Mapping{}, err
	}
	a.cfg.Mappings = next
	a.persistAndReload()
	return m, nil
}

// DeleteMapping removes the mapping on port and releases it.
func (a *App) DeleteMapping(port int) error {
	a.mu.Lock()
	idx := a.mappingIndex(port)
	if idx < 0 {
		a.mu.Unlock()
		return notFound("mapping not found")
	}
	a.cfg.Mappings = append(a.cfg.Mappings[:idx], a.cfg.Mappings[idx+1:]...)
	a.mu.Unlock()
	a.persistAndReload()
	return nil
}

// SetMappingEnabled toggles a mapping's configured enabled flag.
func (a *App) SetMappingEnabled(port int, enabled bool) error {
	a.mu.Lock()
	idx := a.mappingIndex(port)
	if idx < 0 {
		a.mu.Unlock()
		return notFound("mapping not found")
	}
	a.cfg.Mappings[idx].Enabled = enabled
	a.mu.Unlock()
	a.persistAndReload()
	return nil
}

// buildMapping constructs a model.Mapping from input, allocating a port when
// in.Port == 0. Caller holds a.mu. excludePort is the mapping's own port on
// update (so it isn't counted as a conflict with itself).
func (a *App) buildMapping(in MappingInput, excludePort int) (model.Mapping, error) {
	if !in.Strategy.Valid() {
		return model.Mapping{}, badRequest("invalid strategy")
	}
	port := in.Port
	if port == 0 {
		p, err := mapping.AllocatePort(a.cfg.PortLo(), a.cfg.PortHi(), a.usedPorts(excludePort))
		if err != nil {
			return model.Mapping{}, conflict(err.Error())
		}
		port = p
	} else if port != excludePort {
		if owner := a.portOwner(port); owner != "" {
			return model.Mapping{}, conflict(fmt.Sprintf("port %d is already used by %q", port, owner))
		}
	}

	m := model.Mapping{
		Port: port, Name: in.Name, Strategy: in.Strategy,
		Nodes: in.Nodes, NodeFilter: in.NodeFilter, Enabled: true,
		Username: in.Username, Password: in.Password,
	}
	if in.Enabled != nil {
		m.Enabled = *in.Enabled
	}
	if in.Strategy != model.StrategySingle {
		if in.HealthCheck != nil {
			m.HealthCheck = in.HealthCheck
		} else {
			m.HealthCheck = model.DefaultHealthCheck()
		}
	}
	return m, nil
}

// validateTrial runs the full config validation over a candidate mapping set,
// so all cross-field rules are reused. Caller holds a.mu.
func (a *App) validateTrial(next []model.Mapping) error {
	trial := *a.cfg
	trial.Mappings = next
	if err := trial.Validate(); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

func (a *App) mappingIndex(port int) int {
	for i := range a.cfg.Mappings {
		if a.cfg.Mappings[i].Port == port {
			return i
		}
	}
	return -1
}

func (a *App) portOwner(port int) string {
	for _, m := range a.cfg.Mappings {
		if m.Port == port {
			if m.Name != "" {
				return m.Name
			}
			return fmt.Sprintf("mapping on %d", port)
		}
	}
	return ""
}

// Status returns the engine runtime status.
func (a *App) Status() engine.Status { return a.engine.Status() }
