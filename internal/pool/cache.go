package pool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wuxi/sub2proxy/internal/fsutil"
)

// CacheFileName is the node cache basename under data_dir.
const CacheFileName = "nodes.json"

const cacheFileMode = 0o600

// cacheFile is the on-disk shape of nodes.json. Manual nodes are excluded — their
// source of truth is config.yaml (config-persistence spec).
type cacheFile struct {
	Subscriptions map[string]subCache `json:"subscriptions"`
}

type subCache struct {
	Proxies     []map[string]any `json:"proxies"`
	LastRefresh time.Time        `json:"last_refresh"`
}

// SaveCache atomically writes the subscription node cache to data_dir/nodes.json.
func (p *Pool) SaveCache(dataDir string) error {
	p.mu.RLock()
	cf := cacheFile{Subscriptions: make(map[string]subCache, len(p.subProxies))}
	for id, proxies := range p.subProxies {
		cf.Subscriptions[id] = subCache{Proxies: proxies, LastRefresh: p.subRefresh[id]}
	}
	p.mu.RUnlock()

	data, err := json.Marshal(cf)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	return fsutil.AtomicWrite(filepath.Join(dataDir, CacheFileName), data, cacheFileMode)
}

// LoadCache loads subscription nodes from data_dir/nodes.json and rebuilds. A
// missing or corrupt cache is ignored (returns nil) so startup is never blocked;
// the caller then refreshes subscriptions to rebuild (config-persistence spec).
func (p *Pool) LoadCache(dataDir string) error {
	data, err := os.ReadFile(filepath.Join(dataDir, CacheFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return nil // unreadable cache: ignore, rebuild from subscriptions
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil // corrupt cache: ignore
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, sc := range cf.Subscriptions {
		p.subProxies[id] = sc.Proxies
		p.subRefresh[id] = sc.LastRefresh
	}
	p.rebuild()
	return nil
}

// LastRefresh returns the cached last-refresh time for a subscription.
func (p *Pool) LastRefresh(subID string) (time.Time, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	t, ok := p.subRefresh[subID]
	return t, ok
}
