// Package core is the orchestrator: it holds the authoritative in-memory state
// (config + node pool) under a single RWMutex and wires config, subscribe, pool,
// mapping, and engine together. The API layer calls App methods; background
// workers refresh subscriptions and drive engine reloads (design D9).
package core

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/wuxi/sub2proxy/internal/config"
	"github.com/wuxi/sub2proxy/internal/engine"
	"github.com/wuxi/sub2proxy/internal/mapping"
	"github.com/wuxi/sub2proxy/internal/model"
	"github.com/wuxi/sub2proxy/internal/pool"
	"github.com/wuxi/sub2proxy/internal/subscribe"
)

// App is the orchestrator.
type App struct {
	mu  sync.RWMutex
	cfg *config.Config

	pool    *pool.Pool
	fetcher *subscribe.Fetcher
	tester  *pool.Tester
	store   *config.Store
	engine  *engine.Engine
	log     *slog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	workers map[string]*subWorker // subID -> refresh worker
	wmu     sync.Mutex
}

// New builds an App around a loaded config. The config Store and engine are wired
// to snapshot callbacks so persistence and reloads always see current state.
func New(cfg *config.Config, logger *slog.Logger) *App {
	a := &App{
		cfg:     cfg,
		pool:    pool.New(),
		fetcher: subscribe.NewFetcher(),
		log:     logger,
		workers: map[string]*subWorker{},
	}
	a.tester = pool.NewTester(a.pool)
	a.store = config.NewStore(cfg.Path(), a.configSnapshot)
	a.engine = engine.New(cfg.DataDir, a.engineSnapshot)
	// Default context so workers spawned before Start (or in tests) don't panic;
	// Start re-derives it from the caller's context.
	a.ctx, a.cancel = context.WithCancel(context.Background())
	return a
}

// Start loads the node cache, parses manual nodes, applies the initial engine
// config, and launches refresh workers. It returns once startup is done; the
// engine reload loop and workers run until ctx is cancelled.
func (a *App) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.engine.Start(a.ctx)

	if err := a.pool.LoadCache(a.cfg.DataDir); err != nil {
		a.log.Warn("load node cache", "err", err)
	}
	a.reloadManualNodes()

	// Restore per-subscription runtime state from the cache and start workers.
	a.mu.Lock()
	for i := range a.cfg.Subscriptions {
		s := &a.cfg.Subscriptions[i]
		if t, ok := a.pool.LastRefresh(s.ID); ok {
			s.LastRefresh = t
		}
	}
	subs := append([]model.Subscription(nil), a.cfg.Subscriptions...)
	a.mu.Unlock()

	// Initial data-plane apply from whatever the cache gave us.
	if err := a.engine.ApplyNow(); err != nil {
		a.log.Warn("initial engine apply", "err", err)
	}

	for _, s := range subs {
		a.startWorker(s.ID)
	}
	// Refresh any subscription without cached nodes soon after boot.
	go a.initialRefresh(ctx, subs)
	return nil
}

// initialRefresh fetches subscriptions that have no cached nodes yet.
func (a *App) initialRefresh(ctx context.Context, subs []model.Subscription) {
	for _, s := range subs {
		if _, ok := a.pool.LastRefresh(s.ID); ok {
			continue // cache already has nodes; the worker will refresh on schedule
		}
		a.refreshOne(ctx, s.ID)
	}
}

// Shutdown cancels background workers, flushes pending config writes, and saves
// the node cache. Bounded by the caller's deadline.
func (a *App) Shutdown() {
	a.cancel()
	a.stopAllWorkers()
	if err := a.store.Flush(); err != nil {
		a.log.Warn("flush config on shutdown", "err", err)
	}
	a.saveCache()
}

func (a *App) stopAllWorkers() {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	for id, w := range a.workers {
		close(w.stop)
		delete(a.workers, id)
	}
}

// Engine exposes the engine for status queries.
func (a *App) Engine() *engine.Engine { return a.engine }

// configSnapshot returns a copy of the config for persistence. Called by Store.
func (a *App) configSnapshot() *config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	c := *a.cfg
	c.Subscriptions = append([]model.Subscription(nil), a.cfg.Subscriptions...)
	c.ManualNodes = append([]string(nil), a.cfg.ManualNodes...)
	c.Mappings = append([]model.Mapping(nil), a.cfg.Mappings...)
	return &c
}

// engineSnapshot resolves every mapping against the pool and returns engine-ready
// plans. This is where node-disappearance degrade is applied to the data plane:
// a mapping's effective Enabled is its configured Enabled AND not auto-disabled
// (design D5, port-mapping spec). Called by the engine on each reload.
func (a *App) engineSnapshot() ([]model.Node, []engine.MappingPlan) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	nodes := a.pool.Nodes()
	plans := make([]engine.MappingPlan, 0, len(a.cfg.Mappings))
	for i := range a.cfg.Mappings {
		m := &a.cfg.Mappings[i]
		res := mapping.Resolve(m, nodes)
		plans = append(plans, engine.MappingPlan{
			Port:        m.Port,
			Strategy:    m.Strategy,
			Nodes:       res.Nodes,
			HealthCheck: m.EffectiveHealthCheck(),
			Enabled:     m.Enabled && !res.AutoDisable,
			Username:    m.Username,
			Password:    m.Password,
		})
	}
	return nodes, plans
}

// persistAndReload schedules a debounced config write and an engine reload. Call
// after any config mutation (mapping/manual-node/subscription edit); holds no
// lock. It does NOT touch the node cache — only subscription-node mutations change
// nodes.json, and those paths call saveCache explicitly.
func (a *App) persistAndReload() {
	a.store.Schedule()
	a.engine.Trigger()
}

// saveCache persists the subscription node cache (nodes.json), logging on failure.
// Only paths that mutate the pool's subscription proxies need it.
func (a *App) saveCache() {
	if err := a.pool.SaveCache(a.cfg.DataDir); err != nil {
		a.log.Warn("save node cache", "err", err)
	}
}

// reloadManualNodes parses all manual share links and updates the pool's manual
// contribution. Unparseable links are skipped with a warning (they were already
// validated on add). Caller must not hold a.mu.
func (a *App) reloadManualNodes() {
	a.mu.RLock()
	links := append([]string(nil), a.cfg.ManualNodes...)
	a.mu.RUnlock()

	var proxies []map[string]any
	for _, link := range links {
		ps, _, err := subscribe.Parse([]byte(link))
		if err != nil {
			a.log.Warn("parse manual node", "link", link, "err", err)
			continue
		}
		proxies = append(proxies, ps...)
	}
	a.pool.SetManualNodes(proxies)
}

// usedPorts returns the set of mapping ports currently in use. Caller holds a.mu.
func (a *App) usedPorts(exclude int) map[int]bool {
	used := make(map[int]bool, len(a.cfg.Mappings))
	for _, m := range a.cfg.Mappings {
		if m.Port != exclude {
			used[m.Port] = true
		}
	}
	return used
}

// nowFunc is overridable in tests.
var nowFunc = time.Now
