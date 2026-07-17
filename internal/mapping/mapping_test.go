package mapping

import (
	"testing"

	"github.com/wuxi/sub2proxy/internal/model"
)

func TestAllocatePortSmallestFree(t *testing.T) {
	used := map[int]bool{27001: true, 27002: true, 27004: true}
	got, err := AllocatePort(27001, 27999, used)
	if err != nil {
		t.Fatal(err)
	}
	if got != 27003 {
		t.Fatalf("want 27003, got %d", got)
	}
}

func TestAllocatePortExhausted(t *testing.T) {
	used := map[int]bool{27001: true, 27002: true}
	_, err := AllocatePort(27001, 27002, used)
	if err == nil {
		t.Fatal("want exhaustion error")
	}
}

func nodes(pairs ...[2]string) []model.Node {
	out := make([]model.Node, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, model.Node{ID: p[0], Name: p[1]})
	}
	return out
}

func TestResolveExplicitNameCorrection(t *testing.T) {
	pool := nodes([2]string{"id1", "美国 1 (renamed)"})
	m := &model.Mapping{Strategy: model.StrategySingle, Nodes: []model.NodeRef{{ID: "id1", Name: "stale name"}}}
	res := Resolve(m, pool)
	if res.AutoDisable {
		t.Fatal("should not auto-disable when node present")
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Name != "美国 1 (renamed)" {
		t.Fatalf("name should be corrected from pool, got %+v", res.Nodes)
	}
}

func TestResolveSingleMissingAutoDisables(t *testing.T) {
	pool := nodes([2]string{"other", "别的"})
	m := &model.Mapping{Strategy: model.StrategySingle, Nodes: []model.NodeRef{{ID: "gone", Name: "美国 1"}}}
	res := Resolve(m, pool)
	if !res.AutoDisable {
		t.Fatal("single with missing node should auto-disable")
	}
	if res.Reason == "" {
		t.Error("expected a reason")
	}
}

func TestResolveGroupShrinks(t *testing.T) {
	pool := nodes([2]string{"id1", "美国 1"}) // id2 missing
	m := &model.Mapping{Strategy: model.StrategyFailover, Nodes: []model.NodeRef{{ID: "id1", Name: "美国 1"}, {ID: "id2", Name: "美国 2"}}}
	res := Resolve(m, pool)
	if res.AutoDisable {
		t.Fatal("group should keep serving with remaining node")
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("group should shrink to 1 node, got %d", len(res.Nodes))
	}
	if len(res.Missing) != 1 {
		t.Fatalf("should report 1 missing node, got %d", len(res.Missing))
	}
}

func TestResolveGroupEmptiedAutoDisables(t *testing.T) {
	pool := nodes([2]string{"other", "别的"})
	m := &model.Mapping{Strategy: model.StrategyFailover, Nodes: []model.NodeRef{{ID: "gone1"}, {ID: "gone2"}}}
	res := Resolve(m, pool)
	if !res.AutoDisable {
		t.Fatal("emptied group should auto-disable")
	}
}

func TestResolveFilterNewNodesIncluded(t *testing.T) {
	pool := nodes([2]string{"id1", "美国 1"}, [2]string{"id2", "美国 3"}, [2]string{"id3", "英国 1"})
	m := &model.Mapping{Strategy: model.StrategyAuto, NodeFilter: "美国|US"}
	res := Resolve(m, pool)
	if len(res.Nodes) != 2 {
		t.Fatalf("filter should match 2 US nodes, got %d", len(res.Nodes))
	}
	// Sorted by name: 美国 1 before 美国 3.
	if res.Nodes[0].Name != "美国 1" || res.Nodes[1].Name != "美国 3" {
		t.Errorf("filter results should be name-sorted, got %+v", res.Nodes)
	}
}

func TestResolveFilterZeroMatchAutoDisables(t *testing.T) {
	pool := nodes([2]string{"id1", "英国 1"})
	m := &model.Mapping{Strategy: model.StrategyAuto, NodeFilter: "美国"}
	res := Resolve(m, pool)
	if !res.AutoDisable {
		t.Fatal("filter with no matches should auto-disable")
	}
}
