package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fingerprint computes a node's stable identity from its Clash proxy map:
// drop the "name" key, then SHA-256 the compact JSON encoding. encoding/json
// marshals map keys in sorted order at every level, so the result is invariant
// to key ordering (design D5). Returns hex-lowercase sha256.
func Fingerprint(proxy map[string]any) string {
	cleaned := make(map[string]any, len(proxy))
	for k, v := range proxy {
		if k == "name" {
			continue
		}
		cleaned[k] = v
	}
	// json.Marshal on map[string]any sorts keys recursively and emits no
	// whitespace, giving the canonical form we hash.
	b, err := json.Marshal(cleaned)
	if err != nil {
		// A proxy map that came from YAML/mihomo convert is always JSON-encodable;
		// fall back to a best-effort key dump so we never panic.
		b = []byte(bestEffort(cleaned))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ShortID returns the first 8 chars of a fingerprint for display.
func ShortID(fingerprint string) string {
	if len(fingerprint) <= 8 {
		return fingerprint
	}
	return fingerprint[:8]
}

func bestEffort(m map[string]any) string {
	out := ""
	for k, v := range m {
		out += k
		if s, ok := v.(string); ok {
			out += "=" + s
		}
		out += ";"
	}
	return out
}
