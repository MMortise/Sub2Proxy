package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wuxi/sub2proxy/internal/config"
	"github.com/wuxi/sub2proxy/internal/core"
)

const testKey = "supersecretkey"

func newTestServer(t *testing.T) (*Server, *core.App) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("auth_key: "+testKey+"\ndata_dir: "+dir+"\n"), 0o600)
	cfg, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir = dir
	app := core.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(app.Shutdown)
	return NewServer(app, testKey, nil), app
}

// do issues a request with Bearer auth unless noAuth is set.
func do(t *testing.T, h http.Handler, method, target string, body any, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	if auth {
		req.Header.Set("Authorization", "Bearer "+testKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthNoAuth(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), "GET", "/api/health", nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d", rec.Code)
	}
}

func TestProtectedRequiresAuth(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), "GET", "/api/mappings", nil, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestBearerAuthWorks(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), "GET", "/api/mappings", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer should authorize, got %d", rec.Code)
	}
}

// The per-IP lockout must also cover the bearer-auth path, or the key could be
// brute-forced through the Authorization header without ever hitting /api/login.
func TestBearerBruteForceLocksOut(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	bearer := func(key string) int {
		req := httptest.NewRequest("GET", "/api/mappings", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 1; i < failThreshold; i++ {
		if code := bearer("nope"); code != http.StatusUnauthorized {
			t.Fatalf("wrong bearer #%d: want 401, got %d", i, code)
		}
	}
	if code := bearer("nope"); code != http.StatusTooManyRequests {
		t.Fatalf("threshold wrong bearer: want 429, got %d", code)
	}
	// Even the correct key is refused while locked, so guessing can't slip through.
	if code := bearer(testKey); code != http.StatusTooManyRequests {
		t.Fatalf("correct bearer during lock: want 429, got %d", code)
	}
}

// A logged-in session cookie authorizes even while the key lockout is active, so
// an attacker sharing a client's IP can't lock the real user out of the UI.
func TestCookieBypassesLockout(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	rec := do(t, h, "POST", "/api/login", map[string]string{"key": testKey}, false)
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected session cookie")
	}
	for i := 0; i < failThreshold; i++ {
		req := httptest.NewRequest("GET", "/api/mappings", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest("GET", "/api/mappings", nil)
	req.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("cookie during lock: want 200, got %d", rec2.Code)
	}
}

func TestLoginFlow(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()

	// Wrong key.
	rec := do(t, h, "POST", "/api/login", map[string]string{"key": "wrong"}, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key should be 401, got %d", rec.Code)
	}

	// Correct key sets a session cookie.
	rec = do(t, h, "POST", "/api/login", map[string]string{"key": testKey}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct key should be 200, got %d", rec.Code)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly {
		t.Fatal("expected HttpOnly session cookie")
	}

	// Cookie authorizes a protected request.
	req := httptest.NewRequest("GET", "/api/mappings", nil)
	req.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("session cookie should authorize, got %d", rec2.Code)
	}
}

func TestSessionExpiry(t *testing.T) {
	s, _ := newTestServer(t)
	// Force a short TTL and create an already-expired session.
	s.sessions.ttl = time.Millisecond
	token := s.sessions.create()
	time.Sleep(5 * time.Millisecond)
	if s.sessions.valid(token) {
		t.Fatal("expired session should be invalid")
	}
}

func TestLoginLimiterLocksOut(t *testing.T) {
	l := newLoginLimiter()
	ip := "1.2.3.4"
	now := time.Now()

	// First failThreshold-1 fails return decreasing remaining, no lock.
	for i := 1; i < failThreshold; i++ {
		remaining, locked := l.recordFail(ip, now)
		if locked != 0 {
			t.Fatalf("should not lock before %d fails", failThreshold)
		}
		if remaining != failThreshold-i {
			t.Fatalf("attempt %d: remaining = %d, want %d", i, remaining, failThreshold-i)
		}
	}
	// The threshold-th fail locks out.
	remaining, locked := l.recordFail(ip, now)
	if locked != lockDuration || remaining != 0 {
		t.Fatalf("threshold fail should lock for %v, got locked=%v remaining=%d", lockDuration, locked, remaining)
	}
	if d := l.lockRemaining(ip, now); d <= 0 || d > lockDuration {
		t.Fatalf("lockRemaining = %v, want ~%v", d, lockDuration)
	}
	// After the lock expires, the counter resets.
	if d := l.lockRemaining(ip, now.Add(lockDuration+time.Second)); d != 0 {
		t.Fatalf("lock should have expired, got %v", d)
	}
	l.reset(ip)
	if l.lockRemaining(ip, now) != 0 {
		t.Fatal("reset should clear the lock")
	}
}

func TestLoginLockoutHandler(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	for i := 0; i < failThreshold; i++ {
		do(t, h, "POST", "/api/login", map[string]string{"key": "wrong"}, false)
	}
	// Now locked: even the correct key is rejected with 429.
	rec := do(t, h, "POST", "/api/login", map[string]string{"key": testKey}, false)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 during lockout, got %d: %s", rec.Code, rec.Body)
	}
	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == "" {
		t.Error("lockout response should carry an error message")
	}
}

func TestLoginWrongKeyShowsRemaining(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), "POST", "/api/login", map[string]string{"key": "wrong"}, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "还可尝试") {
		t.Errorf("wrong-key error should show remaining attempts, got %q", body["error"])
	}
}

func TestSubscriptionCRUD(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxies:\n  - {name: N1, type: ss, server: 1.1.1.1, port: 8388, cipher: aes-256-gcm, password: secret}\n"))
	}))
	defer upstream.Close()

	rec := do(t, h, "POST", "/api/subscriptions", map[string]string{"name": "air", "url": upstream.URL}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create subscription = %d: %s", rec.Code, rec.Body)
	}
	var created struct {
		Subscription struct{ ID string } `json:"subscription"`
		NodeCount    int                 `json:"node_count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.NodeCount != 1 {
		t.Fatalf("want 1 node, got %d", created.NodeCount)
	}

	// Duplicate URL -> 409.
	rec = do(t, h, "POST", "/api/subscriptions", map[string]string{"name": "dup", "url": upstream.URL}, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate URL should be 409, got %d", rec.Code)
	}

	// Refresh.
	rec = do(t, h, "POST", "/api/subscriptions/"+created.Subscription.ID+"/refresh", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d", rec.Code)
	}

	// Delete.
	rec = do(t, h, "DELETE", "/api/subscriptions/"+created.Subscription.ID, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
}

func TestNodeCRUD(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()

	// Add manual node.
	link := "ss://YWVzLTI1Ni1nY206c2VjcmV0@1.2.3.4:8388#我的节点"
	rec := do(t, h, "POST", "/api/nodes", map[string]string{"link": link}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("add node = %d: %s", rec.Code, rec.Body)
	}
	var node struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &node)

	// List with filter.
	rec = do(t, h, "GET", "/api/nodes?q=我的", nil, true)
	var nodes []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &nodes)
	if len(nodes) != 1 {
		t.Fatalf("filter should return 1 node, got %d", len(nodes))
	}

	// Bad link -> 400.
	rec = do(t, h, "POST", "/api/nodes", map[string]string{"link": "garbage"}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad link should be 400, got %d", rec.Code)
	}

	// Delete.
	rec = do(t, h, "DELETE", "/api/nodes/"+node.ID, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete node = %d", rec.Code)
	}
}

func TestMappingCRUDAndNoSideEffectOnFailure(t *testing.T) {
	s, app := newTestServer(t)
	h := s.Handler()

	// Add a node to reference.
	link := "ss://YWVzLTI1Ni1nY206c2VjcmV0@1.2.3.4:8388#US-1"
	rec := do(t, h, "POST", "/api/nodes", map[string]string{"link": link}, true)
	var node struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(rec.Body.Bytes(), &node)

	// Create with auto-allocated port.
	rec = do(t, h, "POST", "/api/mappings", map[string]any{
		"name": "us", "strategy": "single", "nodes": []map[string]string{{"id": node.ID, "name": node.Name}},
	}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create mapping = %d: %s", rec.Code, rec.Body)
	}
	var m struct{ Port int }
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m.Port != 27100 {
		t.Fatalf("want auto-allocated 27100, got %d", m.Port)
	}

	// Out-of-range port -> 400, and NO side effect (still 1 mapping).
	rec = do(t, h, "POST", "/api/mappings", map[string]any{
		"port": 80, "name": "bad", "strategy": "single", "nodes": []map[string]string{{"id": node.ID}},
	}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range should be 400, got %d", rec.Code)
	}
	if len(app.Mappings()) != 1 {
		t.Fatalf("failed create must not add a mapping, have %d", len(app.Mappings()))
	}

	// Disable then check it reflects.
	rec = do(t, h, "POST", "/api/mappings/27100/disable", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d", rec.Code)
	}
	if app.Mappings()[0].Enabled {
		t.Error("mapping should be disabled")
	}

	// Update (rename).
	rec = do(t, h, "PUT", "/api/mappings/27100", map[string]any{
		"port": 27100, "name": "us-renamed", "strategy": "single",
		"nodes": []map[string]string{{"id": node.ID, "name": node.Name}}, "enabled": true,
	}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body)
	}

	// Delete.
	rec = do(t, h, "DELETE", "/api/mappings/27100", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if len(app.Mappings()) != 0 {
		t.Fatal("mapping should be gone")
	}
}

func TestStatusEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), "GET", "/api/status", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var st map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("status body not json: %v", err)
	}
}

func TestStaticPlaceholder(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), "GET", "/", nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("root = %d", rec.Code)
	}
	// Unknown API path -> 404 JSON.
	rec = do(t, s.Handler(), "GET", "/api/bogus", nil, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api path should be 404, got %d", rec.Code)
	}
}
