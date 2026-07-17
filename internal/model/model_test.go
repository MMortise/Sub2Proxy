package model

import "testing"

func TestFingerprintIgnoresName(t *testing.T) {
	a := map[string]any{"name": "美国 1", "type": "vless", "server": "a.com", "port": 443, "uuid": "x"}
	b := map[string]any{"name": "US-01", "type": "vless", "server": "a.com", "port": 443, "uuid": "x"}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("fingerprint should ignore name")
	}
}

func TestFingerprintKeyOrderInvariant(t *testing.T) {
	a := map[string]any{"type": "vless", "server": "a.com", "port": 443, "uuid": "x"}
	b := map[string]any{"uuid": "x", "port": 443, "server": "a.com", "type": "vless"}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("fingerprint should be invariant to map key insertion order")
	}
}

func TestFingerprintParamChangeDiffers(t *testing.T) {
	a := map[string]any{"type": "vless", "server": "a.com", "port": 443, "uuid": "x"}
	b := map[string]any{"type": "vless", "server": "a.com", "port": 8443, "uuid": "x"}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("changing port must change fingerprint")
	}
}

func TestFingerprintNestedMap(t *testing.T) {
	a := map[string]any{"type": "vless", "ws-opts": map[string]any{"path": "/p", "headers": map[string]any{"Host": "h"}}}
	b := map[string]any{"type": "vless", "ws-opts": map[string]any{"headers": map[string]any{"Host": "h"}, "path": "/p"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("nested map key order must not affect fingerprint")
	}
}

func TestRegion(t *testing.T) {
	cases := map[string]string{
		"🇺🇸 US-Los Angeles 01": "US",
		"日本 BGP":               "JP",
		"HK-IPLC 專線":           "HK",
		"🇬🇧 London 1":          "UK",
		"新加坡 SG 中转":            "SG",
		"Australia Sydney 01":  "AU", // must NOT match US via "us" substring
		"德国法兰克福":               "DE",
		"台北 Hinet":            "TW",
		"随便一个名字":               "",
		"Relay Direct":         "", // no region token
	}
	for name, want := range cases {
		if got := Region(name); got != want {
			t.Errorf("Region(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestStrategyValid(t *testing.T) {
	if !StrategyFailover.Valid() {
		t.Error("failover should be valid")
	}
	if Strategy("bogus").Valid() {
		t.Error("bogus should be invalid")
	}
}
