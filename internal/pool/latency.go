package pool

import (
	"context"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"

	"github.com/wuxi/sub2proxy/internal/model"
)

// Latency test parameters (design D3 / node-pool spec). The probe URL is the same
// generate_204 endpoint used for group health checks (model.DefaultHealthCheckURL).
const (
	LatencyTestURL  = model.DefaultHealthCheckURL
	LatencyTimeout  = 5 * time.Second
	TestConcurrency = 8
)

// Tester runs real-link latency tests through nodes, deduping in-flight requests
// for the same fingerprint so concurrent callers don't double-dial a node.
type Tester struct {
	pool *Pool
	url  string

	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// NewTester builds a Tester bound to a pool.
func NewTester(p *Pool) *Tester {
	return &Tester{pool: p, url: LatencyTestURL, inflight: map[string]chan struct{}{}}
}

// Test measures one node's latency through its proxy and records the result.
// Concurrent Test calls for the same fp share a single measurement.
func (t *Tester) Test(ctx context.Context, fp string) LatencyResult {
	t.mu.Lock()
	if done, busy := t.inflight[fp]; busy {
		t.mu.Unlock()
		<-done // wait for the in-flight measurement, then return the recorded value
		if r, ok := t.pool.Latency(fp); ok {
			return r
		}
		return LatencyResult{OK: false, TestedAt: time.Now()}
	}
	done := make(chan struct{})
	t.inflight[fp] = done
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.inflight, fp)
		close(done)
		t.mu.Unlock()
	}()

	result := t.measure(ctx, fp)
	t.pool.SetLatency(fp, result)
	return result
}

// TestAll tests every node concurrently (bounded), returning when all finish.
// Callers that must not block (the API) should run this in a goroutine.
func (t *Tester) TestAll(ctx context.Context) {
	nodes := t.pool.Nodes()
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	t.TestSet(ctx, ids)
}

// TestSet tests the given node fingerprints concurrently (bounded), returning
// when all finish.
func (t *Tester) TestSet(ctx context.Context, ids []string) {
	sem := make(chan struct{}, TestConcurrency)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(fp string) {
			defer wg.Done()
			defer func() { <-sem }()
			t.Test(ctx, fp)
		}(id)
	}
	wg.Wait()
}

// measure dials the node and times a request to the test URL.
func (t *Tester) measure(ctx context.Context, fp string) LatencyResult {
	now := time.Now()
	proxyMap, ok := t.pool.Proxy(fp)
	if !ok {
		return LatencyResult{OK: false, TestedAt: now}
	}
	px, err := adapter.ParseProxy(model.CloneProxy(proxyMap))
	if err != nil {
		return LatencyResult{OK: false, TestedAt: now}
	}
	tctx, cancel := context.WithTimeout(ctx, LatencyTimeout)
	defer cancel()
	// nil expectedStatus => any response status counts as reachable.
	delay, err := px.URLTest(tctx, t.url, nil)
	if err != nil {
		return LatencyResult{OK: false, TestedAt: time.Now()}
	}
	return LatencyResult{DelayMS: int(delay), OK: true, TestedAt: time.Now()}
}
