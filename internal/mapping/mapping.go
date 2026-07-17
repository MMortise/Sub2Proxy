// Package mapping holds pure logic for port allocation, node-set resolution, and
// degrade decisions. It operates on plain values (no pool/engine deps) so it is
// easily testable (port-mapping spec).
package mapping

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/wuxi/sub2proxy/internal/model"
)

// AllocatePort returns the smallest free port in [lo, hi] not present in used.
// It returns an error when the range is exhausted (409 at the API layer).
func AllocatePort(lo, hi int, used map[int]bool) (int, error) {
	for port := lo; port <= hi; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port in range [%d, %d]", lo, hi)
}

// Resolution is the outcome of resolving a mapping against the current pool.
type Resolution struct {
	Nodes       []model.NodeRef // present nodes, in effective order (name-corrected)
	Missing     []model.NodeRef // explicitly referenced but absent
	AutoDisable bool            // true when the mapping cannot serve
	Reason      string          // populated when AutoDisable
}

// Resolve computes a mapping's live node set from the pool. For explicit Nodes it
// keeps present nodes in order (correcting display names from the pool) and lists
// missing ones; for NodeFilter it matches node names by regex, sorted by name.
// It sets AutoDisable when a single node vanished or a group emptied
// (port-mapping spec: node-disappearance degrade).
func Resolve(m *model.Mapping, nodes []model.Node) Resolution {
	var res Resolution
	if m.UsesFilter() {
		re, err := regexp.Compile(m.NodeFilter)
		if err != nil {
			return Resolution{AutoDisable: true, Reason: fmt.Sprintf("invalid node_filter: %v", err)}
		}
		matched := make([]model.NodeRef, 0)
		for _, n := range nodes {
			if re.MatchString(n.Name) {
				matched = append(matched, model.NodeRef{ID: n.ID, Name: n.Name})
			}
		}
		sort.Slice(matched, func(i, j int) bool {
			if matched[i].Name != matched[j].Name {
				return matched[i].Name < matched[j].Name
			}
			return matched[i].ID < matched[j].ID
		})
		res.Nodes = matched
		if len(matched) == 0 {
			res.AutoDisable = true
			res.Reason = "node_filter matches no nodes"
		}
		return res
	}

	// Explicit node list: keep order, correct names, track missing. The id index is
	// built only here, since the filter path above never needs it.
	byID := make(map[string]model.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for _, ref := range m.Nodes {
		if n, ok := byID[ref.ID]; ok {
			res.Nodes = append(res.Nodes, model.NodeRef{ID: n.ID, Name: n.Name})
		} else {
			res.Missing = append(res.Missing, ref)
		}
	}
	if len(res.Nodes) == 0 {
		res.AutoDisable = true
		if m.Strategy == model.StrategySingle {
			res.Reason = degradeReason("node", res.Missing)
		} else {
			res.Reason = degradeReason("all nodes", res.Missing)
		}
	}
	return res
}

func degradeReason(what string, missing []model.NodeRef) string {
	names := make([]string, 0, len(missing))
	for _, m := range missing {
		if m.Name != "" {
			names = append(names, m.Name)
		} else {
			names = append(names, model.ShortID(m.ID))
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("%s no longer available", what)
	}
	return fmt.Sprintf("%s no longer available: %v", what, names)
}
