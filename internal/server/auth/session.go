package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "flowlens_session"
	idleLifetime      = 30 * time.Minute
	absoluteLifetime  = 24 * time.Hour
	loginWindow       = 15 * time.Minute
	maxLoginFailures  = 5
)

type sessionMaterial struct {
	Token         string
	TokenHash     [32]byte
	CSRFToken     string
	ExpiresAt     time.Time
	IdleExpiresAt time.Time
}

func newSessionMaterial(now time.Time) (sessionMaterial, error) {
	token, err := randomToken(32)
	if err != nil {
		return sessionMaterial{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return sessionMaterial{}, err
	}
	return sessionMaterial{
		Token:         token,
		TokenHash:     sha256.Sum256([]byte(token)),
		CSRFToken:     csrf,
		ExpiresAt:     now.Add(absoluteLifetime),
		IdleExpiresAt: now.Add(idleLifetime),
	}, nil
}

func (material sessionMaterial) Cookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    material.Token,
		Path:     "/",
		Expires:  material.ExpiresAt,
		MaxAge:   int(absoluteLifetime.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

func expiredSessionCookie() *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

type LoginLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	failures map[string][]time.Time
}

func NewLoginLimiter(now func() time.Time) *LoginLimiter {
	return &LoginLimiter{now: now, failures: make(map[string][]time.Time)}
}

func (limiter *LoginLimiter) Blocked(ip, username string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return len(limiter.current("ip:"+ip)) >= maxLoginFailures || len(limiter.current("user:"+normalizeUsername(username))) >= maxLoginFailures
}

func (limiter *LoginLimiter) Failed(ip, username string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	for _, key := range []string{"ip:" + ip, "user:" + normalizeUsername(username)} {
		limiter.failures[key] = append(limiter.current(key), now)
	}
}

func (limiter *LoginLimiter) Succeeded(ip, username string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	delete(limiter.failures, "ip:"+ip)
	delete(limiter.failures, "user:"+normalizeUsername(username))
}

func (limiter *LoginLimiter) RetryAfter(ip, username string) time.Duration {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	var oldest time.Time
	for _, key := range []string{"ip:" + ip, "user:" + normalizeUsername(username)} {
		attempts := limiter.current(key)
		if len(attempts) >= maxLoginFailures && (oldest.IsZero() || attempts[0].Before(oldest)) {
			oldest = attempts[0]
		}
	}
	if oldest.IsZero() {
		return 0
	}
	remaining := loginWindow - limiter.now().Sub(oldest)
	if remaining < time.Second {
		return time.Second
	}
	return remaining
}

func (limiter *LoginLimiter) current(key string) []time.Time {
	cutoff := limiter.now().Add(-loginWindow)
	attempts := limiter.failures[key]
	first := 0
	for first < len(attempts) && !attempts[first].After(cutoff) {
		first++
	}
	attempts = attempts[first:]
	if len(attempts) == 0 {
		delete(limiter.failures, key)
	} else {
		limiter.failures[key] = attempts
	}
	return attempts
}

func normalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
