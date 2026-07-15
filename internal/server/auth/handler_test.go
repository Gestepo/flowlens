package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBootstrapCreatesOnlyOneAdministratorAndStartsSession(t *testing.T) {
	store := newMemoryStore()
	handler := NewHandler(store, "bootstrap-secret", fixedNow)
	recorder := performJSON(handler.Bootstrap, http.MethodPost, `{"username":"admin","password":"correct horse battery staple"}`, "Bearer bootstrap-secret", nil)
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Len(t, store.admins, 1)
	require.NotEqual(t, "correct horse battery staple", store.admins[0].PasswordHash)
	match, _ := Verify("correct horse battery staple", store.admins[0].PasswordHash, DefaultParams)
	require.True(t, match)
	require.Contains(t, recorder.Header().Get("Set-Cookie"), "Secure")
	require.Len(t, store.sessions, 1)

	second := performJSON(handler.Bootstrap, http.MethodPost, `{"username":"other","password":"another correct password"}`, "Bearer bootstrap-secret", nil)
	require.Equal(t, http.StatusConflict, second.Code)
}

func TestBootstrapRejectsWrongToken(t *testing.T) {
	handler := NewHandler(newMemoryStore(), "bootstrap-secret", fixedNow)
	recorder := performJSON(handler.Bootstrap, http.MethodPost, `{"username":"admin","password":"correct horse battery staple"}`, "Bearer wrong", nil)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestLoginRateLimitsTheFifthFailure(t *testing.T) {
	store := newMemoryStore()
	hash, err := Hash("correct horse battery staple", DefaultParams)
	require.NoError(t, err)
	store.admins = append(store.admins, Admin{ID: 1, Username: "admin", PasswordHash: hash})
	handler := NewHandler(store, "bootstrap-secret", fixedNow)

	for attempt := 1; attempt <= 5; attempt++ {
		recorder := performJSON(handler.Login, http.MethodPost, `{"username":"admin","password":"wrong password value"}`, "", nil)
		if attempt < 5 {
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		} else {
			require.Equal(t, http.StatusTooManyRequests, recorder.Code)
			require.NotEmpty(t, recorder.Header().Get("Retry-After"))
		}
	}
}

func TestSessionMiddlewareRejectsExpiryAndUnsafeRequestWithoutCSRF(t *testing.T) {
	store := newMemoryStore()
	hash, err := Hash("correct horse battery staple", DefaultParams)
	require.NoError(t, err)
	store.admins = append(store.admins, Admin{ID: 1, Username: "admin", PasswordHash: hash})
	handler := NewHandler(store, "bootstrap-secret", fixedNow)
	login := performJSON(handler.Login, http.MethodPost, `{"username":"admin","password":"correct horse battery staple"}`, "", nil)
	require.Equal(t, http.StatusOK, login.Code)
	cookie := login.Result().Cookies()[0]

	protected := handler.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	missingCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	missingCSRF.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, missingCSRF)
	require.Equal(t, http.StatusForbidden, recorder.Code)

	var response sessionResponse
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &response))
	valid := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	valid.AddCookie(cookie)
	valid.Header.Set("X-CSRF-Token", response.CSRFToken)
	recorder = httptest.NewRecorder()
	protected.ServeHTTP(recorder, valid)
	require.Equal(t, http.StatusNoContent, recorder.Code)

	for key, session := range store.sessions {
		session.IdleExpiresAt = fixedNow().Add(-time.Second)
		store.sessions[key] = session
	}
	expired := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	expired.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	protected.ServeHTTP(recorder, expired)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func fixedNow() time.Time { return time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC) }

func performJSON(endpoint http.HandlerFunc, method, body, authorization string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.1:4321"
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	endpoint(recorder, request)
	return recorder
}

type memoryStore struct {
	mu       sync.Mutex
	admins   []Admin
	sessions map[[32]byte]Session
}

func newMemoryStore() *memoryStore { return &memoryStore{sessions: make(map[[32]byte]Session)} }

func (store *memoryStore) AdminCount(context.Context) (int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.admins), nil
}

func (store *memoryStore) CreateAdmin(_ context.Context, username, passwordHash string) (Admin, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	admin := Admin{ID: 1, Username: username, PasswordHash: passwordHash}
	store.admins = append(store.admins, admin)
	return admin, nil
}

func (store *memoryStore) FindAdmin(_ context.Context, username string) (Admin, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, admin := range store.admins {
		if admin.Username == username {
			return admin, nil
		}
	}
	return Admin{}, ErrNotFound
}

func (store *memoryStore) UpdatePassword(_ context.Context, id int64, hash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.admins {
		if store.admins[index].ID == id {
			store.admins[index].PasswordHash = hash
			return nil
		}
	}
	return ErrNotFound
}

func (store *memoryStore) CreateSession(_ context.Context, session Session) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessions[session.TokenHash] = session
	return nil
}

func (store *memoryStore) FindSession(_ context.Context, hash [32]byte) (Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, ok := store.sessions[hash]
	if !ok {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (store *memoryStore) TouchSession(_ context.Context, hash [32]byte, lastSeen, idleExpires time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, ok := store.sessions[hash]
	if !ok {
		return ErrNotFound
	}
	session.LastSeenAt, session.IdleExpiresAt = lastSeen, idleExpires
	store.sessions[hash] = session
	return nil
}

func (store *memoryStore) DeleteSession(_ context.Context, hash [32]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.sessions, hash)
	return nil
}

func (store *memoryStore) DeleteAdminSessions(_ context.Context, adminID int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, session := range store.sessions {
		if session.AdminID == adminID {
			delete(store.sessions, key)
		}
	}
	return nil
}
