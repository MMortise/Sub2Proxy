// Package config loads, validates, and atomically persists config.yaml — the
// single source of truth for static settings, subscriptions, manual nodes, and
// mappings (design D6).
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/wuxi/sub2proxy/internal/fsutil"
	"github.com/wuxi/sub2proxy/internal/model"
	"gopkg.in/yaml.v3"
)

// Defaults (design appendix B).
const (
	DefaultListen  = "0.0.0.0:27000"
	DefaultDataDir = "/data"
	DefaultPortLo  = 27001
	DefaultPortHi  = 27999
	FileMode       = 0o600
)

// Config is the full parsed config.yaml.
type Config struct {
	Listen        string               `yaml:"listen"`
	AuthKey       string               `yaml:"auth_key"`
	PortRange     [2]int               `yaml:"port_range"`
	DataDir       string               `yaml:"data_dir"`
	Subscriptions []model.Subscription `yaml:"subscriptions"`
	ManualNodes   []string             `yaml:"manual_nodes"`
	Mappings      []model.Mapping      `yaml:"mappings"`

	path string // where this config was loaded from; used by Save
}

// PortLo and PortHi expose the mapping port range bounds.
func (c *Config) PortLo() int { return c.PortRange[0] }
func (c *Config) PortHi() int { return c.PortRange[1] }
func (c *Config) Path() string { return c.path }

// applyDefaults fills zero-valued optional fields.
func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	if c.DataDir == "" {
		c.DataDir = DefaultDataDir
	}
	if c.PortRange == [2]int{} {
		c.PortRange = [2]int{DefaultPortLo, DefaultPortHi}
	}
	for i := range c.Mappings {
		m := &c.Mappings[i]
		if m.Strategy == model.StrategySingle {
			m.HealthCheck = nil
			continue
		}
		if m.HealthCheck == nil {
			m.HealthCheck = model.DefaultHealthCheck()
			continue
		}
		if m.HealthCheck.URL == "" {
			m.HealthCheck.URL = model.DefaultHealthCheckURL
		}
		if m.HealthCheck.IntervalSec == 0 {
			m.HealthCheck.IntervalSec = model.DefaultHealthInterval
		}
	}
}

// knownKeys is the set of recognized top-level config.yaml keys, for unknown-key
// warnings (forward compatibility: warn, don't fail).
var knownKeys = map[string]bool{
	"listen": true, "auth_key": true, "port_range": true, "data_dir": true,
	"subscriptions": true, "manual_nodes": true, "mappings": true,
}

// Load reads config.yaml from path. On first run (file absent) it writes a
// template with a freshly generated random auth_key and starts with it, so
// `docker compose up -d` just works — the generated key is surfaced via warn.
// If the file exists but auth_key is empty, a key is generated and persisted the
// same way. Unknown top-level keys are reported via warn. The returned config has
// defaults applied and has passed validation.
func Load(path string, warn func(string)) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key := GenerateAuthKey()
		tmpl := []byte(templateWithKey(key))
		if werr := os.WriteFile(path, tmpl, FileMode); werr != nil {
			return nil, fmt.Errorf("write config template: %w", werr)
		}
		warnf(warn, "first run: generated config.yaml with a random auth_key: %s  (change it in %s to set your own)", key, path)
		data = tmpl
	} else if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.path = path

	if warn != nil {
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil {
			for k := range raw {
				if !knownKeys[k] {
					warn(fmt.Sprintf("unknown config key %q ignored", k))
				}
			}
		}
	}

	// An empty auth_key (e.g. a hand-created or template file the user never
	// edited) gets a generated key persisted back, so the app never crash-loops
	// waiting for manual setup.
	if cfg.AuthKey == "" {
		cfg.AuthKey = GenerateAuthKey()
		if werr := fsutil.AtomicWrite(path, mustMarshal(&cfg), FileMode); werr != nil {
			return nil, fmt.Errorf("persist generated auth_key: %w", werr)
		}
		warnf(warn, "auth_key was empty; generated one: %s  (change it in %s to set your own)", cfg.AuthKey, path)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GenerateAuthKey returns a random URL-safe key (~24 chars) for first-run setup.
func GenerateAuthKey() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is catastrophic; fall back to a fixed-length marker
		// that still passes the >=8 check so startup isn't blocked.
		return "changeme-please-set-a-key"
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func mustMarshal(c *Config) []byte {
	out, _ := yaml.Marshal(c)
	return out
}

func warnf(warn func(string), format string, args ...any) {
	if warn != nil {
		warn(fmt.Sprintf(format, args...))
	}
}

func templateWithKey(key string) string {
	return fmt.Sprintf(templateYAML, key)
}

const templateYAML = `# sub2proxy configuration. See README for full field docs.
listen: 0.0.0.0:27000          # Web UI / API listen address
auth_key: "%s"                 # login key for UI and API (>= 8 chars). Auto-generated on first run; change to set your own.
port_range: [27001, 27999]     # mapping port allocation range (match compose published range)
data_dir: /data                # derived data (node cache) directory

subscriptions: []              # added via the web UI
manual_nodes: []               # raw share links added manually
mappings: []                   # port -> strategy over nodes, added via the web UI
`
