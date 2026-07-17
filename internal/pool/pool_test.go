package pool

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wuxi/sub2proxy/internal/model"
)

func ssMap(name, server string, port int) map[string]any {
	return map[string]any{
		"name": name, "type": "ss", "server": server, "port": port,
		"cipher": "aes-256-gcm", "password": "secret",
	}
}

func TestDedupMergeSourcesAcrossSubs(t *testing.T) {
	p := New()
	// Same connection params, different names, across two subscriptions.
	p.SetSubscriptionNodes("subA", []map[string]any{ssMap("美国 1", "1.2.3.4", 8388)}, time.Now())
	p.SetSubscriptionNodes("subB", []map[string]any{ssMap("US-01", "1.2.3.4", 8388)}, time.Now())

	nodes := p.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("want 1 deduped node, got %d", len(nodes))
	}
	n := nodes[0]
	if len(n.Sources) != 2 {
		t.Fatalf("want 2 sources, got %v", n.Sources)
	}
	// Deterministic name: subA sorts before subB, so "美国 1" wins.
	if n.Name != "美国 1" {
		t.Errorf("want first-source name '美国 1', got %q", n.Name)
	}
}

func TestManualMergePreservedWhenSubGone(t *testing.T) {
	p := New()
	node := ssMap("美国 1", "1.2.3.4", 8388)
	p.SetSubscriptionNodes("subA", []map[string]any{node}, time.Now())
	p.SetManualNodes([]map[string]any{ssMap("我的", "1.2.3.4", 8388)}) // same fp

	fp := model.Fingerprint(node)
	if !p.Has(fp) {
		t.Fatal("node should exist")
	}
	if p.OnlyManual(fp) {
		t.Fatal("node has two sources, not only-manual")
	}

	// Subscription refresh drops the node; manual source keeps it alive.
	p.SetSubscriptionNodes("subA", nil, time.Now())
	if !p.Has(fp) {
		t.Fatal("node should survive via manual source")
	}
	if !p.OnlyManual(fp) {
		t.Fatal("node should now be only-manual")
	}
}

func TestNodeDisappearsWhenAllSourcesGone(t *testing.T) {
	p := New()
	node := ssMap("美国 1", "1.2.3.4", 8388)
	p.SetSubscriptionNodes("subA", []map[string]any{node}, time.Now())
	fp := model.Fingerprint(node)
	p.SetSubscriptionNodes("subA", nil, time.Now())
	if p.Has(fp) {
		t.Fatal("node should be gone when its only source removed it")
	}
}

func TestCacheRoundTripExcludesManual(t *testing.T) {
	dir := t.TempDir()
	p := New()
	p.SetSubscriptionNodes("subA", []map[string]any{ssMap("美国 1", "1.2.3.4", 8388)}, time.Now())
	p.SetManualNodes([]map[string]any{ssMap("手动", "9.9.9.9", 8388)})
	if err := p.SaveCache(dir); err != nil {
		t.Fatal(err)
	}
	// Cache file must be 0600.
	info, _ := os.Stat(filepath.Join(dir, CacheFileName))
	if info.Mode().Perm() != cacheFileMode {
		t.Errorf("cache mode = %v", info.Mode().Perm())
	}

	// Reload into a fresh pool: subscription node present, manual absent.
	p2 := New()
	if err := p2.LoadCache(dir); err != nil {
		t.Fatal(err)
	}
	nodes := p2.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("want 1 cached (subscription) node, got %d", len(nodes))
	}
	if nodes[0].Server != "1.2.3.4" {
		t.Errorf("cached node server = %q", nodes[0].Server)
	}
}

func TestLoadCacheCorruptIgnored(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, CacheFileName), []byte("{not json"), 0o600)
	p := New()
	if err := p.LoadCache(dir); err != nil {
		t.Fatalf("corrupt cache should be ignored, got %v", err)
	}
	if len(p.Nodes()) != 0 {
		t.Fatal("corrupt cache should yield empty pool")
	}
}

func TestLoadCacheMissingIgnored(t *testing.T) {
	p := New()
	if err := p.LoadCache(t.TempDir()); err != nil {
		t.Fatalf("missing cache should be ignored, got %v", err)
	}
}

func TestLatencyUnreachable(t *testing.T) {
	p := New()
	// Point at an unroutable server so the dial fails fast.
	p.SetManualNodes([]map[string]any{ssMap("dead", "127.0.0.1", 1)})
	fp := model.Fingerprint(ssMap("dead", "127.0.0.1", 1))
	r := NewTester(p).Test(context.Background(), fp)
	if r.OK {
		t.Fatal("unreachable node should report not OK")
	}
	if got, _ := p.Latency(fp); got.OK {
		t.Fatal("recorded latency should be not OK")
	}
}

func TestLatencyThroughHTTPProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	proxyAddr := startHTTPProxy(t)

	host, portStr, _ := net.SplitHostPort(proxyAddr)
	port := atoiT(t, portStr)
	httpNode := map[string]any{
		"name": "local-http", "type": "http", "server": host, "port": port,
	}
	p := New()
	p.SetManualNodes([]map[string]any{httpNode})
	fp := model.Fingerprint(httpNode)

	tester := NewTester(p)
	tester.url = backend.URL // plain-HTTP target, forwarded by the http proxy
	r := tester.Test(context.Background(), fp)
	if !r.OK {
		t.Fatalf("expected latency through local http proxy, got not OK")
	}
}

func TestLatencyInflightDedup(t *testing.T) {
	proxyAddr := startHTTPProxy(t)
	host, portStr, _ := net.SplitHostPort(proxyAddr)
	httpNode := map[string]any{"name": "p", "type": "http", "server": host, "port": atoiT(t, portStr)}
	p := New()
	p.SetManualNodes([]map[string]any{httpNode})
	fp := model.Fingerprint(httpNode)

	tester := NewTester(p)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(204)
	}))
	defer backend.Close()
	tester.url = backend.URL

	// Two concurrent tests for the same fp: both should return without error.
	done := make(chan LatencyResult, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- tester.Test(context.Background(), fp) }()
	}
	for i := 0; i < 2; i++ {
		if r := <-done; !r.OK {
			t.Error("concurrent test returned not OK")
		}
	}
}

// startHTTPProxy runs a minimal forward HTTP proxy (plain GET forwarding) and
// returns its address. Enough for mihomo's http outbound to reach an http:// URL.
func startHTTPProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleProxyConn(conn)
		}
	}()
	return ln.Addr().String()
}

func handleProxyConn(conn net.Conn) {
	defer conn.Close()
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	// mihomo's http outbound reaches targets via CONNECT tunneling, which is what
	// URLTest's DialContext uses. Support CONNECT by dialing the target and piping
	// bytes both ways.
	if req.Method == http.MethodConnect {
		target, err := net.DialTimeout("tcp", req.Host, 3*time.Second)
		if err != nil {
			io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		defer target.Close()
		io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go io.Copy(target, conn)
		io.Copy(conn, target)
		return
	}
	// Absolute-URI GET fallback.
	outReq, err := http.NewRequest(req.Method, req.RequestURI, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	resp.Write(conn)
}

func atoiT(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("bad port %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

var _ = strings.TrimSpace
