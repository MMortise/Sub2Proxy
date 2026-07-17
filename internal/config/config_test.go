package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wuxi/sub2proxy/internal/model"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	path := writeConfig(t, "auth_key: supersecret\n")
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("listen default = %q", cfg.Listen)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("data_dir default = %q", cfg.DataDir)
	}
	if cfg.PortRange != [2]int{DefaultPortLo, DefaultPortHi} {
		t.Errorf("port_range default = %v", cfg.PortRange)
	}
}

func TestLoadMissingGeneratesKeyAndStarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	var warnings []string
	cfg, err := Load(path, func(w string) { warnings = append(warnings, w) })
	if err != nil {
		t.Fatalf("first run should succeed, got %v", err)
	}
	if len(cfg.AuthKey) < 8 {
		t.Fatalf("first run should generate an auth_key, got %q", cfg.AuthKey)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("template not written: %v", statErr)
	}
	if info.Mode().Perm() != FileMode {
		t.Errorf("template mode = %v, want %v", info.Mode().Perm(), os.FileMode(FileMode))
	}
	// The generated key is surfaced and persisted for a stable restart.
	found := false
	for _, w := range warnings {
		if strings.Contains(w, cfg.AuthKey) {
			found = true
		}
	}
	if !found {
		t.Error("generated auth_key should be surfaced via warn")
	}
	cfg2, err := Load(path, nil)
	if err != nil || cfg2.AuthKey != cfg.AuthKey {
		t.Errorf("restart should reuse the persisted key: %v / %q vs %q", err, cfg2.AuthKey, cfg.AuthKey)
	}
}

func TestLoadEmptyKeyGenerates(t *testing.T) {
	path := writeConfig(t, "auth_key: \"\"\ndata_dir: /tmp\n")
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("empty key should be auto-filled, got %v", err)
	}
	if len(cfg.AuthKey) < 8 {
		t.Fatalf("empty auth_key should be regenerated, got %q", cfg.AuthKey)
	}
}

func TestLoadUnknownFieldWarns(t *testing.T) {
	path := writeConfig(t, "auth_key: supersecret\nfuture_option: x\n")
	var warnings []string
	cfg, err := Load(path, func(w string) { warnings = append(warnings, w) })
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("config should load despite unknown field")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "future_option") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning for future_option, got %v", warnings)
	}
}

func TestValidate(t *testing.T) {
	base := func() *Config {
		return &Config{
			Listen:    "0.0.0.0:27000",
			AuthKey:   "supersecret",
			PortRange: [2]int{27001, 27999},
			DataDir:   "/data",
		}
	}
	single := model.Mapping{Port: 27001, Name: "us", Strategy: model.StrategySingle,
		Nodes: []model.NodeRef{{ID: "a", Name: "US 1"}}, Enabled: true}

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; "" means expect success
	}{
		{"ok single", func(c *Config) { c.Mappings = []model.Mapping{single} }, ""},
		{"short key", func(c *Config) { c.AuthKey = "short" }, "auth_key"},
		{"range includes listen", func(c *Config) { c.PortRange = [2]int{26999, 27001} }, "listen port"},
		{"range inverted", func(c *Config) { c.PortRange = [2]int{27999, 27001} }, "port_range"},
		{"port out of range", func(c *Config) {
			m := single
			m.Port = 8080
			c.Mappings = []model.Mapping{m}
		}, "out of range"},
		{"port equals listen", func(c *Config) {
			m := single
			m.Port = 27000
			c.Mappings = []model.Mapping{m}
		}, "out of range"}, // 27000 is outside [27001,27999] so caught by range first
		{"duplicate ports", func(c *Config) {
			m2 := single
			m2.Name = "uk"
			c.Mappings = []model.Mapping{single, m2}
		}, "port 27001 used by both"},
		{"bad strategy", func(c *Config) {
			m := single
			m.Strategy = "bogus"
			c.Mappings = []model.Mapping{m}
		}, "strategy"},
		{"nodes and filter", func(c *Config) {
			m := single
			m.Strategy = model.StrategyFailover
			m.NodeFilter = "US"
			c.Mappings = []model.Mapping{m}
		}, "mutually exclusive"},
		{"neither nodes nor filter", func(c *Config) {
			m := single
			m.Strategy = model.StrategyFailover
			m.Nodes = nil
			c.Mappings = []model.Mapping{m}
		}, "one of nodes or node_filter"},
		{"bad regex", func(c *Config) {
			m := single
			m.Strategy = model.StrategyAuto
			m.Nodes = nil
			m.NodeFilter = "美国("
			c.Mappings = []model.Mapping{m}
		}, "node_filter"},
		{"single two nodes", func(c *Config) {
			m := single
			m.Nodes = []model.NodeRef{{ID: "a"}, {ID: "b"}}
			c.Mappings = []model.Mapping{m}
		}, "exactly one"},
		{"health interval too small", func(c *Config) {
			m := single
			m.Strategy = model.StrategyFailover
			m.HealthCheck = &model.HealthCheck{URL: "http://x", IntervalSec: 10}
			c.Mappings = []model.Mapping{m}
		}, "health_check.interval"},
		{"refresh interval too long", func(c *Config) {
			c.Subscriptions = []model.Subscription{{Name: "a", URL: "http://x", RefreshInterval: "48h"}}
		}, "refresh_interval"},
		{"refresh interval ok", func(c *Config) {
			c.Subscriptions = []model.Subscription{{Name: "a", URL: "http://x", RefreshInterval: "6h"}}
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want success, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestStoreDebounceCollapses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	var mu sync.Mutex
	key := "supersecret1"
	snap := func() *Config {
		mu.Lock()
		defer mu.Unlock()
		return &Config{Listen: DefaultListen, AuthKey: key, PortRange: [2]int{27001, 27999}, DataDir: "/data"}
	}
	s := NewStore(path, snap)
	s.debounce = 50 * time.Millisecond

	// Ten rapid mutations should collapse into (at most a couple of) writes,
	// and the final content must reflect the latest snapshot.
	for i := 0; i < 10; i++ {
		mu.Lock()
		key = "supersecret" + string(rune('0'+i))
		mu.Unlock()
		s.Schedule()
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected a persisted file: %v", err)
	}
	if !strings.Contains(string(got), "supersecret9") {
		t.Fatalf("final write should carry latest snapshot, got %q", got)
	}
}

func TestStoreFlushWritesImmediately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	snap := func() *Config {
		return &Config{Listen: DefaultListen, AuthKey: "supersecret", PortRange: [2]int{27001, 27999}, DataDir: "/data"}
	}
	s := NewStore(path, snap)
	s.Schedule() // a pending mutation…
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("flush should have written the file after a mutation: %v", err)
	}
}

// A Flush with no prior mutation must not touch the file, so a manual edit to
// config.yaml made while the app is running isn't clobbered by the shutdown write.
func TestStoreFlushCleanDoesNotClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	external := "auth_key: hand-edited-key\n"
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := func() *Config {
		return &Config{Listen: DefaultListen, AuthKey: "in-memory-key", PortRange: [2]int{27001, 27999}, DataDir: "/data"}
	}
	s := NewStore(path, snap)
	if err := s.Flush(); err != nil { // no Schedule -> clean -> must be a no-op
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != external {
		t.Fatalf("clean flush must not overwrite an external edit; got %q", got)
	}
}
