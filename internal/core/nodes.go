package core

import (
	"context"
	"strings"

	"github.com/wuxi/sub2proxy/internal/model"
	"github.com/wuxi/sub2proxy/internal/pool"
	"github.com/wuxi/sub2proxy/internal/subscribe"
)

// Nodes returns pool nodes, optionally filtered by a case-insensitive name
// substring.
func (a *App) Nodes(query string) []model.Node {
	nodes := a.pool.Nodes()
	if query == "" {
		return nodes
	}
	q := strings.ToLower(query)
	out := nodes[:0]
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.Name), q) {
			out = append(out, n)
		}
	}
	return out
}

// AddManualNode parses a share link and adds it as a manual node. It must yield
// exactly one proxy; the raw link is persisted to config.yaml.
func (a *App) AddManualNode(link string) (model.Node, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return model.Node{}, badRequest("link is required")
	}
	proxies, _, err := subscribe.Parse([]byte(link))
	if err != nil {
		return model.Node{}, badRequest("cannot parse share link: " + err.Error())
	}
	if len(proxies) != 1 {
		return model.Node{}, badRequest("a manual node must be a single share link")
	}
	fp := model.Fingerprint(proxies[0])

	a.mu.Lock()
	a.cfg.ManualNodes = append(a.cfg.ManualNodes, link)
	a.mu.Unlock()

	a.reloadManualNodes()
	a.persistAndReload()

	if n, ok := a.pool.Get(fp); ok {
		return n, nil
	}
	return *model.NodeFromProxy(proxies[0], model.SourceManual), nil
}

// DeleteNode removes a node whose only source is manual. It locates the manual
// link with the matching fingerprint and drops it.
func (a *App) DeleteNode(id string) error {
	if !a.pool.Has(id) {
		return notFound("node not found")
	}
	if !a.pool.OnlyManual(id) {
		return badRequest("only manually added nodes can be deleted")
	}
	a.mu.Lock()
	removed := false
	kept := a.cfg.ManualNodes[:0]
	for _, link := range a.cfg.ManualNodes {
		if !removed && manualLinkMatches(link, id) {
			removed = true
			continue
		}
		kept = append(kept, link)
	}
	a.cfg.ManualNodes = kept
	a.mu.Unlock()

	if !removed {
		return notFound("manual node not found")
	}
	a.reloadManualNodes()
	a.persistAndReload()
	return nil
}

// TestNode measures one node's latency synchronously.
func (a *App) TestNode(id string) (pool.LatencyResult, error) {
	if !a.pool.Has(id) {
		return pool.LatencyResult{}, notFound("node not found")
	}
	return a.tester.Test(context.Background(), id), nil
}

// TestAllNodes starts a latency sweep in the background and returns immediately.
// An empty source tests every node; otherwise only nodes whose sources include
// that source (a subscription id, or "manual") are tested (node-pool spec).
func (a *App) TestAllNodes(source string) {
	if source == "" {
		go a.tester.TestAll(context.Background())
		return
	}
	var ids []string
	for _, n := range a.pool.Nodes() {
		for _, s := range n.Sources {
			if s == source {
				ids = append(ids, n.ID)
				break
			}
		}
	}
	go a.tester.TestSet(context.Background(), ids)
}

func manualLinkMatches(link, fp string) bool {
	proxies, _, err := subscribe.Parse([]byte(strings.TrimSpace(link)))
	if err != nil || len(proxies) != 1 {
		return false
	}
	return model.Fingerprint(proxies[0]) == fp
}
