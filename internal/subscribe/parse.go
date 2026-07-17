// Package subscribe fetches airport subscriptions and parses them into Clash
// proxy maps, reusing mihomo's converters so no protocol dialect is parsed here
// (design D3).
package subscribe

import (
	"fmt"
	"strings"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/common/convert"
	"gopkg.in/yaml.v3"

	"github.com/wuxi/sub2proxy/internal/model"
)

// Parse turns a subscription response body into validated Clash proxy maps.
// It tries Clash YAML first, then base64 / raw share links (convert.ConvertsV2Ray
// handles base64 decoding of every common variant internally). Each candidate is
// validated with adapter.ParseProxy; unsupported entries are skipped with a
// warning. When neither format yields proxies, it returns an error carrying a
// snippet of the response.
func Parse(body []byte) (proxies []map[string]any, warnings []string, err error) {
	raw, perr := extractRaw(body)
	if perr != nil {
		return nil, nil, perr
	}
	for _, m := range raw {
		if _, perr := adapter.ParseProxy(model.CloneProxy(m)); perr != nil {
			warnings = append(warnings, fmt.Sprintf("skipped unsupported node %v: %v", nameOf(m), perr))
			continue
		}
		proxies = append(proxies, m)
	}
	if len(proxies) == 0 {
		return nil, warnings, fmt.Errorf("no usable nodes in subscription: %s", snippet(body))
	}
	return proxies, warnings, nil
}

// extractRaw pulls the candidate proxy maps out of a response, format-agnostic.
func extractRaw(body []byte) ([]map[string]any, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &doc); err == nil && len(doc.Proxies) > 0 {
		return doc.Proxies, nil
	}
	if maps, err := convert.ConvertsV2Ray(body); err == nil && len(maps) > 0 {
		return maps, nil
	}
	return nil, fmt.Errorf("unrecognized subscription format: %s", snippet(body))
}

func nameOf(m map[string]any) any {
	if n, ok := m["name"]; ok {
		return n
	}
	return "<unnamed>"
}

// snippet returns the first 200 chars of body with newlines collapsed, for error
// messages (subscription-management spec).
func snippet(body []byte) string {
	s := strings.ReplaceAll(string(body), "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
