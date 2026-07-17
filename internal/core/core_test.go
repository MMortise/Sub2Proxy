package core

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wuxi/sub2proxy/internal/config"
	"github.com/wuxi/sub2proxy/internal/model"
)

// newTestApp builds an App on a temp config without starting the engine reload
// loop, so no real listener ports are bound during unit tests.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("auth_key: supersecret\ndata_dir: "+dir+"\n"), 0o600)
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded.DataDir = dir
	a := New(loaded, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(a.Shutdown)
	return a
}

func ssLink(tag string) string {
	// ss://base64(method:password)@host:port#tag ; host varies by tag for distinct fps.
	return "ss://YWVzLTI1Ni1nY206c2VjcmV0@" + tag + ".example.com:8388#" + tag
}

func TestManualNodeAddAndDelete(t *testing.T) {
	a := newTestApp(t)
	n, err := a.AddManualNode(ssLink("node-a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Nodes("")) != 1 {
		t.Fatalf("want 1 node, got %d", len(a.Nodes("")))
	}
	if !a.pool.OnlyManual(n.ID) {
		t.Error("node should be only-manual")
	}
	if err := a.DeleteNode(n.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.Nodes("")) != 0 {
		t.Fatal("node should be gone after delete")
	}
}

func TestManualNodeBadLink(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.AddManualNode("not a link"); err == nil {
		t.Fatal("want parse error")
	} else if HTTPStatus(err) != http.StatusBadRequest {
		t.Errorf("want 400, got %d", HTTPStatus(err))
	}
}

func TestCreateMappingAutoAllocAndConflict(t *testing.T) {
	a := newTestApp(t)
	n, _ := a.AddManualNode(ssLink("us1"))

	m1, err := a.CreateMapping(MappingInput{Name: "us", Strategy: model.StrategySingle,
		Nodes: []model.NodeRef{{ID: n.ID, Name: n.Name}}})
	if err != nil {
		t.Fatal(err)
	}
	if m1.Port != 27100 {
		t.Fatalf("want auto-allocated 27100, got %d", m1.Port)
	}

	// Second auto-alloc should get 27101.
	n2, _ := a.AddManualNode(ssLink("us2"))
	m2, err := a.CreateMapping(MappingInput{Name: "us-b", Strategy: model.StrategySingle,
		Nodes: []model.NodeRef{{ID: n2.ID, Name: n2.Name}}})
	if err != nil {
		t.Fatal(err)
	}
	if m2.Port != 27101 {
		t.Fatalf("want 27101, got %d", m2.Port)
	}

	// Explicit conflicting port -> 409 with owner name.
	_, err = a.CreateMapping(MappingInput{Port: 27100, Name: "dup", Strategy: model.StrategySingle,
		Nodes: []model.NodeRef{{ID: n.ID, Name: n.Name}}})
	if err == nil || HTTPStatus(err) != http.StatusConflict {
		t.Fatalf("want 409 conflict, got %v", err)
	}

	// Out-of-range port -> 400.
	_, err = a.CreateMapping(MappingInput{Port: 8080, Name: "oor", Strategy: model.StrategySingle,
		Nodes: []model.NodeRef{{ID: n.ID, Name: n.Name}}})
	if err == nil || HTTPStatus(err) != http.StatusBadRequest {
		t.Fatalf("want 400 for out-of-range, got %v", err)
	}
}

func TestMappingDegradeDoesNotRewriteEnabled(t *testing.T) {
	a := newTestApp(t)
	n, _ := a.AddManualNode(ssLink("us1"))
	_, err := a.CreateMapping(MappingInput{Port: 27100, Name: "us", Strategy: model.StrategySingle,
		Nodes: []model.NodeRef{{ID: n.ID, Name: n.Name}}})
	if err != nil {
		t.Fatal(err)
	}

	// Remove the node: mapping should show a disabled reason but keep Enabled=true
	// in the persisted config (runtime auto-disable != config change).
	if err := a.DeleteNode(n.ID); err != nil {
		t.Fatal(err)
	}
	ms := a.Mappings()
	if len(ms) != 1 {
		t.Fatalf("want 1 mapping, got %d", len(ms))
	}
	if ms[0].DisabledReason == "" {
		t.Error("expected an auto-disable reason after node removal")
	}
	// Persisted config still has Enabled=true.
	a.mu.RLock()
	persistedEnabled := a.cfg.Mappings[0].Enabled
	a.mu.RUnlock()
	if !persistedEnabled {
		t.Error("config Enabled must not be rewritten by auto-disable")
	}

	// Re-add the node: mapping recovers (no disabled reason).
	if _, err := a.AddManualNode(ssLink("us1")); err != nil {
		t.Fatal(err)
	}
	if a.Mappings()[0].DisabledReason != "" {
		t.Error("mapping should recover after node reappears")
	}
}

func TestSubscriptionAddRefreshAndFailure(t *testing.T) {
	var serve func(w http.ResponseWriter)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serve(w)
	}))
	defer srv.Close()

	serve = func(w http.ResponseWriter) {
		w.Write([]byte("proxies:\n  - {name: Sub-1, type: ss, server: 5.5.5.5, port: 8388, cipher: aes-256-gcm, password: secret}\n"))
	}
	a := newTestApp(t)
	sub, count, err := a.AddSubscription(SubscriptionInput{Name: "airport", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want 1 node from subscription, got %d", count)
	}
	if len(a.Nodes("")) != 1 {
		t.Fatalf("pool should have the subscription node")
	}

	// Duplicate URL -> 409.
	if _, _, err := a.AddSubscription(SubscriptionInput{Name: "dup", URL: srv.URL}); err == nil || HTTPStatus(err) != http.StatusConflict {
		t.Fatalf("want 409 for duplicate URL, got %v", err)
	}

	// Now make the server fail; refresh should preserve the existing node.
	serve = func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }
	if _, err := a.RefreshSubscription(sub.ID); err == nil {
		t.Fatal("want refresh error")
	}
	if len(a.Nodes("")) != 1 {
		t.Fatal("failed refresh must preserve existing nodes")
	}
	subs := a.Subscriptions()
	if subs[0].LastError == "" {
		t.Error("subscription should record last error")
	}
}

func TestConfigSnapshotExcludesRuntime(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.cfg.Subscriptions = []model.Subscription{{ID: "x", Name: "a", URL: "http://x", NodeCount: 9, LastError: "boom"}}
	a.mu.Unlock()
	snap := a.configSnapshot()
	// Runtime fields carry yaml:"-", so a persisted round-trip drops them; here we
	// just confirm the snapshot copies the slice (mutation isolation).
	snap.Subscriptions[0].Name = "changed"
	if a.Subscriptions()[0].Name == "changed" {
		t.Error("snapshot must be an independent copy")
	}
}

var _ = time.Second
