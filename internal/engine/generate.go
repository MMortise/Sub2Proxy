// Package engine embeds mihomo as the data plane: it generates a mihomo config
// from the node pool and mapping plans, applies it (hot reload), and reads back
// runtime status (design D1/D4, proxy-engine spec).
package engine

import (
	"fmt"
	"sort"

	"github.com/wuxi/sub2proxy/internal/model"
	"gopkg.in/yaml.v3"
)

// MappingPlan is an engine-ready mapping: node references already resolved to
// present pool nodes, and Enabled already reflecting any auto-disable.
type MappingPlan struct {
	Port        int
	Strategy    model.Strategy
	Nodes       []model.NodeRef
	HealthCheck *model.HealthCheck
	Enabled     bool
	Username    string
	Password    string
}

type mihomoConfig struct {
	Mode        string           `yaml:"mode"`
	LogLevel    string           `yaml:"log-level"`
	Proxies     []map[string]any `yaml:"proxies"`
	ProxyGroups []groupConfig    `yaml:"proxy-groups"`
	Listeners   []listenerConfig `yaml:"listeners"`
	Rules       []string         `yaml:"rules"`
}

type groupConfig struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
	Strategy string   `yaml:"strategy,omitempty"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
}

type listenerConfig struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Port   int            `yaml:"port"`
	Listen string         `yaml:"listen"`
	Proxy  string         `yaml:"proxy"`
	Users  []listenerUser `yaml:"users,omitempty"`
}

type listenerUser struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// groupType maps a strategy to its mihomo proxy-group type and load-balance
// strategy (design D4). single has no group.
func groupType(s model.Strategy) (typ, lbStrategy string) {
	switch s {
	case model.StrategyFailover:
		return "fallback", ""
	case model.StrategyRoundRobin:
		return "load-balance", "round-robin"
	case model.StrategyHash:
		return "load-balance", "consistent-hashing"
	case model.StrategySticky:
		return "load-balance", "sticky-sessions"
	case model.StrategyAuto:
		return "url-test", ""
	default:
		return "", ""
	}
}

// Generate builds the mihomo config YAML from the full node pool and mapping
// plans. All pool nodes go into proxies; display-name collisions are
// disambiguated with a " #N" suffix, and groups/listeners reference the
// disambiguated names. Disabled plans produce no listener or group. Output is
// deterministic (nodes sorted by fingerprint) for golden-file testing.
func Generate(nodes []model.Node, plans []MappingPlan) ([]byte, error) {
	_, cfg := build(nodes, plans)
	return yaml.Marshal(cfg)
}

// build assembles the mihomo config and the fingerprint->name map (needed by
// Status to resolve a single mapping's active node).
func build(nodes []model.Node, plans []MappingPlan) (nameOf map[string]string, cfg mihomoConfig) {
	sorted := make([]model.Node, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	nameOf = make(map[string]string, len(sorted)) // fp -> unique mihomo name
	usedName := make(map[string]int)
	proxies := make([]map[string]any, 0, len(sorted))
	for _, n := range sorted {
		base := n.Name
		if base == "" {
			base = model.ShortID(n.ID)
		}
		name := base
		if c := usedName[base]; c > 0 {
			name = fmt.Sprintf("%s #%d", base, c+1)
		}
		usedName[base]++
		nameOf[n.ID] = name

		clone := model.CloneProxy(n.Proxy)
		clone["name"] = name
		proxies = append(proxies, clone)
	}

	cfg = mihomoConfig{
		Mode:     "rule",
		LogLevel: "warning",
		Proxies:  proxies,
		Rules:    []string{"MATCH,DIRECT"},
	}

	// Stable plan order by port.
	ordered := make([]MappingPlan, len(plans))
	copy(ordered, plans)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Port < ordered[j].Port })

	for _, p := range ordered {
		if !p.Enabled || len(p.Nodes) == 0 {
			continue
		}
		names := make([]string, 0, len(p.Nodes))
		for _, ref := range p.Nodes {
			if n, ok := nameOf[ref.ID]; ok {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			continue
		}

		var proxyRef string
		if p.Strategy == model.StrategySingle {
			proxyRef = names[0]
		} else {
			gtype, lb := groupType(p.Strategy)
			hc := p.HealthCheck
			if hc == nil {
				hc = model.DefaultHealthCheck()
			}
			cfg.ProxyGroups = append(cfg.ProxyGroups, groupConfig{
				Name:     groupName(p.Port),
				Type:     gtype,
				Proxies:  names,
				Strategy: lb,
				URL:      hc.URL,
				Interval: hc.IntervalSec,
			})
			proxyRef = groupName(p.Port)
		}
		lc := listenerConfig{
			Name:   listenerName(p.Port),
			Type:   "mixed",
			Port:   p.Port,
			Listen: "0.0.0.0",
			Proxy:  proxyRef,
		}
		if p.Username != "" {
			lc.Users = []listenerUser{{Username: p.Username, Password: p.Password}}
		}
		cfg.Listeners = append(cfg.Listeners, lc)
	}

	return nameOf, cfg
}

func groupName(port int) string    { return fmt.Sprintf("pg-%d", port) }
func listenerName(port int) string { return fmt.Sprintf("in-%d", port) }
