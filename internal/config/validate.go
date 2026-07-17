package config

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/wuxi/sub2proxy/internal/model"
)

// Validate checks cross-field invariants. Errors name the offending field so a
// hand-edited config.yaml can be fixed (config-persistence spec). It is called
// on load and before every write-back.
func (c *Config) Validate() error {
	if len(c.AuthKey) < 8 {
		return fmt.Errorf("auth_key: must be at least 8 characters")
	}

	listenPort, err := portOf(c.Listen)
	if err != nil {
		return fmt.Errorf("listen: %v", err)
	}

	lo, hi := c.PortRange[0], c.PortRange[1]
	if lo <= 0 || hi <= 0 || lo > hi {
		return fmt.Errorf("port_range: must be [low, high] with 0 < low <= high, got [%d, %d]", lo, hi)
	}
	if listenPort >= lo && listenPort <= hi {
		return fmt.Errorf("port_range: must not include the listen port %d", listenPort)
	}

	seen := map[int]string{}
	for i := range c.Mappings {
		if err := c.validateMapping(&c.Mappings[i], lo, hi, listenPort, seen); err != nil {
			return err
		}
	}

	for i := range c.Subscriptions {
		s := &c.Subscriptions[i]
		if err := model.ValidateRefreshInterval(s.RefreshInterval); err != nil {
			return fmt.Errorf("subscriptions[%s].refresh_interval: %v", subLabel(s, i), err)
		}
	}
	return nil
}

func (c *Config) validateMapping(m *model.Mapping, lo, hi, listenPort int, seen map[int]string) error {
	label := m.Name
	if label == "" {
		label = strconv.Itoa(m.Port)
	}
	if m.Port < lo || m.Port > hi {
		return fmt.Errorf("mappings[%s].port: %d out of range [%d, %d]", label, m.Port, lo, hi)
	}
	if m.Port == listenPort {
		return fmt.Errorf("mappings[%s].port: %d conflicts with the web listen port", label, m.Port)
	}
	if other, dup := seen[m.Port]; dup {
		return fmt.Errorf("mappings: port %d used by both %q and %q", m.Port, other, label)
	}
	seen[m.Port] = label

	if !m.Strategy.Valid() {
		return fmt.Errorf("mappings[%s].strategy: %q is not one of single|failover|round-robin|hash|sticky|auto", label, m.Strategy)
	}

	hasNodes, hasFilter := len(m.Nodes) > 0, m.NodeFilter != ""
	if hasNodes && hasFilter {
		return fmt.Errorf("mappings[%s]: nodes and node_filter are mutually exclusive", label)
	}
	if !hasNodes && !hasFilter {
		return fmt.Errorf("mappings[%s]: one of nodes or node_filter is required", label)
	}
	if hasFilter {
		if _, err := regexp.Compile(m.NodeFilter); err != nil {
			return fmt.Errorf("mappings[%s].node_filter: invalid regexp: %v", label, err)
		}
	}
	if m.Strategy == model.StrategySingle {
		if hasFilter || len(m.Nodes) != 1 {
			return fmt.Errorf("mappings[%s]: single strategy requires exactly one explicit node", label)
		}
	}
	if m.Strategy != model.StrategySingle && m.HealthCheck != nil {
		iv := m.HealthCheck.IntervalSec
		if iv != 0 && (iv < model.MinHealthInterval || iv > model.MaxHealthInterval) {
			return fmt.Errorf("mappings[%s].health_check.interval: must be within %d–%d seconds, got %d",
				label, model.MinHealthInterval, model.MaxHealthInterval, iv)
		}
	}
	if err := validateInboundAuth(m, label); err != nil {
		return err
	}
	return nil
}

// usernameRe restricts inbound-auth usernames to letters, digits, _ and -.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// validateInboundAuth checks optional inbound proxy credentials: both fields must
// be set together, username matches usernameRe, and password is at most 32 chars.
func validateInboundAuth(m *model.Mapping, label string) error {
	if m.Username == "" && m.Password == "" {
		return nil
	}
	if m.Username == "" || m.Password == "" {
		return fmt.Errorf("mappings[%s]: username and password must be set together", label)
	}
	if !usernameRe.MatchString(m.Username) {
		return fmt.Errorf("mappings[%s].username: only letters/digits/_/- allowed, up to 32 chars", label)
	}
	if utf8.RuneCountInString(m.Password) > 32 {
		return fmt.Errorf("mappings[%s].password: must be at most 32 characters", label)
	}
	return nil
}

func portOf(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("not a valid host:port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("port not numeric: %v", err)
	}
	return port, nil
}

func subLabel(s *model.Subscription, i int) string {
	if s.Name != "" {
		return s.Name
	}
	return strconv.Itoa(i)
}
