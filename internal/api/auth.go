package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Auth constants (design D7 / web-console spec).
const (
	sessionCookie = "s2p_session"
	sessionTTL    = 24 * time.Hour
	failThreshold = 3               // wrong attempts before lockout
	lockDuration  = 2 * time.Minute // lockout window after threshold reached
)

// sessionStore holds active login sessions in memory (lost on restart).
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiry
	ttl      time.Duration
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{sessions: map[string]time.Time{}, ttl: ttl}
}

func (s *sessionStore) create() string {
	var b [16]byte // 128-bit token
	_, _ = rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return token
}

func (s *sessionStore) valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, token)
		return false
	}
	return true
}

func (s *sessionStore) remove(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// loginLimiter locks out a client IP after too many wrong keys: failThreshold
// consecutive failures trigger a lockDuration lock during which logins are
// rejected (web-console spec).
type loginLimiter struct {
	mu    sync.Mutex
	state map[string]*attemptState
	lock  time.Duration
}

type attemptState struct {
	fails     int
	lockUntil time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{state: map[string]*attemptState{}, lock: lockDuration}
}

// lockRemaining returns the remaining lock time for ip, or 0 if not locked.
func (l *loginLimiter) lockRemaining(ip string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state[ip]
	if s == nil {
		return 0
	}
	if now.Before(s.lockUntil) {
		return s.lockUntil.Sub(now)
	}
	return 0
}

// recordFail records a failed attempt. It returns the number of attempts left
// before lockout (0 once locked) and, when the attempt triggered a lock, the
// lock duration.
func (l *loginLimiter) recordFail(ip string, now time.Time) (remaining int, locked time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state[ip]
	if s == nil {
		s = &attemptState{}
		l.state[ip] = s
	}
	// A previously expired lock resets the counter.
	if !s.lockUntil.IsZero() && now.After(s.lockUntil) {
		s.fails = 0
		s.lockUntil = time.Time{}
	}
	s.fails++
	if s.fails >= failThreshold {
		s.lockUntil = now.Add(l.lock)
		s.fails = 0
		return 0, l.lock
	}
	return failThreshold - s.fails, 0
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.state, ip)
	l.mu.Unlock()
}

// humanizeDuration renders a lock duration in Chinese (rounded up to seconds).
func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int((d + time.Second - 1) / time.Second) // ceil to seconds
	m, s := total/60, total%60
	switch {
	case m > 0 && s > 0:
		return fmt.Sprintf("%d 分 %d 秒", m, s)
	case m > 0:
		return fmt.Sprintf("%d 分钟", m)
	default:
		return fmt.Sprintf("%d 秒", s)
	}
}

// bearerKey extracts the key from an "Authorization: Bearer <key>" header.
func bearerKey(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:], true
	}
	return "", false
}

// checkKey verifies a presented key against the auth key under the per-IP rate
// limit. It is shared by the login endpoint and the bearer-auth path so a key
// guess is throttled identically no matter which entrypoint it comes through
// (otherwise the key could be brute-forced via the API's bearer header). It
// returns the HTTP status + message to send on failure (allow=true, 0 = proceed).
func (s *Server) checkKey(ip, key string, now time.Time) (allow bool, status int, msg string) {
	if d := s.limiter.lockRemaining(ip, now); d > 0 {
		return false, http.StatusTooManyRequests, fmt.Sprintf("尝试次数过多，请 %s后重试", humanizeDuration(d))
	}
	if subtle.ConstantTimeCompare([]byte(key), []byte(s.authKey)) == 1 {
		s.limiter.reset(ip)
		return true, 0, ""
	}
	remaining, locked := s.limiter.recordFail(ip, now)
	if locked > 0 {
		return false, http.StatusTooManyRequests,
			fmt.Sprintf("密钥连续错误 %d 次，已锁定 %s", failThreshold, humanizeDuration(locked))
	}
	return false, http.StatusUnauthorized, fmt.Sprintf("密钥错误，还可尝试 %d 次", remaining)
}

// authorize decides whether a request may proceed. A valid session cookie always
// authorizes and is never rate-limited (an attacker sharing a client's IP can't
// lock the logged-in UI out); otherwise a bearer key is treated as a rate-limited
// key attempt.
func (s *Server) authorize(r *http.Request) (allow bool, status int, msg string) {
	if c, err := r.Cookie(sessionCookie); err == nil && s.sessions.valid(c.Value) {
		return true, 0, ""
	}
	if key, ok := bearerKey(r); ok {
		return s.checkKey(clientIP(r), key, time.Now())
	}
	return false, http.StatusUnauthorized, "unauthorized"
}

// requireAuth wraps a handler, rejecting unauthenticated or rate-limited requests.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if allow, status, msg := s.authorize(r); !allow {
			writeError(w, status, msg)
			return
		}
		next(w, r)
	}
}

// handleLogin authenticates by key and issues a session cookie. The key attempt is
// rate-limited per IP, shared with the bearer-auth path via checkKey.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if allow, status, msg := s.checkKey(clientIP(r), body.Key, time.Now()); !allow {
		writeError(w, status, msg)
		return
	}
	token := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLogout clears the current session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.remove(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
