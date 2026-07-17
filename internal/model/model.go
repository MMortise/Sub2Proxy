// Package model holds the pure domain types shared across the app. It must not
// import other internal packages so that config, pool, mapping and engine can all
// depend on it without import cycles.
package model

// Strategy is a mapping's outbound selection policy. Each maps to a mihomo
// proxy-group type (see engine); single needs no group.
type Strategy string

const (
	StrategySingle     Strategy = "single"
	StrategyFailover   Strategy = "failover"
	StrategyRoundRobin Strategy = "round-robin"
	StrategyHash       Strategy = "hash"
	StrategySticky     Strategy = "sticky"
	StrategyAuto       Strategy = "auto"
)

// ValidStrategies is the closed set accepted by validation and the API.
var ValidStrategies = []Strategy{
	StrategySingle, StrategyFailover, StrategyRoundRobin,
	StrategyHash, StrategySticky, StrategyAuto,
}

func (s Strategy) Valid() bool {
	for _, v := range ValidStrategies {
		if s == v {
			return true
		}
	}
	return false
}

// SourceManual is the reserved source token for manually added nodes.
const SourceManual = "manual"

// Node is one deduplicated proxy in the pool. Proxy is the Clash proxy map and is
// the source of truth for the node's connection parameters; ID is its fingerprint.
type Node struct {
	ID       string         `json:"id"`       // fingerprint: sha256(canonical proxy map minus name)
	Name     string         `json:"name"`     // display name (first source's name)
	Protocol string         `json:"protocol"` // vmess/vless/ss/trojan/socks/...
	Server   string         `json:"server"`   // server address, for display
	Region   string         `json:"region"`   // heuristic region tag, "" if unknown
	Proxy    map[string]any `json:"-"`         // Clash proxy map (source of truth)
	Sources  []string       `json:"sources"`  // subscription ids, or SourceManual

	// Runtime-only latency fields, filled from the in-memory latency map.
	DelayMS int  `json:"delay_ms,omitempty"`
	Tested  bool `json:"tested"`
	Alive   bool `json:"alive"`
}

// NodeFromProxy builds a Node from a Clash proxy map and a source token
// (subscription id or SourceManual). The proxy map is the source of truth; the
// display fields are derived for the UI.
func NodeFromProxy(proxy map[string]any, source string) *Node {
	return NodeFromProxyWithID(proxy, source, Fingerprint(proxy))
}

// NodeFromProxyWithID is NodeFromProxy with a precomputed fingerprint, so callers
// that already hashed the proxy (e.g. pool dedup) don't hash it a second time.
func NodeFromProxyWithID(proxy map[string]any, source, id string) *Node {
	name, _ := proxy["name"].(string)
	return &Node{
		ID:       id,
		Name:     name,
		Protocol: stringField(proxy, "type"),
		Server:   stringField(proxy, "server"),
		Region:   Region(name),
		Proxy:    proxy,
		Sources:  []string{source},
	}
}

// CloneProxy shallow-copies a Clash proxy map so callers can validate or mutate a
// copy (e.g. set a display name) without touching the shared original.
func CloneProxy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func stringField(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return ""
	}
}

// NodeRef is a mapping's reference to a node: id is authoritative, name is a
// redundant human-readable label kept in config.yaml.
type NodeRef struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// HealthCheck configures a non-single mapping's group health check.
type HealthCheck struct {
	URL         string `json:"url" yaml:"url"`
	IntervalSec int    `json:"interval" yaml:"interval"`
}

// Health check defaults and bounds (design D4).
const (
	DefaultHealthCheckURL = "http://www.gstatic.com/generate_204"
	DefaultHealthInterval = 300
	MinHealthInterval     = 30
	MaxHealthInterval     = 3600
)

// DefaultHealthCheck returns the health check used when a non-single mapping
// omits it.
func DefaultHealthCheck() *HealthCheck {
	return &HealthCheck{URL: DefaultHealthCheckURL, IntervalSec: DefaultHealthInterval}
}

// EffectiveHealthCheck returns the mapping's health check, or the default for
// non-single strategies when unset. Returns nil for single.
func (m *Mapping) EffectiveHealthCheck() *HealthCheck {
	if m.Strategy == StrategySingle {
		return nil
	}
	if m.HealthCheck != nil {
		return m.HealthCheck
	}
	return DefaultHealthCheck()
}

// Mapping is a local port bound to a strategy over a node set. Port is the key.
// Either Nodes (explicit ordered list) or NodeFilter (RE2 regex) is set, not both.
type Mapping struct {
	Port        int         `json:"port" yaml:"port"`
	Name        string      `json:"name" yaml:"name"`
	Strategy    Strategy    `json:"strategy" yaml:"strategy"`
	Nodes       []NodeRef    `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	NodeFilter  string       `json:"node_filter,omitempty" yaml:"node_filter,omitempty"`
	HealthCheck *HealthCheck `json:"health_check,omitempty" yaml:"health_check,omitempty"` // non-single only; nil = not applicable
	Enabled     bool         `json:"enabled" yaml:"enabled"`

	// Optional inbound proxy auth. When set, clients must authenticate (HTTP
	// Basic / SOCKS5 user-pass) to use this port. Both or neither.
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`

	// Runtime-only fields; never persisted to config.yaml.
	DisabledReason string `json:"disabled_reason,omitempty" yaml:"-"`
	ActiveNode     string `json:"active_node,omitempty" yaml:"-"`
}

// UsesFilter reports whether the mapping selects nodes via NodeFilter.
func (m *Mapping) UsesFilter() bool { return m.NodeFilter != "" }
