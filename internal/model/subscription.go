package model

import (
	"fmt"
	"time"
)

// Subscription is one airport subscription. yaml-tagged fields persist to
// config.yaml; runtime-only fields (populated after a fetch) carry json tags
// only and are excluded from persistence via yaml:"-".
type Subscription struct {
	ID              string `yaml:"id" json:"id"`
	Name            string `yaml:"name" json:"name"`
	URL             string `yaml:"url" json:"url"`
	UserAgent       string `yaml:"user_agent,omitempty" json:"user_agent,omitempty"`
	RefreshInterval string `yaml:"refresh_interval,omitempty" json:"refresh_interval,omitempty"` // e.g. "6h"

	// Runtime-only fields.
	NodeCount   int       `yaml:"-" json:"node_count"`
	LastRefresh time.Time `yaml:"-" json:"last_refresh,omitempty"`
	LastError   string    `yaml:"-" json:"last_error,omitempty"`
	Quota       *Quota    `yaml:"-" json:"quota,omitempty"`
}

// Refresh interval default and bounds (design D3 / subscription-management spec).
const (
	DefaultRefreshInterval = 6 * time.Hour
	MinRefreshInterval     = 5 * time.Minute
	MaxRefreshInterval     = 24 * time.Hour
)

// UserAgentOrDefault returns the configured UA or the clash.meta default.
func (s Subscription) UserAgentOrDefault() string {
	if s.UserAgent == "" {
		return "clash.meta"
	}
	return s.UserAgent
}

// Interval parses RefreshInterval, returning the default when empty. A parse
// error is returned to the caller for validation.
func (s Subscription) Interval() (time.Duration, error) {
	if s.RefreshInterval == "" {
		return DefaultRefreshInterval, nil
	}
	return time.ParseDuration(s.RefreshInterval)
}

// ValidateRefreshInterval checks a refresh-interval string. Empty is allowed (the
// default is used); otherwise it must parse and fall within Min–Max bounds. It is
// the single rule shared by config validation and the API input check.
func ValidateRefreshInterval(interval string) error {
	if interval == "" {
		return nil
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		return err
	}
	if d < MinRefreshInterval || d > MaxRefreshInterval {
		return fmt.Errorf("must be within 5m–24h, got %s", d)
	}
	return nil
}

// Quota holds parsed subscription-userinfo header values. Fields are 0 when the
// corresponding header field is absent.
type Quota struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"` // Unix seconds
}
