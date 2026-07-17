package subscribe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wuxi/sub2proxy/internal/model"
)

// Fetch limits (design D3).
const (
	FetchTimeout   = 15 * time.Second
	MaxBodyBytes   = 16 << 20 // 16 MiB
	MaxRedirects   = 5
	userInfoHeader = "subscription-userinfo"
)

// Result is the outcome of fetching and parsing one subscription.
type Result struct {
	Proxies  []map[string]any
	Quota    *model.Quota // nil when the subscription-userinfo header is absent
	Warnings []string
}

// Fetcher retrieves and parses subscriptions. It holds a single http.Client so
// connections are reused across refreshes.
type Fetcher struct {
	client *http.Client
}

// NewFetcher builds a Fetcher with the fetch timeout and redirect cap applied.
func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: FetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= MaxRedirects {
					return fmt.Errorf("stopped after %d redirects", MaxRedirects)
				}
				return nil
			},
		},
	}
}

// Fetch retrieves the subscription, enforces the size cap, parses both formats,
// and extracts quota info. Network, size, and parse failures all return an error
// so the caller can preserve the previous node set.
func (f *Fetcher) Fetch(ctx context.Context, sub model.Subscription) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", sub.UserAgentOrDefault())

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: unexpected status %s", resp.Status)
	}

	// Read one extra byte past the cap to detect oversize bodies.
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("subscription body exceeds %d bytes", MaxBodyBytes)
	}

	proxies, warnings, err := Parse(body)
	if err != nil {
		return nil, err
	}
	return &Result{
		Proxies:  proxies,
		Quota:    parseUserInfo(resp.Header.Get(userInfoHeader)),
		Warnings: warnings,
	}, nil
}

// parseUserInfo parses a subscription-userinfo header value like
// "upload=1; download=2; total=3; expire=4". Returns nil when the header is
// empty; missing individual fields stay 0 (subscription-management spec).
func parseUserInfo(header string) *model.Quota {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	q := &model.Quota{}
	for _, part := range strings.Split(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "upload":
			q.Upload = n
		case "download":
			q.Download = n
		case "total":
			q.Total = n
		case "expire":
			q.Expire = n
		}
	}
	return q
}
