package auth

import (
	"encoding/base64"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewSessionMaterialUsesOpaqueTokenHashAndSecureCookie(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	material, err := newSessionMaterial(now)
	require.NoError(t, err)

	raw, err := base64.RawURLEncoding.DecodeString(material.Token)
	require.NoError(t, err)
	require.Len(t, raw, 32)
	require.NotEqual(t, []byte(material.Token), material.TokenHash[:])
	require.Equal(t, now.Add(24*time.Hour), material.ExpiresAt)
	require.Equal(t, now.Add(30*time.Minute), material.IdleExpiresAt)

	cookie := material.Cookie()
	require.Equal(t, sessionCookieName, cookie.Name)
	require.Equal(t, material.Token, cookie.Value)
	require.Equal(t, "/", cookie.Path)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
}

func TestLoginLimiterBlocksAfterFiveFailuresByIPOrUsername(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	limiter := NewLoginLimiter(func() time.Time { return now })
	for range 5 {
		require.False(t, limiter.Blocked("192.0.2.1", "admin"))
		limiter.Failed("192.0.2.1", "admin")
	}
	require.True(t, limiter.Blocked("192.0.2.1", "someone-else"))
	require.True(t, limiter.Blocked("198.51.100.2", "admin"))

	now = now.Add(15*time.Minute + time.Second)
	require.False(t, limiter.Blocked("192.0.2.1", "admin"))
}

func TestLoginLimiterIsConcurrencySafe(t *testing.T) {
	limiter := NewLoginLimiter(time.Now)
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			limiter.Failed("192.0.2.1", "admin")
		}()
	}
	workers.Wait()
	require.True(t, limiter.Blocked("192.0.2.1", "admin"))
}
