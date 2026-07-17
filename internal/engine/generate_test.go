package engine

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wuxi/sub2proxy/internal/model"
)

var update = flag.Bool("update", false, "update golden files")

func node(name, server string, port int) model.Node {
	proxy := map[string]any{"name": name, "type": "ss", "server": server, "port": port,
		"cipher": "aes-256-gcm", "password": "secret"}
	return *model.NodeFromProxy(proxy, "subA")
}

func TestGenerateGolden(t *testing.T) {
	nodes := []model.Node{
		node("美国 1", "1.1.1.1", 8388),
		node("美国 2", "2.2.2.2", 8388),
		node("香港 1", "3.3.3.3", 8388),
		node("香港 1", "4.4.4.4", 8388), // same display name, different server -> disambiguated
		node("英国 1", "5.5.5.5", 8388),
	}
	ref := func(n model.Node) model.NodeRef { return model.NodeRef{ID: n.ID, Name: n.Name} }
	hc := model.DefaultHealthCheck()

	plans := []MappingPlan{
		{Port: 27001, Strategy: model.StrategySingle, Nodes: []model.NodeRef{ref(nodes[4])}, Enabled: true},
		{Port: 27002, Strategy: model.StrategyFailover, Nodes: []model.NodeRef{ref(nodes[0]), ref(nodes[1])}, HealthCheck: hc, Enabled: true},
		{Port: 27003, Strategy: model.StrategyRoundRobin, Nodes: []model.NodeRef{ref(nodes[0]), ref(nodes[1])}, HealthCheck: hc, Enabled: true},
		{Port: 27004, Strategy: model.StrategyHash, Nodes: []model.NodeRef{ref(nodes[2]), ref(nodes[3])}, HealthCheck: hc, Enabled: true},
		{Port: 27005, Strategy: model.StrategySticky, Nodes: []model.NodeRef{ref(nodes[0])}, HealthCheck: hc, Enabled: true},
		{Port: 27006, Strategy: model.StrategyAuto, Nodes: []model.NodeRef{ref(nodes[0]), ref(nodes[2])}, HealthCheck: hc, Enabled: true},
		{Port: 27007, Strategy: model.StrategyFailover, Nodes: []model.NodeRef{ref(nodes[0])}, HealthCheck: hc, Enabled: false}, // disabled -> no listener
	}

	got, err := Generate(nodes, plans)
	if err != nil {
		t.Fatal(err)
	}

	goldenPath := filepath.Join("testdata", "golden.yaml")
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("generated config differs from golden.\n--- got ---\n%s", got)
	}

	// Structural assertions independent of golden.
	s := string(got)
	if strings.Count(s, "name: '香港 1 #2'") != 1 {
		t.Error("expected exactly one disambiguated '香港 1 #2' proxy definition")
	}
	if strings.Contains(s, "in-27007") {
		t.Error("disabled mapping must not produce a listener")
	}
	if !strings.Contains(s, "type: fallback") || !strings.Contains(s, "type: url-test") || !strings.Contains(s, "consistent-hashing") {
		t.Error("expected strategy group types in output")
	}
	// single mapping references the node name directly, no pg-27001 group.
	if strings.Contains(s, "pg-27001") {
		t.Error("single mapping must not create a group")
	}
}

func TestGenerateInboundAuth(t *testing.T) {
	nodes := []model.Node{node("英国 1", "5.5.5.5", 8388)}
	plans := []MappingPlan{
		{Port: 27001, Strategy: model.StrategySingle, Nodes: []model.NodeRef{{ID: nodes[0].ID, Name: "英国 1"}},
			Enabled: true, Username: "alice", Password: "s3cret"},
		{Port: 27002, Strategy: model.StrategySingle, Nodes: []model.NodeRef{{ID: nodes[0].ID, Name: "英国 1"}},
			Enabled: true}, // no auth
	}
	out, err := Generate(nodes, plans)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "username: alice") || !strings.Contains(s, "password: s3cret") {
		t.Errorf("listener with auth should emit users, got:\n%s", s)
	}
	// The no-auth listener must not carry a users block.
	if strings.Count(s, "users:") != 1 {
		t.Errorf("expected exactly one users block, got %d", strings.Count(s, "users:"))
	}
}

func TestGenerateStable(t *testing.T) {
	nodes := []model.Node{node("b", "1.1.1.1", 1), node("a", "2.2.2.2", 2)}
	plans := []MappingPlan{{Port: 27001, Strategy: model.StrategySingle, Nodes: []model.NodeRef{{ID: nodes[0].ID, Name: "b"}}, Enabled: true}}
	a, _ := Generate(nodes, plans)
	b, _ := Generate(nodes, plans)
	if string(a) != string(b) {
		t.Error("generation must be deterministic")
	}
}
