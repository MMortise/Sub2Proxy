package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	mihomoC "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel"
	"github.com/metacubex/mihomo/tunnel/statistic"
	"gopkg.in/yaml.v3"

	"github.com/wuxi/sub2proxy/internal/model"
)

// ReloadDebounce collapses a burst of reload triggers into one apply (design D9).
const ReloadDebounce = 500 * time.Millisecond

// Snapshot supplies the engine with the current pool and mapping plans at apply
// time, so a debounced reload always uses the latest state.
type Snapshot func() ([]model.Node, []MappingPlan)

// Engine embeds mihomo. Apply generates and hot-reloads a config; on failure the
// last good config keeps serving and the error is recorded (proxy-engine spec).
type Engine struct {
	snapshot Snapshot
	debounce time.Duration

	mu         sync.Mutex
	lastGood   []byte
	lastPlans  []MappingPlan
	lastNameOf map[string]string
	lastErr    string
	lastErrAt  time.Time

	reloadCh chan struct{}

	rateMu   sync.Mutex
	ratePrev map[int]byteSample // per-port cumulative bytes for rate calc
}

type byteSample struct {
	up, down int64
	at       time.Time
}

// New builds an Engine. dataDir is set as mihomo's home dir so its cache lives
// under the app's data directory instead of ~/.config/mihomo.
func New(dataDir string, snapshot Snapshot) *Engine {
	mihomoC.SetHomeDir(dataDir)
	log.SetLevel(log.WARNING)
	return &Engine{
		snapshot: snapshot,
		debounce: ReloadDebounce,
		reloadCh: make(chan struct{}, 1),
		ratePrev: map[int]byteSample{},
	}
}

// Start launches the debounced reload consumer. It returns after starting the
// goroutine; call Trigger to request reloads.
func (e *Engine) Start(ctx context.Context) {
	go e.reloadLoop(ctx)
}

// Trigger requests a debounced reload. Non-blocking: extra triggers coalesce.
func (e *Engine) Trigger() {
	select {
	case e.reloadCh <- struct{}{}:
	default:
	}
}

func (e *Engine) reloadLoop(ctx context.Context) {
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.reloadCh:
			if timer == nil {
				timer = time.NewTimer(e.debounce)
				timerC = timer.C
			} else {
				timer.Reset(e.debounce)
			}
		case <-timerC:
			timer = nil
			timerC = nil
			if err := e.applyNow(); err != nil {
				log.Warnln("[engine] reload failed: %v", err)
			}
		}
	}
}

// ApplyNow forces a synchronous apply (used at startup). Reports errors but never
// panics; a bad config leaves the previous one in place.
func (e *Engine) ApplyNow() error { return e.applyNow() }

func (e *Engine) applyNow() error {
	nodes, plans := e.snapshot()
	nameOf, cfgStruct := build(nodes, plans)
	raw, err := yaml.Marshal(cfgStruct)
	if err != nil {
		e.recordErr(fmt.Sprintf("marshal config: %v", err))
		return err
	}
	cfg, err := executor.ParseWithBytes(raw)
	if err != nil {
		e.recordErr(fmt.Sprintf("parse config: %v", err))
		return err
	}
	// force=true is required for the tunnel to route our named listeners (with
	// force=false mihomo binds the listener but drops connections). Connection
	// preservation on unchanged ports still holds: named listeners are diffed by
	// listener.PatchInboundListeners regardless of force, and our top-level
	// inbounds are empty so force merely no-op recreates them (proxy-engine spec:
	// hot reload preserves existing connections).
	executor.ApplyConfig(cfg, true)

	e.mu.Lock()
	e.lastGood = raw
	e.lastPlans = plans
	e.lastNameOf = nameOf
	e.lastErr = ""
	e.lastErrAt = time.Time{}
	e.mu.Unlock()
	return nil
}

func (e *Engine) recordErr(msg string) {
	e.mu.Lock()
	e.lastErr = msg
	e.lastErrAt = time.Now()
	e.mu.Unlock()
}

// LastError returns the most recent reload error and its time, if any.
func (e *Engine) LastError() (string, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastErr, e.lastErrAt
}

// MappingStatus is the runtime state of one mapping.
type MappingStatus struct {
	Port        int    `json:"port"`
	ActiveNode  string `json:"active_node"`
	Connections int    `json:"connections"`
	UpRate      int64  `json:"up_rate"`   // bytes/sec since last Status call
	DownRate    int64  `json:"down_rate"` // bytes/sec since last Status call
}

// Status is the whole-engine runtime snapshot.
type Status struct {
	Mappings    []MappingStatus `json:"mappings"`
	LastError   string          `json:"last_error,omitempty"`
	LastErrorAt time.Time       `json:"last_error_at,omitempty"`
	TotalUp     int64           `json:"total_up_rate"`
	TotalDown   int64           `json:"total_down_rate"`
}

// Status reports per-mapping active node, connection count, and byte rate. Rates
// are computed by diffing per-port cumulative bytes against the previous call.
func (e *Engine) Status() Status {
	e.mu.Lock()
	plans := e.lastPlans
	nameOf := e.lastNameOf
	lastErr, lastErrAt := e.lastErr, e.lastErrAt
	e.mu.Unlock()

	// Aggregate per-port connection counts and cumulative bytes.
	type portAgg struct {
		conns    int
		up, down int64
	}
	agg := map[int]*portAgg{}
	snap := statistic.DefaultManager.Snapshot()
	for _, ti := range snap.Connections {
		if ti.Metadata == nil {
			continue
		}
		port := int(ti.Metadata.InPort)
		a := agg[port]
		if a == nil {
			a = &portAgg{}
			agg[port] = a
		}
		a.conns++
		a.up += ti.UploadTotal.Load()
		a.down += ti.DownloadTotal.Load()
	}

	proxies := tunnel.Proxies()
	now := time.Now()

	// Non-nil slice so an empty status serializes to [] rather than null.
	out := Status{Mappings: []MappingStatus{}, LastError: lastErr, LastErrorAt: lastErrAt}
	totalUp, totalDown := statistic.DefaultManager.Now()
	out.TotalUp, out.TotalDown = totalUp, totalDown

	e.rateMu.Lock()
	defer e.rateMu.Unlock()
	for _, p := range plans {
		if !p.Enabled {
			continue
		}
		ms := MappingStatus{Port: p.Port, ActiveNode: e.activeNode(p, nameOf, proxies)}
		if a := agg[p.Port]; a != nil {
			ms.Connections = a.conns
			ms.UpRate, ms.DownRate = e.rateFor(p.Port, a.up, a.down, now)
		} else {
			e.rateFor(p.Port, 0, 0, now) // reset baseline
		}
		out.Mappings = append(out.Mappings, ms)
	}
	return out
}

// activeNode resolves a mapping's current outbound node name.
func (e *Engine) activeNode(p MappingPlan, nameOf map[string]string, proxies map[string]mihomoProxy) string {
	if p.Strategy == model.StrategySingle {
		if len(p.Nodes) > 0 {
			return nameOf[p.Nodes[0].ID]
		}
		return ""
	}
	if px, ok := proxies[groupName(p.Port)]; ok {
		if g, ok := px.Adapter().(interface{ Now() string }); ok {
			return g.Now()
		}
	}
	return ""
}

// rateFor computes bytes/sec for a port by diffing cumulative totals against the
// previous sample. Caller holds e.rateMu.
func (e *Engine) rateFor(port int, up, down int64, now time.Time) (upRate, downRate int64) {
	prev, ok := e.ratePrev[port]
	e.ratePrev[port] = byteSample{up: up, down: down, at: now}
	if !ok {
		return 0, 0
	}
	dt := now.Sub(prev.at).Seconds()
	if dt <= 0 {
		return 0, 0
	}
	if du := up - prev.up; du > 0 {
		upRate = int64(float64(du) / dt)
	}
	if dd := down - prev.down; dd > 0 {
		downRate = int64(float64(dd) / dt)
	}
	return upRate, downRate
}

// mihomoProxy is the subset of the mihomo proxy we use from tunnel.Proxies().
type mihomoProxy = mihomoC.Proxy
