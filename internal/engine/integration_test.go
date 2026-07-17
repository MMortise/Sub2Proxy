//go:build integration

// Integration test for the embedded mihomo data plane (tasks 3.6, 3.7). It binds
// real mixed listeners and routes traffic through local HTTP-proxy "nodes".
// Run with: go test -tags=integration ./internal/engine/
//
// mihomo's executor is a process-global singleton, so all assertions live in one
// test that applies configs sequentially.
package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/wuxi/sub2proxy/internal/model"
)

func TestEngineIntegration(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	aliveAddr := startForwardProxy(t)
	deadAddr := "127.0.0.1:1" // never accepts

	aliveNode := httpNode("alive", aliveAddr)
	deadNode := httpNode("dead", deadAddr)
	nodes := []model.Node{*aliveNode, *deadNode}
	ref := func(n *model.Node) model.NodeRef { return model.NodeRef{ID: n.ID, Name: n.Name} }
	hc := &model.HealthCheck{URL: backend.URL, IntervalSec: 30}

	plans := []MappingPlan{
		{Port: 27701, Strategy: model.StrategySingle, Nodes: []model.NodeRef{ref(aliveNode)}, Enabled: true},
		{Port: 27702, Strategy: model.StrategySingle, Nodes: []model.NodeRef{ref(deadNode)}, Enabled: true},
		{Port: 27703, Strategy: model.StrategyFailover, Nodes: []model.NodeRef{ref(deadNode), ref(aliveNode)}, HealthCheck: hc, Enabled: true},
	}

	eng := New(t.TempDir(), func() ([]model.Node, []MappingPlan) { return nodes, plans })
	if err := eng.ApplyNow(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	time.Sleep(400 * time.Millisecond) // let listeners bind & first health checks run

	// (1) Dual protocol through the alive single mapping.
	if code := httpProxyGet(t, "27701", backend.URL); code != http.StatusNoContent {
		t.Errorf("http via 27701: want 204, got %d", code)
	}
	if code := socks5Get(t, "127.0.0.1:27701", backend.URL); code != http.StatusNoContent {
		t.Errorf("socks5 via 27701: want 204, got %d", code)
	}

	// (2) The dead single mapping actually routes through the dead node -> fails.
	if code := httpProxyGet(t, "27702", backend.URL); code == http.StatusNoContent {
		t.Error("27702 routes through a dead node and should fail, but it succeeded")
	}

	// (3) Failover [dead, alive] skips the dead node and succeeds.
	if code := httpProxyGet(t, "27703", backend.URL); code != http.StatusNoContent {
		t.Errorf("failover via 27703: want 204, got %d", code)
	}

	// (4) Status reflects the failover group's active node = alive.
	st := eng.Status()
	var found bool
	for _, ms := range st.Mappings {
		if ms.Port == 27703 {
			found = true
			if ms.ActiveNode != "alive" {
				t.Errorf("27703 active node = %q, want alive", ms.ActiveNode)
			}
		}
	}
	if !found {
		t.Error("status missing mapping 27703")
	}

	// (5) Hot reload preserves an in-flight connection on an unchanged port.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(700 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer slow.Close()

	resultCh := make(chan int, 1)
	go func() { resultCh <- httpProxyGet(t, "27701", slow.URL) }()
	time.Sleep(150 * time.Millisecond) // ensure the request is in flight

	// Add a new mapping and reload while 27701's request is in flight.
	plans = append(plans, MappingPlan{Port: 27704, Strategy: model.StrategySingle, Nodes: []model.NodeRef{ref(aliveNode)}, Enabled: true})
	if err := eng.ApplyNow(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	select {
	case code := <-resultCh:
		if code != http.StatusNoContent {
			t.Errorf("in-flight request across reload: want 204, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Error("in-flight request did not complete after reload")
	}

	// New mapping works after reload.
	if code := httpProxyGet(t, "27704", backend.URL); code != http.StatusNoContent {
		t.Errorf("new mapping 27704 after reload: want 204, got %d", code)
	}
}

func httpNode(name, addr string) *model.Node {
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)
	return model.NodeFromProxy(map[string]any{
		"name": name, "type": "http", "server": host, "port": port,
	}, "test")
}

func httpProxyGet(t *testing.T, port, target string) int {
	t.Helper()
	proxyURL, _ := url.Parse("http://127.0.0.1:" + port)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func socks5Get(t *testing.T, addr, target string) int {
	t.Helper()
	dialer, err := proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
	if err != nil {
		return 0
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, a string) (net.Conn, error) {
			return dialer.Dial(network, a)
		}},
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(target)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// startForwardProxy runs a minimal CONNECT-capable forward HTTP proxy.
func startForwardProxy(t *testing.T) string {
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
			go serveForward(conn)
		}
	}()
	return ln.Addr().String()
}

func serveForward(conn net.Conn) {
	defer conn.Close()
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
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
