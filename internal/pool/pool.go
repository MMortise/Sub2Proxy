// Package pool holds the deduplicated node pool. The source of truth is the set
// of per-subscription proxy lists plus the manual proxy list; the deduped node
// map is derived by rebuild() so dedup and source-merging are always consistent
// (design D5, node-pool spec).
package pool

import (
	"sort"
	"sync"
	"time"

	"github.com/wuxi/sub2proxy/internal/model"
)

// LatencyResult is an in-memory latency measurement for a node fingerprint.
type LatencyResult struct {
	DelayMS  int       `json:"delay_ms"`
	OK       bool      `json:"ok"`
	TestedAt time.Time `json:"tested_at"`
}

// Pool is the concurrency-safe node pool.
type Pool struct {
	mu sync.RWMutex

	subProxies    map[string][]map[string]any // subID -> raw proxies (cached, persisted to nodes.json)
	subRefresh    map[string]time.Time        // subID -> last successful refresh
	manualProxies []map[string]any            // manual nodes (from config, not cached)

	nodes   map[string]*model.Node   // fp -> deduped node (derived)
	latency map[string]LatencyResult // fp -> latency (in-memory only)
}

// New returns an empty pool.
func New() *Pool {
	return &Pool{
		subProxies: map[string][]map[string]any{},
		subRefresh: map[string]time.Time{},
		nodes:      map[string]*model.Node{},
		latency:    map[string]LatencyResult{},
	}
}

// SetSubscriptionNodes replaces subID's contribution and rebuilds the pool,
// returning the deduped node count for that subscription's proxies.
func (p *Pool) SetSubscriptionNodes(subID string, proxies []map[string]any, refreshedAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subProxies[subID] = proxies
	p.subRefresh[subID] = refreshedAt
	p.rebuild()
}

// RemoveSubscription drops subID's contribution and rebuilds.
func (p *Pool) RemoveSubscription(subID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.subProxies, subID)
	delete(p.subRefresh, subID)
	p.rebuild()
}

// SetManualNodes replaces the manual proxy set and rebuilds.
func (p *Pool) SetManualNodes(proxies []map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.manualProxies = proxies
	p.rebuild()
}

// rebuild recomputes the deduped node map from the input proxy sets. Subscriptions
// are folded in sorted id order (manual last) so the retained display name — the
// first source's — is deterministic (node-pool spec). Caller holds p.mu.
func (p *Pool) rebuild() {
	nodes := make(map[string]*model.Node)
	add := func(proxy map[string]any, source string) {
		fp := model.Fingerprint(proxy)
		if n, ok := nodes[fp]; ok {
			n.Sources = appendUnique(n.Sources, source)
			return
		}
		nodes[fp] = model.NodeFromProxyWithID(proxy, source, fp)
	}

	subIDs := make([]string, 0, len(p.subProxies))
	for id := range p.subProxies {
		subIDs = append(subIDs, id)
	}
	sort.Strings(subIDs)
	for _, id := range subIDs {
		for _, proxy := range p.subProxies[id] {
			add(proxy, id)
		}
	}
	for _, proxy := range p.manualProxies {
		add(proxy, model.SourceManual)
	}

	// Prune latency entries for fingerprints that no longer exist.
	for fp := range p.latency {
		if _, ok := nodes[fp]; !ok {
			delete(p.latency, fp)
		}
	}
	p.nodes = nodes
}

// Nodes returns a snapshot of the pool sorted by name, with latency filled in.
func (p *Pool) Nodes() []model.Node {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]model.Node, 0, len(p.nodes))
	for fp, n := range p.nodes {
		view := *n
		if lat, ok := p.latency[fp]; ok {
			view.Tested = true
			view.Alive = lat.OK
			view.DelayMS = lat.DelayMS
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Get returns a copy of the node with fingerprint fp.
func (p *Pool) Get(fp string) (model.Node, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n, ok := p.nodes[fp]
	if !ok {
		return model.Node{}, false
	}
	return *n, true
}

// Proxy returns the raw Clash proxy map for fp (source of truth for the engine).
func (p *Pool) Proxy(fp string) (map[string]any, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n, ok := p.nodes[fp]
	if !ok {
		return nil, false
	}
	return n.Proxy, true
}

// Has reports whether fp is currently in the pool.
func (p *Pool) Has(fp string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.nodes[fp]
	return ok
}

// OnlyManual reports whether fp exists and its sole source is manual (deletable).
func (p *Pool) OnlyManual(fp string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n, ok := p.nodes[fp]
	return ok && len(n.Sources) == 1 && n.Sources[0] == model.SourceManual
}

// SetLatency records a latency measurement for fp.
func (p *Pool) SetLatency(fp string, r LatencyResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.nodes[fp]; ok {
		p.latency[fp] = r
	}
}

// Latency returns the recorded latency for fp, if any.
func (p *Pool) Latency(fp string) (LatencyResult, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	r, ok := p.latency[fp]
	return r, ok
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
