package subscribe

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wuxi/sub2proxy/internal/model"
)

const (
	vlessLink  = "vless://b831381d-6324-4d53-ad4f-8cda48b30811@example.com:443?encryption=none&security=tls&sni=example.com&type=ws&host=example.com&path=%2Fp#US-1"
	trojanLink = "trojan://pass123@example.org:8443?sni=example.org#UK-1"
	ssLink     = "ss://YWVzLTI1Ni1nY206c2VjcmV0@1.2.3.4:8388#SS-1"
)

func TestParseYAMLPath(t *testing.T) {
	body := `
proxies:
  - {name: US-1, type: vless, server: example.com, port: 443, uuid: b831381d-6324-4d53-ad4f-8cda48b30811, network: ws, tls: true, servername: example.com, ws-opts: {path: /p}}
  - {name: SS-1, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-256-gcm, password: secret}
`
	proxies, warns, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 2 {
		t.Fatalf("want 2 proxies, got %d (warns %v)", len(proxies), warns)
	}
}

func TestParseBase64AndRawEquivalent(t *testing.T) {
	links := strings.Join([]string{vlessLink, trojanLink, ssLink}, "\n")
	rawProxies, _, err := Parse([]byte(links))
	if err != nil {
		t.Fatalf("raw links: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(links))
	b64Proxies, _, err := Parse([]byte(b64))
	if err != nil {
		t.Fatalf("base64 links: %v", err)
	}
	if len(rawProxies) != len(b64Proxies) || len(rawProxies) != 3 {
		t.Fatalf("raw=%d base64=%d, want 3 each", len(rawProxies), len(b64Proxies))
	}
	// Base64 and raw paths must yield identical core identity per node.
	for i := range rawProxies {
		for _, k := range []string{"type", "server", "port"} {
			if toStr(rawProxies[i][k]) != toStr(b64Proxies[i][k]) {
				t.Errorf("node %d field %s differs: raw=%v base64=%v", i, k, rawProxies[i][k], b64Proxies[i][k])
			}
		}
	}
}

func TestParseVlessCoreFields(t *testing.T) {
	proxies, _, err := Parse([]byte(vlessLink))
	if err != nil {
		t.Fatal(err)
	}
	p := proxies[0]
	if toStr(p["type"]) != "vless" || toStr(p["server"]) != "example.com" || toStr(p["port"]) != "443" {
		t.Fatalf("unexpected vless fields: %v", p)
	}
}

func TestParseHTMLErrorPage(t *testing.T) {
	html := "<!DOCTYPE html><html><body>403 Forbidden: token invalid</body></html>"
	_, _, err := Parse([]byte(html))
	if err == nil {
		t.Fatal("HTML page should not parse")
	}
	if !strings.Contains(err.Error(), "403 Forbidden") {
		t.Errorf("error should include response snippet, got %q", err.Error())
	}
}

func TestParseSkipsUnsupported(t *testing.T) {
	body := `
proxies:
  - {name: good, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-256-gcm, password: secret}
  - {name: bad, type: totally-unknown-proto, server: x, port: 1}
`
	proxies, warns, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 1 {
		t.Fatalf("want 1 usable proxy, got %d", len(proxies))
	}
	if len(warns) == 0 {
		t.Error("expected a warning for the unsupported node")
	}
}

func TestFetchNormalWithQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "clash.meta" {
			t.Errorf("unexpected UA %q", ua)
		}
		w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1000; expire=1735689600")
		w.Write([]byte("proxies:\n  - {name: SS-1, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-256-gcm, password: secret}\n"))
	}))
	defer srv.Close()

	res, err := NewFetcher().Fetch(context.Background(), model.Subscription{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Proxies) != 1 {
		t.Fatalf("want 1 proxy, got %d", len(res.Proxies))
	}
	if res.Quota == nil || res.Quota.Total != 1000 || res.Quota.Upload != 100 || res.Quota.Expire != 1735689600 {
		t.Fatalf("bad quota: %+v", res.Quota)
	}
}

func TestFetchQuotaPartialAndAbsent(t *testing.T) {
	// Partial header.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "total=1000; expire=1735689600")
		w.Write([]byte(vlessLink))
	}))
	defer srv.Close()
	res, err := NewFetcher().Fetch(context.Background(), model.Subscription{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if res.Quota == nil || res.Quota.Total != 1000 || res.Quota.Upload != 0 {
		t.Fatalf("partial quota wrong: %+v", res.Quota)
	}

	// Absent header.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(vlessLink))
	}))
	defer srv2.Close()
	res2, err := NewFetcher().Fetch(context.Background(), model.Subscription{URL: srv2.URL})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Quota != nil {
		t.Fatalf("quota should be nil when header absent, got %+v", res2.Quota)
	}
}

func TestFetchOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		big := strings.Repeat("x", MaxBodyBytes+10)
		w.Write([]byte(big))
	}))
	defer srv.Close()
	_, err := NewFetcher().Fetch(context.Background(), model.Subscription{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want oversize error, got %v", err)
	}
}

func TestFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(vlessLink))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := NewFetcher().Fetch(ctx, model.Subscription{URL: srv.URL})
	if err == nil {
		t.Fatal("want timeout error")
	}
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return itoa(t)
	case int64:
		return itoa(int(t))
	case float64:
		return itoa(int(t))
	default:
		return ""
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
